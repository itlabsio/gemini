// Package notify шлёт исходящие нотификации (Slack / Telegram webhook) об исходах
// прогонов. Каких именно — решает вызывающий (настройки notify_on_*).
// Текст ошибки фильтруется от возможных кред перед отправкой.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/config"
)

// Notifier — отправитель нотификаций.
type Notifier struct {
	cfg  config.NotifyConfig
	http *http.Client
}

// New создаёт Notifier (nil-safe: при Enabled=false методы становятся no-op).
func New(cfg config.NotifyConfig) *Notifier {
	return &Notifier{cfg: cfg, http: &http.Client{Timeout: 10 * time.Second}}
}

// Event — данные для сообщения о прогоне.
type Event struct {
	HeadRole   string
	Kind       string // dump | restore
	Database   string
	RunID      string
	Error      string // пусто у Success
	OccurredAt time.Time
}

// Failure отправляет уведомление о провале прогона.
func (n *Notifier) Failure(ctx context.Context, e Event) error {
	return n.send(ctx, fmt.Sprintf(
		"🔴 Gemini/%s: %s FAILED\nБаза: %s\nRun: %s\nВремя: %s\nОшибка: %s",
		e.HeadRole, e.Kind, e.Database, e.RunID,
		e.OccurredAt.Format(time.RFC3339), Sanitize(e.Error),
	))
}

// Success отправляет уведомление об успешном прогоне.
func (n *Notifier) Success(ctx context.Context, e Event) error {
	return n.send(ctx, fmt.Sprintf(
		"🟢 Gemini/%s: %s OK\nБаза: %s\nRun: %s\nВремя: %s",
		e.HeadRole, e.Kind, e.Database, e.RunID, e.OccurredAt.Format(time.RFC3339),
	))
}

func (n *Notifier) send(ctx context.Context, text string) error {
	if n == nil || !n.cfg.Enabled || n.cfg.WebhookURL == "" {
		return nil
	}

	var payload any
	switch n.cfg.Provider {
	case "telegram":
		msg := map[string]any{"chat_id": n.cfg.TelegramChatID, "text": text, "disable_web_page_preview": true}
		if id, err := strconv.ParseInt(n.cfg.TelegramThreadID, 10, 64); err == nil {
			msg["message_thread_id"] = id
		}
		payload = msg
	default: // slack
		payload = map[string]any{"text": text}
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("notify: webhook returned %d", resp.StatusCode)
	}
	return nil
}

// credPatterns вычищают из текста строки, похожие на пароли/DSN.
var credPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|pgpassword|pwd)\s*[:=]\s*\S+`),
	regexp.MustCompile(`postgres(?:ql)?://[^@\s]+@`),
	regexp.MustCompile(`(?i)authorization:\s*\S+`),
}

// Sanitize маскирует потенциальные секреты в свободном тексте ошибки.
func Sanitize(s string) string {
	for _, re := range credPatterns {
		s = re.ReplaceAllString(s, "[redacted]")
	}
	if len(s) > 1500 {
		s = s[:1500] + "…"
	}
	return s
}

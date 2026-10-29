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
	"strings"
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
	Instance   string // имя инстанса PostgreSQL; пусто → строка не выводится
	Database   string
	RunID      string
	Error      string // пусто у Success
	OccurredAt time.Time
}

// Failure отправляет уведомление о провале прогона.
func (n *Notifier) Failure(ctx context.Context, e Event) error {
	return n.send(ctx, fmt.Sprintf("❌ Gemini/%s: %s FAILED\n\n%sОшибка: %s",
		e.HeadRole, e.Kind, e.details(), Sanitize(e.Error)))
}

// Success отправляет уведомление об успешном прогоне.
func (n *Notifier) Success(ctx context.Context, e Event) error {
	return n.send(ctx, fmt.Sprintf("✅ Gemini/%s: %s OK\n\n%s",
		e.HeadRole, e.Kind, strings.TrimSuffix(e.details(), "\n")))
}

// details — общий блок сообщения: инстанс, база, run, время (каждое с \n).
func (e Event) details() string {
	var b strings.Builder
	if e.Instance != "" {
		fmt.Fprintf(&b, "Инстанс: %s\n", e.Instance)
	}
	fmt.Fprintf(&b, "База: %s\nRun: %s\nВремя: %s\n", e.Database, e.RunID, e.OccurredAt.Format(time.RFC3339))
	return b.String()
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

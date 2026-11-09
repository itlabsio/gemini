// Package notify шлёт исходящие нотификации об исходах прогонов в каналы
// (Telegram, MAX; новый провайдер — ветка в Send, ValidToken и ValidTarget).
// Текст ошибки фильтруется от возможных кред перед отправкой.
package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Провайдеры каналов.
const (
	ProviderTelegram = "telegram"
	ProviderMax      = "max"
)

const (
	maxParallelSends = 5
	testInterval     = 10 * time.Second
)

// API MAX работает на сертификате УЦ Минцифры. Доверие к нему ограничено клиентом
// MAX: в системное хранилище образа сертификаты не добавляются, чтобы этому УЦ не
// доверяли соединения с S3, Keycloak, Vault и базами.
var (
	//go:embed certs/russian-trusted-root.crt
	maxRootCA []byte
	//go:embed certs/russian-trusted-sub.crt
	maxSubCA []byte
)

// newMaxClient — HTTP-клиент, доверяющий только УЦ Минцифры.
func newMaxClient() *http.Client {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(maxRootCA) || !pool.AppendCertsFromPEM(maxSubCA) {
		panic("notify: invalid embedded MAX CA certificates")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return &http.Client{Timeout: 10 * time.Second, Transport: tr}
}

// Channel — куда и чем отправить сообщение. Token — расшифрованный токен бота.
type Channel struct {
	Name     string
	Provider string
	Token    string
	ChatID   string
	ThreadID string
}

// Notifier — отправитель нотификаций.
type Notifier struct {
	http        *http.Client
	maxHTTP     *http.Client
	telegramURL string
	maxURL      string
	slots       chan struct{}

	mu       sync.Mutex
	lastTest map[string]time.Time
}

// New создаёт Notifier.
func New() *Notifier {
	return &Notifier{
		http:        &http.Client{Timeout: 10 * time.Second},
		maxHTTP:     newMaxClient(),
		telegramURL: "https://api.telegram.org",
		maxURL:      "https://platform-api2.max.ru",
		slots:       make(chan struct{}, maxParallelSends),
		lastTest:    map[string]time.Time{},
	}
}

// Event — данные для сообщения о прогоне.
type Event struct {
	HeadRole   string
	Kind       string // dump | restore
	Project    string // проект инстанса; пусто → только глобальные каналы
	Instance   string // имя инстанса PostgreSQL; пусто → строка не выводится
	Database   string
	RunID      string
	Error      string // пусто у Success
	OccurredAt time.Time
}

// FailureText — сообщение о провале прогона.
func FailureText(e Event) string {
	return fmt.Sprintf("❌ Gemini/%s: %s FAILED\n\n%sОшибка: %s",
		e.HeadRole, e.Kind, e.details(), Sanitize(e.Error))
}

// SuccessText — сообщение об успешном прогоне.
func SuccessText(e Event) string {
	return fmt.Sprintf("✅ Gemini/%s: %s OK\n\n%s",
		e.HeadRole, e.Kind, strings.TrimSuffix(e.details(), "\n"))
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

// Dispatch отправляет в фоне: вызывается из цикла сверки прогонов, и недоступный
// канал не должен его задерживать. Ошибка только логируется.
// ponytail: без очереди и ретраев — сообщение теряется при сбое или рестарте,
// очередь в БД понадобится, если доставка станет критичной.
func (n *Notifier) Dispatch(ch Channel, text string) {
	go func() {
		n.slots <- struct{}{}
		defer func() { <-n.slots }()
		if err := n.Send(context.Background(), ch, text); err != nil {
			log.Printf("notify: channel %q: %v", ch.Name, err)
		}
	}()
}

// AllowTest ограничивает тестовые отправки: не чаще раза в testInterval на канал.
// ponytail: счётчик на реплику, общий лимит не нужен, пока реплик единицы.
func (n *Notifier) AllowTest(channelID string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := time.Now()
	if last, ok := n.lastTest[channelID]; ok && now.Sub(last) < testInterval {
		return false
	}
	n.lastTest[channelID] = now
	return true
}

// Send отправляет сообщение и дожидается ответа.
func (n *Notifier) Send(ctx context.Context, ch Channel, text string) error {
	var err error
	switch ch.Provider {
	case ProviderTelegram:
		err = n.sendTelegram(ctx, ch, text)
	case ProviderMax:
		err = n.sendMax(ctx, ch, text)
	default:
		return fmt.Errorf("notify: unsupported provider %q", ch.Provider)
	}
	return scrubURL(ch.Provider, err)
}

// scrubURL убирает URL из ошибки транспорта: в URL Telegram — токен бота.
func scrubURL(provider string, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("notify: %s: %w", provider, ue.Err)
	}
	return err
}

// BotName запрашивает у мессенджера имя бота и этим же проверяет токен.
// Возвращает @username, а без него — отображаемое имя.
func (n *Notifier) BotName(ctx context.Context, provider, token string) (string, error) {
	var (
		req    *http.Request
		err    error
		client = n.http
	)
	switch provider {
	case ProviderTelegram:
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, n.telegramURL+"/bot"+token+"/getMe", nil)
	case ProviderMax:
		if req, err = http.NewRequestWithContext(ctx, http.MethodGet, n.maxURL+"/me", nil); err == nil {
			req.Header.Set("Authorization", token)
		}
		client = n.maxHTTP
	default:
		return "", fmt.Errorf("notify: unsupported provider %q", provider)
	}
	if err != nil {
		return "", scrubURL(provider, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", scrubURL(provider, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("notify: %s returned %d", provider, resp.StatusCode)
	}
	type bot struct {
		Username  string `json:"username"`
		FirstName string `json:"first_name"`
	}
	var out struct {
		bot
		Result bot `json:"result"` // Telegram оборачивает ответ в result
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return "", fmt.Errorf("notify: %s: bad response: %w", provider, err)
	}
	b := out.bot
	if provider == ProviderTelegram {
		b = out.Result
	}
	switch {
	case b.Username != "":
		return "@" + b.Username, nil
	case b.FirstName != "":
		return b.FirstName, nil
	}
	return "", fmt.Errorf("notify: %s returned no bot name", provider)
}

func (n *Notifier) sendTelegram(ctx context.Context, ch Channel, text string) error {
	msg := map[string]any{"chat_id": ch.ChatID, "text": text, "disable_web_page_preview": true}
	if id, err := strconv.ParseInt(ch.ThreadID, 10, 64); err == nil {
		msg["message_thread_id"] = id
	}
	body, _ := json.Marshal(msg)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		n.telegramURL+"/bot"+ch.Token+"/sendMessage", bytes.NewReader(body))
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
		var out struct {
			Description string `json:"description"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out)
		return fmt.Errorf("notify: telegram returned %d: %s", resp.StatusCode, out.Description)
	}
	return nil
}

// sendMax шлёт текст в чат MAX: токен — в заголовке, chat_id — в query.
// Текст укладывается в лимит MAX (4000 символов): ошибка режется Sanitize до 1500 байт.
func (n *Notifier) sendMax(ctx context.Context, ch Channel, text string) error {
	body, _ := json.Marshal(map[string]string{"text": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		n.maxURL+"/messages?"+url.Values{"chat_id": {ch.ChatID}}.Encode(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", ch.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.maxHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var out struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out)
		return fmt.Errorf("notify: max returned %d: %s %s", resp.StatusCode, out.Code, out.Message)
	}
	return nil
}

var (
	telegramTokenRe  = regexp.MustCompile(`^\d+:[A-Za-z0-9_-]{20,}$`)
	telegramChatRe   = regexp.MustCompile(`^(-?\d{1,20}|@[A-Za-z0-9_]{5,32})$`)
	telegramThreadRe = regexp.MustCompile(`^\d{0,20}$`)
	// Формат токена MAX не задокументирован: только печатные ASCII без пробелов,
	// чтобы значение было безопасно для заголовка.
	maxTokenRe = regexp.MustCompile(`^[\x21-\x7e]{20,512}$`)
	maxChatRe  = regexp.MustCompile(`^-?\d{1,20}$`)
)

// ValidToken проверяет формат токена: он попадает в путь URL или заголовок,
// поэтому произвольные символы пропускать нельзя. Неизвестный провайдер — false.
func ValidToken(provider, token string) bool {
	switch provider {
	case ProviderTelegram:
		return telegramTokenRe.MatchString(token)
	case ProviderMax:
		return maxTokenRe.MatchString(token)
	}
	return false
}

// ValidTarget проверяет адрес получателя (топиков в MAX нет). Неизвестный провайдер — false.
func ValidTarget(provider, chatID, threadID string) bool {
	switch provider {
	case ProviderTelegram:
		return telegramChatRe.MatchString(chatID) && telegramThreadRe.MatchString(threadID)
	case ProviderMax:
		return maxChatRe.MatchString(chatID) && threadID == ""
	}
	return false
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
	if len(s) > maxErrorLen {
		// Режем по границе руны: ошибки PostgreSQL часто на русском, и срез
		// посреди символа дал бы невалидный UTF-8 в сообщении и в БД.
		n := maxErrorLen
		for n > 0 && !utf8.RuneStart(s[n]) {
			n--
		}
		s = s[:n] + "…"
	}
	return s
}

// maxErrorLen — предел длины текста ошибки после Sanitize (в байтах).
const maxErrorLen = 1500

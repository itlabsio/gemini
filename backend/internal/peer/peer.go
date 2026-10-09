// Package peer — HTTP-обмен между головами Gemini.
//
// Через границу контуров ходят ТОЛЬКО метаданные события (id базы, ключ в S3,
// статус, время). Секреты подключений здесь не передаются никогда.
//
// Аутентификация — HMAC-SHA256 подпись каждого запроса на основе секрета из
// HeadPairing.auth_secret_hash. Сам секрет по сети повторно не ходит.
package peer

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/config"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

const (
	headerTimestamp = "X-Gemini-Timestamp"
	headerSignature = "X-Gemini-Signature"
	headerPeerRole  = "X-Gemini-Peer-Role"
)

// Sign вычисляет подпись запроса: HMAC-SHA256(secret, method + "\n" + path + "\n" + ts + "\n" + body).
// path должен включать query-строку, если она есть: верификатор подписывает
// r.URL.RequestURI(), иначе параметры запроса остались бы вне подписи.
func Sign(secret, method, path, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(method))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(path))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(ts))
	mac.Write([]byte{'\n'})
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// artifactDomain отделяет пространство подписей артефактов в S3 от подписей
// HTTP-запросов: одну нельзя предъявить вместо другой.
const artifactDomain = "gemini-dump-v1"

// artifactMessage — то, что подписывается: домен ‖ ключ ‖ sha256.
func artifactMessage(s3Key, sha256hex string) []byte {
	return []byte(artifactDomain + "\n" + s3Key + "\n" + sha256hex)
}

// SignArtifact подписывает манифест дампа (ключ + sha256) приватным ключом Source.
//
// Зачем: файл <key>.sha256 лежит в том же бакете, что и дамп, и пишется тем же
// принципалом — он доказывает только отсутствие повреждения при передаче, но не
// происхождение. Кто может писать в бакет, тот подменит и дамп, и его сумму, а
// Target выполнит полученный SQL через psql. Подпись переносит доверие с ACL
// бакета на ключ Source, приватная часть которого бакета никогда не покидает.
//
// Ключ Source асимметричный: одну подпись проверяют все Target'ы своим
// запиненным публичным ключом (см. VerifyArtifact).
func SignArtifact(priv ed25519.PrivateKey, s3Key, sha256hex string) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, artifactMessage(s3Key, sha256hex)))
}

// VerifyArtifact сверяет подпись манифеста запиненным публичным ключом Source.
func VerifyArtifact(pub ed25519.PublicKey, s3Key, sha256hex, sig string) bool {
	raw, err := base64.StdEncoding.DecodeString(sig)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(pub, artifactMessage(s3Key, sha256hex), raw)
}

// Client шлёт подписанные запросы второй голове с экспоненциальным backoff.
type Client struct {
	http     *http.Client
	retry    config.PeerRetryConfig
	selfRole string
}

// NewClient создаёт peer-клиента. selfRole — роль ЭТОЙ головы ("source"/"dr").
func NewClient(retry config.PeerRetryConfig, selfRole string) *Client {
	return &Client{
		http:     &http.Client{Timeout: 15 * time.Second},
		retry:    retry,
		selfRole: selfRole,
	}
}

// Post отправляет JSON-тело на peerURL+path, подписывая запрос secret'ом.
// Повторяет при сетевой ошибке / 5xx с backoff initialDelay→…→maxDelay,
// до retry.MaxAttempts попыток. errors.Is(err, ErrExhausted) — ретраи исчерпаны.
func (c *Client) Post(ctx context.Context, peerURL, path, secret string, body []byte) error {
	delay := c.retry.InitialDelay
	var lastErr error
	for attempt := 1; attempt <= c.retry.MaxAttempts; attempt++ {
		err := c.doOnce(ctx, peerURL, path, secret, body)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable(err) {
			return err
		}
		if attempt == c.retry.MaxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		if delay *= 2; delay > c.retry.MaxDelay {
			delay = c.retry.MaxDelay
		}
	}
	return fmt.Errorf("%w: %v", ErrExhausted, lastErr)
}

// Get выполняет один подписанный GET-запрос (без ретраев — вызывается синхронно
// из обработчика UI) и возвращает тело ответа.
func (c *Client) Get(ctx context.Context, peerURL, path, secret string) ([]byte, error) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := Sign(secret, http.MethodGet, path, ts, nil)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, peerURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(headerTimestamp, ts)
	req.Header.Set(headerSignature, sig)
	req.Header.Set(headerPeerRole, c.selfRole)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, httpStatusError{code: resp.StatusCode}
	}
	return body, nil
}

// ErrExhausted — все попытки доставки исчерпаны.
var ErrExhausted = errors.New("peer: retries exhausted")

type httpStatusError struct{ code int }

func (e httpStatusError) Error() string { return "peer: unexpected status " + strconv.Itoa(e.code) }

func retryable(err error) bool {
	var se httpStatusError
	if errors.As(err, &se) {
		return se.code >= 500
	}
	return true // сетевые ошибки — ретраим
}

func (c *Client) doOnce(ctx context.Context, peerURL, path, secret string, body []byte) error {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := Sign(secret, http.MethodPost, path, ts, body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, peerURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerTimestamp, ts)
	req.Header.Set(headerSignature, sig)
	req.Header.Set(headerPeerRole, c.selfRole)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 300 {
		return httpStatusError{code: resp.StatusCode}
	}
	return nil
}

// Verifier проверяет подпись входящих межголовых запросов.
type Verifier struct {
	skew time.Duration
	// expectedPeerRole — роль пира, выведенная из СОБСТВЕННОЙ роли головы.
	// Именно она выбирает секреты; одноимённый заголовок запроса только сверяется.
	expectedPeerRole string
	// secretsFor возвращает HMAC-секреты всех active-пар роли (Source может быть
	// спарен с несколькими Target). Запрос принимается, если подходит любой.
	secretsFor func(ctx context.Context, peerRole string) ([]model.PairingSecret, error)
}

// NewVerifier создаёт верификатор. expectedPeerRole берётся из конфига головы
// (config.PeerRole), а не из запроса.
func NewVerifier(skew time.Duration, expectedPeerRole string,
	secretsFor func(ctx context.Context, peerRole string) ([]model.PairingSecret, error)) *Verifier {
	return &Verifier{skew: skew, expectedPeerRole: expectedPeerRole, secretsFor: secretsFor}
}

type ctxKey int

const pairingIDKey ctxKey = 0

// PeerPairingID возвращает id пары, чьим секретом был подписан текущий запрос
// (устанавливается Middleware). Пусто вне обработчика межголового запроса.
func PeerPairingID(ctx context.Context) string {
	id, _ := ctx.Value(pairingIDKey).(string)
	return id
}

// Middleware проверяет X-Gemini-Signature / X-Gemini-Timestamp на webhook-эндпоинтах.
func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts := r.Header.Get(headerTimestamp)
		sig := r.Header.Get(headerSignature)
		peerRole := r.Header.Get(headerPeerRole)
		if ts == "" || sig == "" || peerRole == "" {
			http.Error(w, "missing peer auth headers", http.StatusUnauthorized)
			return
		}
		// Заголовок только сверяем — секрет ниже берём по роли из конфига.
		if peerRole != v.expectedPeerRole {
			http.Error(w, "unexpected peer role", http.StatusUnauthorized)
			return
		}
		tsInt, err := strconv.ParseInt(ts, 10, 64)
		if err != nil || absDuration(time.Since(time.Unix(tsInt, 0))) > v.skew {
			http.Error(w, "stale or invalid timestamp", http.StatusUnauthorized)
			return
		}

		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(io.LimitReader(r.Body, 1<<20))
			r.Body = io.NopCloser(bytes.NewReader(body))
		}

		secrets, err := v.secretsFor(r.Context(), v.expectedPeerRole)
		if err != nil || len(secrets) == 0 {
			http.Error(w, "no active pairing for peer", http.StatusUnauthorized)
			return
		}
		// RequestURI() = путь + query. Пробуем секрет каждой active-пары роли:
		// Source может быть спарен с несколькими Target'ами, и любой из них —
		// легитимный отправитель. id совпавшей пары кладём в контекст.
		matched := ""
		for _, ps := range secrets {
			want := Sign(ps.Secret, r.Method, r.URL.RequestURI(), ts, body)
			if subtle.ConstantTimeCompare([]byte(want), []byte(sig)) == 1 {
				matched = ps.PairingID
				break
			}
		}
		if matched == "" {
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), pairingIDKey, matched)))
	})
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

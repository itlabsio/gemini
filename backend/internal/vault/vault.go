// Package vault резолвит секреты подключений из HashiCorp Vault (KV v1/v2).
// Секрет никогда не покидает голову — используется локально для создания
// ephemeral k8s Secret на время выполнения Job.
package vault

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	vaultapi "github.com/hashicorp/vault/api"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/config"
)

// Client — тонкая обёртка над Vault API с ленивой аутентификацией.
type Client struct {
	cfg    config.VaultConfig
	api    *vaultapi.Client
	mu     sync.Mutex
	expiry time.Time
}

// DBSecret — то, что мы ожидаем найти по vault_path.
type DBSecret struct {
	Username string
	Password string
}

// New создаёт клиента (без немедленной аутентификации).
func New(cfg config.VaultConfig) (*Client, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	vc := vaultapi.DefaultConfig()
	vc.Address = cfg.Address
	api, err := vaultapi.NewClient(vc)
	if err != nil {
		return nil, fmt.Errorf("vault client: %w", err)
	}
	if cfg.AuthMethod == "token" && cfg.Token != "" {
		api.SetToken(cfg.Token)
	}
	return &Client{cfg: cfg, api: api}, nil
}

func (c *Client) ensureAuth(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cfg.AuthMethod == "token" {
		return nil
	}
	if time.Now().Before(c.expiry) {
		return nil
	}

	jwtBytes, err := os.ReadFile(c.cfg.K8sTokenPath)
	if err != nil {
		return fmt.Errorf("read SA token: %w", err)
	}
	secret, err := c.api.Logical().WriteWithContext(ctx,
		fmt.Sprintf("auth/%s/login", c.cfg.K8sMountPath),
		map[string]any{"role": c.cfg.K8sRole, "jwt": string(jwtBytes)},
	)
	if err != nil {
		return fmt.Errorf("vault k8s login: %w", err)
	}
	if secret == nil || secret.Auth == nil {
		return fmt.Errorf("vault k8s login: empty auth")
	}
	c.api.SetToken(secret.Auth.ClientToken)
	ttl := time.Duration(secret.Auth.LeaseDuration) * time.Second
	c.expiry = time.Now().Add(ttl - time.Minute)
	return nil
}

// readFields читает секрет по ПОЛНОМУ пути Vault API — ровно тому, что
// показывает UI и принимает `vault read`, вместе с mount'ом и (для KV v2)
// сегментом data/: например "kv/data/prod/db-backup/core".
//
// Именно поэтому у головы нет настройки KV-хранилища: секреты разных баз лежат
// в разных mount'ах, общего выбрать нельзя, а mount и так уже есть в пути.
func (c *Client) readFields(ctx context.Context, path string) (map[string]any, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, err
	}
	sec, err := c.api.Logical().ReadWithContext(ctx, strings.Trim(path, "/"))
	if err != nil {
		return nil, fmt.Errorf("vault read %q: %w", path, err)
	}
	if sec == nil || sec.Data == nil {
		hint := ""
		if !strings.Contains(path, "/data/") {
			hint = ` (для KV v2 путь должен содержать сегмент "data/", напр. kv/data/prod/db-backup/core)`
		}
		return nil, fmt.Errorf("vault: no secret at %q%s", path, hint)
	}
	// KV v2 заворачивает значения ещё раз: {"data": {...}, "metadata": {...}}.
	// KV v1 отдаёт поля на верхнем уровне — поддерживаем оба.
	if inner, ok := sec.Data["data"].(map[string]any); ok {
		return inner, nil
	}
	return sec.Data, nil
}

// guessUsernameKeys / guessPasswordKeys — имена, по которым логин и пароль ищутся,
// когда сопоставление не задано явно (старые инстансы и очевидные случаи).
var (
	guessUsernameKeys = []string{"username", "user", "login"}
	guessPasswordKeys = []string{"password", "pass", "pwd"}
)

// SecretKeys возвращает ТОЛЬКО имена полей секрета, отсортированные. Значения
// наружу не отдаются никогда — список нужен UI, чтобы админ сопоставил ключи
// с логином и паролем.
func (c *Client) SecretKeys(ctx context.Context, path string) ([]string, error) {
	fields, err := c.readFields(ctx, path)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

// GuessKeys подсказывает сопоставление по известным именам ("" — не угадали).
func GuessKeys(keys []string) (usernameKey, passwordKey string) {
	pick := func(candidates []string) string {
		for _, c := range candidates {
			if slices.Contains(keys, c) {
				return c
			}
		}
		return ""
	}
	return pick(guessUsernameKeys), pick(guessPasswordKeys)
}

// ReadDBSecret достаёт логин/пароль из секрета. usernameKey/passwordKey —
// сопоставление, заданное на инстансе; пустые значения включают эвристику по
// известным именам полей.
func (c *Client) ReadDBSecret(ctx context.Context, path, usernameKey, passwordKey string) (*DBSecret, error) {
	fields, err := c.readFields(ctx, path)
	if err != nil {
		return nil, err
	}

	get := func(explicit string, guesses []string) (string, error) {
		str := func(k string) (string, bool) {
			v, ok := fields[k]
			if !ok {
				return "", false
			}
			s, ok := v.(string)
			return s, ok && s != ""
		}
		if explicit != "" {
			s, ok := str(explicit)
			if !ok {
				return "", fmt.Errorf("key %q is missing or not a non-empty string", explicit)
			}
			return s, nil
		}
		for _, k := range guesses {
			if s, ok := str(k); ok {
				return s, nil
			}
		}
		return "", fmt.Errorf("none of %v found — задайте сопоставление ключей у инстанса", guesses)
	}

	user, err := get(usernameKey, guessUsernameKeys)
	if err != nil {
		return nil, fmt.Errorf("vault secret %q: username: %w", path, err)
	}
	pass, err := get(passwordKey, guessPasswordKeys)
	if err != nil {
		return nil, fmt.Errorf("vault secret %q: password: %w", path, err)
	}
	return &DBSecret{Username: user, Password: pass}, nil
}

// Check проверяет доступность пути и сопоставления (при сохранении Instance).
func (c *Client) Check(ctx context.Context, path, usernameKey, passwordKey string) error {
	_, err := c.ReadDBSecret(ctx, path, usernameKey, passwordKey)
	return err
}

// Ping проверяет, что Vault доступен, распечатан и аутентификация проходит
// (для readiness-проба).
func (c *Client) Ping(ctx context.Context) error {
	h, err := c.api.Sys().HealthWithContext(ctx)
	if err != nil {
		return fmt.Errorf("vault health: %w", err)
	}
	switch {
	case !h.Initialized:
		return fmt.Errorf("vault is not initialized")
	case h.Sealed:
		return fmt.Errorf("vault is sealed")
	}
	if err := c.ensureAuth(ctx); err != nil {
		return fmt.Errorf("vault auth: %w", err)
	}
	return nil
}

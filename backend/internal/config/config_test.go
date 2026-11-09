package config

import (
	"strings"
	"testing"
)

func validConfig() Config {
	return Config{
		Role:                RoleSource,
		OwnDatabaseDSN:      "postgres://x",
		SecretEncryptionKey: "0123456789abcdef0123456789abcdef",
		OIDC:                OIDCConfig{IssuerURL: "https://kc/realms/r", ClientID: "gemini"},
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string // "" — конфиг валиден
	}{
		{"minimal source", func(*Config) {}, ""},
		{"target", func(c *Config) { c.Role = RoleTarget }, ""},
		{"bad role", func(c *Config) { c.Role = "dr" }, "HEAD_ROLE"},
		{"no dsn", func(c *Config) { c.OwnDatabaseDSN = "" }, "POSTGRES_URL"},
		{"short key", func(c *Config) { c.SecretEncryptionKey = "short" }, "SECRET_ENCRYPTION_KEY"},
		{"no issuer", func(c *Config) { c.OIDC.IssuerURL = "" }, "OIDC_ISSUER_URL"},
		{"no client", func(c *Config) { c.OIDC.ClientID = "" }, "OIDC_CLIENT_ID"},
		{"vault no addr", func(c *Config) {
			c.Vault = VaultConfig{Enabled: true, AuthMethod: "token", Token: "t"}
		}, "VAULT_ADDR"},
		{"vault k8s ok", func(c *Config) {
			c.Vault = VaultConfig{Enabled: true, Address: "https://v", AuthMethod: "kubernetes", K8sRole: "gemini"}
		}, ""},
		{"vault k8s no role", func(c *Config) {
			c.Vault = VaultConfig{Enabled: true, Address: "https://v", AuthMethod: "kubernetes"}
		}, "VAULT_K8S_ROLE"},
		{"vault token no token", func(c *Config) {
			c.Vault = VaultConfig{Enabled: true, Address: "https://v", AuthMethod: "token"}
		}, "VAULT_TOKEN"},
		// Частая ошибка: имя auth-mount'а пишут в VAULT_AUTH_METHOD.
		{"vault mount name as method", func(c *Config) {
			c.Vault = VaultConfig{Enabled: true, Address: "https://v", AuthMethod: "k8s-prod", K8sRole: "gemini"}
		}, "VAULT_K8S_MOUNT"},
		{"vault disabled ignores fields", func(c *Config) { c.Vault = VaultConfig{AuthMethod: "garbage"} }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validConfig()
			tc.mutate(&c)
			err := c.validate()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("want error mentioning %s, got nil", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Fatalf("error %q does not mention %s", err, tc.wantErr)
			}
		})
	}
}

// Роль пира выводится из своей, а не из заголовка запроса (см. PeerRole).
func TestPeerRole(t *testing.T) {
	if got := (&Config{Role: RoleSource}).PeerRole(); got != "target" {
		t.Errorf("source peer = %q", got)
	}
	if got := (&Config{Role: RoleTarget}).PeerRole(); got != "source" {
		t.Errorf("target peer = %q", got)
	}
}

func TestEnvHelpers(t *testing.T) {
	t.Setenv("GEMINI_T_BOOL", "Yes")
	t.Setenv("GEMINI_T_BAD", "maybe")
	t.Setenv("GEMINI_T_INT", "notanint")
	t.Setenv("GEMINI_T_DUR", "90s")
	if !envBool("GEMINI_T_BOOL", false) {
		t.Error("envBool(Yes) = false")
	}
	if !envBool("GEMINI_T_BAD", true) || envBool("GEMINI_T_BAD", false) {
		t.Error("envBool: unknown value must fall back to default")
	}
	if envInt("GEMINI_T_INT", 7) != 7 {
		t.Error("envInt: garbage must fall back to default")
	}
	if envDuration("GEMINI_T_DUR", 0).Seconds() != 90 {
		t.Error("envDuration(90s)")
	}
}

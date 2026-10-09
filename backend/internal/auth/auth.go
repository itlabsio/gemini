// Package auth валидирует JWT от Keycloak (OIDC) по JWKS и извлекает роль
// пользователя (admin / operator / viewer). Проверка выполняется на обеих головах
// локально — токен второй головы здесь ни при чём (для межголовых вызовов см. peer).
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/config"
)

// Role — роль пользователя в СРК.
type Role string

const (
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
)

// rank задаёт иерархию: admin ⊇ operator ⊇ viewer.
var rank = map[Role]int{RoleViewer: 1, RoleOperator: 2, RoleAdmin: 3}

// AtLeast сообщает, покрывает ли роль r требуемый минимум min.
func (r Role) AtLeast(min Role) bool { return rank[r] >= rank[min] }

// Identity — установленная личность запроса.
type Identity struct {
	Subject string
	Email   string
	Role    Role
}

type ctxKey int

const identityKey ctxKey = 0

// FromContext достаёт Identity, установленную Middleware.
func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityKey).(Identity)
	return id, ok
}

// Verifier кеширует JWKS и валидирует токены.
type Verifier struct {
	cfg    config.OIDCConfig
	keyset jwk.Set // самообновляющийся CachedSet
}

// NewVerifier регистрирует JWKS-эндпоинт Keycloak и делает первичную загрузку ключей.
func NewVerifier(ctx context.Context, cfg config.OIDCConfig) (*Verifier, error) {
	jwksURI := cfg.IssuerURL + "/protocol/openid-connect/certs"
	cache, err := jwk.NewCache(ctx, httprc.NewClient())
	if err != nil {
		return nil, fmt.Errorf("jwk cache: %w", err)
	}
	if err := cache.Register(ctx, jwksURI, jwk.WithMinInterval(15*time.Minute)); err != nil {
		return nil, fmt.Errorf("register jwks: %w", err)
	}
	if _, err := cache.Refresh(ctx, jwksURI); err != nil {
		return nil, fmt.Errorf("initial jwks fetch: %w", err)
	}
	keyset, err := cache.CachedSet(jwksURI)
	if err != nil {
		return nil, fmt.Errorf("cached jwks set: %w", err)
	}
	log.Printf("auth: JWKS cache initialized for %s", jwksURI)
	return &Verifier{cfg: cfg, keyset: keyset}, nil
}

func (v *Verifier) parse(raw string) (jwt.Token, error) {
	tok, err := jwt.Parse([]byte(raw),
		jwt.WithKeySet(v.keyset),
		jwt.WithValidate(true),
		jwt.WithIssuer(v.cfg.IssuerURL),
	)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	// Токен должен быть выпущен для нашего клиента (azp или aud).
	if !audienceMatches(tok, v.cfg.ClientID) {
		return nil, fmt.Errorf("token not issued for client %q", v.cfg.ClientID)
	}
	return tok, nil
}

func audienceMatches(tok jwt.Token, clientID string) bool {
	var azp string
	if err := tok.Get("azp", &azp); err == nil && azp == clientID {
		return true
	}
	if aud, ok := tok.Audience(); ok {
		if slices.Contains(aud, clientID) {
			return true
		}
	}
	return false
}

// resolveRole берёт роли ТОЛЬКО из resource_access.<ClientID>.roles (client-роли
// клиента головы) и возвращает максимальную из известных СРК ролей. realm-роли и
// groups сознательно не учитываются — иначе realm-роль с именем "admin" у
// realm-администратора поднимала бы его в СРК.
func (v *Verifier) resolveRole(tok jwt.Token) (Role, bool) {
	var resourceAccess map[string]any
	if err := tok.Get("resource_access", &resourceAccess); err != nil {
		return "", false
	}
	client, ok := resourceAccess[v.cfg.ClientID]
	if !ok {
		return "", false
	}

	best := Role("")
	for _, name := range stringsFrom(client, "roles") {
		switch Role(name) {
		case RoleAdmin, RoleOperator, RoleViewer:
			if rank[Role(name)] > rank[best] {
				best = Role(name)
			}
		}
	}
	if best == "" {
		return "", false
	}
	return best, true
}

func stringsFrom(v any, key string) []string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	arr, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// Middleware валидирует Bearer-токен и кладёт Identity в контекст.
// Не проверяет конкретную роль — для этого RequireRole.
func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := bearer(r)
		if raw == "" {
			writeErr(w, http.StatusUnauthorized, "Authorization: Bearer <token> required")
			return
		}
		tok, err := v.parse(raw)
		if err != nil {
			log.Printf("auth: rejected token: %v", err)
			writeErr(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		role, ok := v.resolveRole(tok)
		if !ok {
			writeErr(w, http.StatusForbidden, "no gemini role (admin/operator/viewer) in token")
			return
		}
		id := Identity{Role: role}
		id.Subject, _ = tok.Subject()
		var email string
		if err := tok.Get("email", &email); err == nil {
			id.Email = email
		}
		ctx := context.WithValue(r.Context(), identityKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole — обёртка эндпоинта, требующая роль не ниже min.
func RequireRole(min Role, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := FromContext(r.Context())
		if !ok {
			writeErr(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		if !id.Role.AtLeast(min) {
			writeErr(w, http.StatusForbidden, fmt.Sprintf("role %q required, have %q", min, id.Role))
			return
		}
		h(w, r)
	}
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

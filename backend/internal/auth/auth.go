// Package auth валидирует JWT от Keycloak (OIDC) по JWKS и извлекает права:
// client-роль superuser и роли по проектам из claim groups ("/project/role"). Проверка выполняется на обеих головах
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

// Role — роль пользователя внутри проекта.
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

// roleSuperuser — client-роль Keycloak, дающая полный доступ к инстансу Gemini.
// Остальные роли приходят только из групп (см. parseGroups).
const roleSuperuser = "superuser"

// groupRoles — как имена сегментов группы Keycloak ("/<project>/<role>")
// отображаются в роли Gemini.
var groupRoles = map[string]Role{
	"admin":     RoleAdmin,
	"developer": RoleOperator,
	"operator":  RoleOperator,
	"readonly":  RoleViewer,
	"viewer":    RoleViewer,
}

// Identity — установленная личность запроса.
type Identity struct {
	Subject   string
	Email     string
	Superuser bool
	// Projects — роль пользователя в каждом проекте (из claim groups).
	Projects map[string]Role
}

// Can сообщает, может ли пользователь действовать с ролью не ниже min в проекте.
// Superuser может всё; пустой проект (инстанс без назначенного проекта) —
// только superuser.
func (id Identity) Can(project string, min Role) bool {
	if id.Superuser {
		return true
	}
	if project == "" {
		return false
	}
	r, ok := id.Projects[project]
	return ok && r.AtLeast(min)
}

// CanAnywhere — есть ли роль не ниже min хотя бы в одном проекте.
func (id Identity) CanAnywhere(min Role) bool {
	if id.Superuser {
		return true
	}
	for _, r := range id.Projects {
		if r.AtLeast(min) {
			return true
		}
	}
	return false
}

// ProjectsAtLeast — проекты, где роль не ниже min (для фильтрации выборок).
// Для superuser список не имеет смысла — вызывающий проверяет Superuser сам.
func (id Identity) ProjectsAtLeast(min Role) []string {
	out := make([]string, 0, len(id.Projects))
	for p, r := range id.Projects {
		if r.AtLeast(min) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

type ctxKey int

const identityKey ctxKey = 0

// FromContext достаёт Identity, установленную Middleware.
func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityKey).(Identity)
	return id, ok
}

// WithIdentity кладёт Identity в контекст (Middleware и тесты).
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey, id)
}

// Verifier кеширует JWKS и валидирует токены.
type Verifier struct {
	cfg    config.OIDCConfig
	keyset jwk.Set // самообновляющийся CachedSet
}

// NewVerifier регистрирует JWKS-эндпоинт Keycloak и делает первичную загрузку ключей.
func NewVerifier(ctx context.Context, cfg config.OIDCConfig) (*Verifier, error) {
	jwksURI := cfg.IssuerURL + "/protocol/openid-connect/certs"
	// Без таймаута недоступный Keycloak вешал старт до HTTP-сервера: livez не
	// отвечал, и kubelet молча перезапускал под без единой строки об ошибке.
	cache, err := jwk.NewCache(ctx, httprc.NewClient(
		httprc.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
	))
	if err != nil {
		return nil, fmt.Errorf("jwk cache: %w", err)
	}
	initCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	// Register по умолчанию сам ждёт первую загрузку — тоже под таймаутом.
	if err := cache.Register(initCtx, jwksURI, jwk.WithMinInterval(15*time.Minute)); err != nil {
		return nil, fmt.Errorf("register jwks %s: %w", jwksURI, err)
	}
	if _, err := cache.Refresh(initCtx, jwksURI); err != nil {
		return nil, fmt.Errorf("initial jwks fetch %s: %w", jwksURI, err)
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

// isSuperuser смотрит ТОЛЬКО resource_access.<ClientID>.roles (client-роли
// клиента инстанса Gemini). realm-роли сознательно не учитываются — иначе
// realm-роль "superuser" у realm-администратора давала бы полный доступ в Gemini.
func (v *Verifier) isSuperuser(tok jwt.Token) bool {
	var resourceAccess map[string]any
	if err := tok.Get("resource_access", &resourceAccess); err != nil {
		return false
	}
	client, ok := resourceAccess[v.cfg.ClientID]
	if !ok {
		return false
	}
	return slices.Contains(stringsFrom(client, "roles"), roleSuperuser)
}

// parseGroups разбирает claim groups (Keycloak "Full group path": "/project/role").
// Проект — первый сегмент пути, роль — наивысшая из распознанных среди
// остальных сегментов. Группы без ведущего "/" (fullPath выключен) и группы без
// распознанной роли ничего не дают; при нескольких группах одного проекта
// берётся наивысшая роль.
func parseGroups(groups []string) map[string]Role {
	out := map[string]Role{}
	for _, g := range groups {
		if !strings.HasPrefix(g, "/") {
			continue
		}
		segs := strings.Split(strings.Trim(g, "/"), "/")
		if len(segs) < 2 || segs[0] == "" {
			continue
		}
		var best Role
		for _, seg := range segs[1:] {
			if r, ok := groupRoles[seg]; ok && rank[r] > rank[best] {
				best = r
			}
		}
		if best != "" && rank[best] > rank[out[segs[0]]] {
			out[segs[0]] = best
		}
	}
	return out
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
		var groups []string
		_ = tok.Get("groups", &groups)
		id := Identity{Superuser: v.isSuperuser(tok), Projects: parseGroups(groups)}
		if !id.Superuser && len(id.Projects) == 0 {
			writeErr(w, http.StatusForbidden,
				"no gemini access: need superuser role or membership in a project group (/<project>/<role>)")
			return
		}
		id.Subject, _ = tok.Subject()
		var email string
		if err := tok.Get("email", &email); err == nil {
			id.Email = email
		}
		ctx := WithIdentity(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireSuperuser — обёртка эндпоинта, доступного только superuser.
func RequireSuperuser(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := FromContext(r.Context())
		if !ok {
			writeErr(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		if !id.Superuser {
			writeErr(w, http.StatusForbidden, "superuser role required")
			return
		}
		h(w, r)
	}
}

// RequireAnyProject — грубый гейт: роль не ниже min хотя бы в одном проекте.
// Проверка на конкретный проект/инстанс — в обработчике через Identity.Can.
func RequireAnyProject(min Role, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := FromContext(r.Context())
		if !ok {
			writeErr(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		if !id.CanAnywhere(min) {
			writeErr(w, http.StatusForbidden, fmt.Sprintf("role %q in some project required", min))
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

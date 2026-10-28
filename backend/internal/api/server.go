// Package api собирает HTTP-роутер головы Gemini.
//
// Три класса эндпоинтов:
//   - /api/*      — для людей: OIDC-мидлварь + RBAC (superuser / роль в проекте инстанса);
//   - /api/config — публичный: роль головы для шелла фронта (бейдж на логине);
//   - /peer/*, /webhook/* — для второй головы: HMAC-подпись (peer.Verifier);
//   - /pairing/exchange — публичный (код одноразовый);
//   - /health/livez, /health/readyz — probes; через ingress наружу не проксируются.
package api

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/auth"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/config"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/operator"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/peer"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/storage"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/store"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/vault"
)

// Deps — всё, что нужно обработчикам.
type Deps struct {
	Cfg          *config.Config
	Store        *store.Store
	Operator     *operator.Operator
	Verifier     *auth.Verifier
	PeerVerifier *peer.Verifier
	Peer         *peer.Client
	Vault        *vault.Client
	Storage      *storage.Pool
	Ready        func() error // health readiness-проверка
}

// Server держит зависимости и реализует http.Handler.
type Server struct {
	d Deps
}

// NewRouter собирает mux.Router со всеми маршрутами головы.
func NewRouter(d Deps) http.Handler {
	s := &Server{d: d}
	r := mux.NewRouter()

	r.HandleFunc("/health/livez", s.livez).Methods(http.MethodGet)
	r.HandleFunc("/health/readyz", s.readyz).Methods(http.MethodGet)

	// Публичная конфигурация для неаутентифицированного фронта (бейдж роли на логине).
	// Регистрируется до /api-саброутера, чтобы не попасть под OIDC-мидлварь.
	r.HandleFunc("/api/config", s.publicConfig).Methods(http.MethodGet)

	// --- Эндпоинты для людей ---
	apiR := r.PathPrefix("/api").Subrouter()
	apiR.Use(d.Verifier.Middleware)

	apiR.HandleFunc("/me", s.me).Methods(http.MethodGet)

	apiR.HandleFunc("/projects", auth.RequireSuperuser(s.listProjects)).Methods(http.MethodGet)
	apiR.HandleFunc("/projects", auth.RequireSuperuser(s.createProject)).Methods(http.MethodPost)
	apiR.HandleFunc("/projects/{name}", auth.RequireSuperuser(s.deleteProject)).Methods(http.MethodDelete)
	apiR.HandleFunc("/projects/{name}/managed-by", auth.RequireSuperuser(s.clearProjectManagedBy)).Methods(http.MethodDelete)

	apiR.HandleFunc("/instances", auth.RequireAnyProject(auth.RoleViewer, s.listInstances)).Methods(http.MethodGet)
	apiR.HandleFunc("/instances", auth.RequireSuperuser(s.createInstance)).Methods(http.MethodPost)
	apiR.HandleFunc("/instances/{id}", auth.RequireAnyProject(auth.RoleViewer, s.getInstance)).Methods(http.MethodGet)
	apiR.HandleFunc("/instances/{id}", auth.RequireAnyProject(auth.RoleAdmin, s.updateInstance)).Methods(http.MethodPut)
	apiR.HandleFunc("/instances/{id}", auth.RequireAnyProject(auth.RoleAdmin, s.deleteInstance)).Methods(http.MethodDelete)
	apiR.HandleFunc("/instances/{id}/managed-by", auth.RequireSuperuser(s.clearInstanceManagedBy)).Methods(http.MethodDelete)
	apiR.HandleFunc("/instances/{id}/discover", auth.RequireAnyProject(auth.RoleAdmin, s.discoverInstance)).Methods(http.MethodPost)
	apiR.HandleFunc("/instances/{id}/databases", auth.RequireAnyProject(auth.RoleViewer, s.listDatabases)).Methods(http.MethodGet)

	// Имена полей секрета Vault для сопоставления в UI (значения не отдаются).
	apiR.HandleFunc("/vault/secret-keys", auth.RequireAnyProject(auth.RoleAdmin, s.vaultSecretKeys)).Methods(http.MethodPost)

	apiR.HandleFunc("/databases/{id}", auth.RequireAnyProject(auth.RoleAdmin, s.patchDatabase)).Methods(http.MethodPatch)
	apiR.HandleFunc("/databases/{id}/run", auth.RequireAnyProject(auth.RoleOperator, s.runDatabase)).Methods(http.MethodPost)

	apiR.HandleFunc("/runs", auth.RequireAnyProject(auth.RoleViewer, s.listRuns)).Methods(http.MethodGet)
	apiR.HandleFunc("/runs/active", auth.RequireAnyProject(auth.RoleViewer, s.listActiveRuns)).Methods(http.MethodGet)
	apiR.HandleFunc("/runs/{id}", auth.RequireAnyProject(auth.RoleViewer, s.getRun)).Methods(http.MethodGet)
	apiR.HandleFunc("/runs/{id}/cancel", auth.RequireAnyProject(auth.RoleOperator, s.cancelRun)).Methods(http.MethodPost)

	apiR.HandleFunc("/pairings", auth.RequireSuperuser(s.listPairings)).Methods(http.MethodGet)
	apiR.HandleFunc("/pairings/{id}", auth.RequireSuperuser(s.deletePairing)).Methods(http.MethodDelete)
	apiR.HandleFunc("/pairings/{id}/revoke", auth.RequireSuperuser(s.revokePairing)).Methods(http.MethodPost)

	// Читать дефолты (размер тома, ресурсы) нужно и странице инстанса для подсказок —
	// секретов в настройках нет. Менять — только superuser.
	apiR.HandleFunc("/settings", auth.RequireAnyProject(auth.RoleViewer, s.getSettings)).Methods(http.MethodGet)
	apiR.HandleFunc("/settings", auth.RequireSuperuser(s.putSettings)).Methods(http.MethodPut)

	apiR.HandleFunc("/s3-buckets", auth.RequireSuperuser(s.listS3Buckets)).Methods(http.MethodGet)
	apiR.HandleFunc("/s3-buckets", auth.RequireSuperuser(s.createS3Bucket)).Methods(http.MethodPost)
	apiR.HandleFunc("/s3-buckets/{id}", auth.RequireSuperuser(s.updateS3Bucket)).Methods(http.MethodPut)
	apiR.HandleFunc("/s3-buckets/{id}", auth.RequireSuperuser(s.deleteS3Bucket)).Methods(http.MethodDelete)
	apiR.HandleFunc("/s3-buckets/{id}/test", auth.RequireSuperuser(s.testS3Bucket)).Methods(http.MethodPost)

	if d.Cfg.IsSource() {
		apiR.HandleFunc("/pairing/issue-code", auth.RequireSuperuser(s.issuePairingCode)).Methods(http.MethodPost)
	}
	if d.Cfg.IsTarget() {
		apiR.HandleFunc("/pairing/init", auth.RequireSuperuser(s.initPairing)).Methods(http.MethodPost)
		apiR.HandleFunc("/instances/{id}/roles", auth.RequireAnyProject(auth.RoleAdmin, s.listInstanceRoles)).Methods(http.MethodGet)
		apiR.HandleFunc("/mapping/candidates", auth.RequireAnyProject(auth.RoleViewer, s.mappingCandidates)).Methods(http.MethodGet)
		apiR.HandleFunc("/mapping", auth.RequireAnyProject(auth.RoleViewer, s.listMappings)).Methods(http.MethodGet)
		apiR.HandleFunc("/mapping", auth.RequireAnyProject(auth.RoleAdmin, s.saveMapping)).Methods(http.MethodPost)
		apiR.HandleFunc("/mapping/{id}", auth.RequireAnyProject(auth.RoleAdmin, s.deleteMapping)).Methods(http.MethodDelete)
	}

	// --- Публичный: обмен pairing (одноразовый код) ---
	if d.Cfg.IsSource() {
		r.HandleFunc("/pairing/exchange", s.pairingExchange).Methods(http.MethodPost)
	}

	// --- Эндпоинты для второй головы: HMAC-подпись ---
	peerR := r.PathPrefix("/peer").Subrouter()
	peerR.Use(d.PeerVerifier.Middleware)
	if d.Cfg.IsSource() {
		peerR.HandleFunc("/databases", s.peerListDatabases).Methods(http.MethodGet)
	}

	whR := r.NewRoute().Subrouter()
	whR.Use(d.PeerVerifier.Middleware)
	if d.Cfg.IsTarget() {
		whR.HandleFunc("/webhook/backup-ready", s.webhookBackupReady).Methods(http.MethodPost)
	}
	if d.Cfg.IsSource() {
		whR.HandleFunc("/webhook/restore-status", s.webhookRestoreStatus).Methods(http.MethodPost)
	}

	return logMiddleware(r)
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		// health-пробы бьются каждые несколько секунд — не засоряем лог, если 2xx.
		if strings.HasPrefix(r.URL.Path, "/health/") && sw.status < 400 {
			return
		}
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

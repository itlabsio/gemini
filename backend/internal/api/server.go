// Package api собирает HTTP-роутер головы Gemini.
//
// Три класса эндпоинтов:
//   - /api/*      — для людей: OIDC-мидлварь + проверка роли на каждый обработчик;
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

	apiR.HandleFunc("/instances", auth.RequireRole(auth.RoleViewer, s.listInstances)).Methods(http.MethodGet)
	apiR.HandleFunc("/instances", auth.RequireRole(auth.RoleAdmin, s.createInstance)).Methods(http.MethodPost)
	apiR.HandleFunc("/instances/{id}", auth.RequireRole(auth.RoleViewer, s.getInstance)).Methods(http.MethodGet)
	apiR.HandleFunc("/instances/{id}", auth.RequireRole(auth.RoleAdmin, s.updateInstance)).Methods(http.MethodPut)
	apiR.HandleFunc("/instances/{id}", auth.RequireRole(auth.RoleAdmin, s.deleteInstance)).Methods(http.MethodDelete)
	apiR.HandleFunc("/instances/{id}/discover", auth.RequireRole(auth.RoleAdmin, s.discoverInstance)).Methods(http.MethodPost)
	apiR.HandleFunc("/instances/{id}/databases", auth.RequireRole(auth.RoleViewer, s.listDatabases)).Methods(http.MethodGet)

	// Имена полей секрета Vault для сопоставления в UI (значения не отдаются).
	apiR.HandleFunc("/vault/secret-keys", auth.RequireRole(auth.RoleAdmin, s.vaultSecretKeys)).Methods(http.MethodPost)

	apiR.HandleFunc("/databases/{id}", auth.RequireRole(auth.RoleAdmin, s.patchDatabase)).Methods(http.MethodPatch)
	apiR.HandleFunc("/databases/{id}/run", auth.RequireRole(auth.RoleOperator, s.runDatabase)).Methods(http.MethodPost)

	apiR.HandleFunc("/runs", auth.RequireRole(auth.RoleViewer, s.listRuns)).Methods(http.MethodGet)
	apiR.HandleFunc("/runs/active", auth.RequireRole(auth.RoleViewer, s.listActiveRuns)).Methods(http.MethodGet)
	apiR.HandleFunc("/runs/{id}", auth.RequireRole(auth.RoleViewer, s.getRun)).Methods(http.MethodGet)
	apiR.HandleFunc("/runs/{id}/cancel", auth.RequireRole(auth.RoleOperator, s.cancelRun)).Methods(http.MethodPost)

	apiR.HandleFunc("/pairings", auth.RequireRole(auth.RoleViewer, s.listPairings)).Methods(http.MethodGet)
	apiR.HandleFunc("/pairings/{id}", auth.RequireRole(auth.RoleAdmin, s.deletePairing)).Methods(http.MethodDelete)
	apiR.HandleFunc("/pairings/{id}/revoke", auth.RequireRole(auth.RoleAdmin, s.revokePairing)).Methods(http.MethodPost)

	apiR.HandleFunc("/settings", auth.RequireRole(auth.RoleViewer, s.getSettings)).Methods(http.MethodGet)
	apiR.HandleFunc("/settings", auth.RequireRole(auth.RoleAdmin, s.putSettings)).Methods(http.MethodPut)

	apiR.HandleFunc("/s3-buckets", auth.RequireRole(auth.RoleAdmin, s.listS3Buckets)).Methods(http.MethodGet)
	apiR.HandleFunc("/s3-buckets", auth.RequireRole(auth.RoleAdmin, s.createS3Bucket)).Methods(http.MethodPost)
	apiR.HandleFunc("/s3-buckets/{id}", auth.RequireRole(auth.RoleAdmin, s.updateS3Bucket)).Methods(http.MethodPut)
	apiR.HandleFunc("/s3-buckets/{id}", auth.RequireRole(auth.RoleAdmin, s.deleteS3Bucket)).Methods(http.MethodDelete)
	apiR.HandleFunc("/s3-buckets/{id}/test", auth.RequireRole(auth.RoleAdmin, s.testS3Bucket)).Methods(http.MethodPost)

	if d.Cfg.IsSource() {
		apiR.HandleFunc("/pairing/issue-code", auth.RequireRole(auth.RoleAdmin, s.issuePairingCode)).Methods(http.MethodPost)
	}
	if d.Cfg.IsTarget() {
		apiR.HandleFunc("/pairing/init", auth.RequireRole(auth.RoleAdmin, s.initPairing)).Methods(http.MethodPost)
		apiR.HandleFunc("/instances/{id}/roles", auth.RequireRole(auth.RoleAdmin, s.listInstanceRoles)).Methods(http.MethodGet)
		apiR.HandleFunc("/mapping/candidates", auth.RequireRole(auth.RoleViewer, s.mappingCandidates)).Methods(http.MethodGet)
		apiR.HandleFunc("/mapping", auth.RequireRole(auth.RoleViewer, s.listMappings)).Methods(http.MethodGet)
		apiR.HandleFunc("/mapping", auth.RequireRole(auth.RoleAdmin, s.saveMapping)).Methods(http.MethodPost)
	}

	// --- Публичный: обмен pairing (одноразовый код) ---
	if d.Cfg.IsSource() {
		r.HandleFunc("/pairing/exchange", s.pairingExchange).Methods(http.MethodPost)
	}

	// --- Эндпоинты для второй головы: HMAC-подпись ---
	peerR := r.PathPrefix("/peer").Subrouter()
	peerR.Use(d.PeerVerifier.Middleware)
	if d.Cfg.IsSource() {
		peerR.HandleFunc("/databases", s.peerListEnabledDatabases).Methods(http.MethodGet)
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

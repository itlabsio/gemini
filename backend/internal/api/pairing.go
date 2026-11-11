package api

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/itlabsio/gemini/backend/internal/store"
)

const (
	pairingCodeTTL = 15 * time.Minute
	// pairingCodeMinInterval — рейт-лимит на выдачу кода (одна кнопка в UI).
	pairingCodeMinInterval = time.Minute
)

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// issuePairingCode (Source, admin): создаёт pending-запись и отдаёт одноразовый код.
func (s *Server) issuePairingCode(w http.ResponseWriter, r *http.Request) {
	last, ok, err := s.d.Store.LastPairingCodeIssuedAt(r.Context(), "target")
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if ok {
		if wait := pairingCodeMinInterval - time.Since(last); wait > 0 {
			secs := int(wait.Round(time.Second).Seconds())
			if secs < 1 {
				secs = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			writeJSON(w, http.StatusTooManyRequests, map[string]any{
				"error":               "код уже выдан; следующий можно создать через " + strconv.Itoa(secs) + " с",
				"retry_after_seconds": secs,
			})
			return
		}
	}

	code := randHex(20)
	if _, err := s.d.Store.CreatePairingCode(r.Context(), "target", sha256Hex(code), pairingCodeTTL); err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "pairing.issue-code", "target", nil)
	writeJSON(w, http.StatusCreated, map[string]any{
		"code":               code,
		"expires_in_seconds": int(pairingCodeTTL.Seconds()),
		"note":               "введите этот код и URL Source-инстанса на стороне DR; код одноразовый",
	})
}

// deletePairing (admin): убирает связь из списка. Активную удалять нельзя.
// Протухшие pending чистятся автоматически при листинге (см. listPairings),
// этот эндпоинт — для ручного удаления revoked/pending-записей.
func (s *Server) deletePairing(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if err := s.d.Store.DeletePairing(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "нельзя удалить активную связь")
			return
		}
		writeStoreError(w, err)
		return
	}
	s.audit(r, "pairing.delete", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

// revokePairing (admin): разрывает связь (active/pending → revoked). Пир не
// уведомляется — после ревокации его подписанные запросы отбиваются по HMAC.
// Для повторного соединения нужен новый pairing с обеих сторон.
func (s *Server) revokePairing(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	p, err := s.d.Store.RevokePairing(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "связь не найдена или уже разорвана")
			return
		}
		writeStoreError(w, err)
		return
	}
	s.audit(r, "pairing.revoke", id, nil)
	writeJSON(w, http.StatusOK, p)
}

type pairingInitRequest struct {
	SourceURL string `json:"source_url"`
	Code      string `json:"code"`
}

type pairingExchangeRequest struct {
	Code      string `json:"code"`
	PeerURL   string `json:"peer_url"`
	Secret    string `json:"secret"`
	PublicKey string `json:"public_key"` // base64 Ed25519 публичный ключ Target
}

type pairingExchangeResponse struct {
	SourcePublicURL string `json:"source_public_url"`
	PublicKey       string `json:"public_key"` // base64 Ed25519 публичный ключ Source
}

// initPairing (DR, admin): вводит URL Source + код, генерирует общий HMAC-секрет,
// обменивается с Source и фиксирует активную пару с обеих сторон.
func (s *Server) initPairing(w http.ResponseWriter, r *http.Request) {
	var req pairingInitRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	sourceURL := strings.TrimSuffix(strings.TrimSpace(req.SourceURL), "/")
	if sourceURL == "" || req.Code == "" {
		writeError(w, http.StatusBadRequest, "source_url and code are required")
		return
	}
	if !httpsURL(sourceURL) {
		writeError(w, http.StatusBadRequest, "source_url must use https")
		return
	}
	if s.d.Cfg.PublicURL == "" {
		writeError(w, http.StatusPreconditionFailed, "PUBLIC_URL is not set on this DR head; Source needs it for the reverse webhook")
		return
	}

	ownPub, err := s.d.Store.EnsureHeadIdentity(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}

	secret := randHex(32)
	exResp, err := exchangeWithSource(r.Context(), sourceURL, pairingExchangeRequest{
		Code:      req.Code,
		PeerURL:   s.d.Cfg.PublicURL,
		Secret:    secret,
		PublicKey: base64.StdEncoding.EncodeToString(ownPub),
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "exchange with Source failed: "+err.Error())
		return
	}
	if exResp.PublicKey == "" {
		writeError(w, http.StatusBadGateway, "Source did not return its public key")
		return
	}

	p, err := s.d.Store.UpsertActivePairingWithSource(r.Context(), sourceURL, secret, exResp.PublicKey)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "pairing.init", sourceURL, nil)
	writeJSON(w, http.StatusOK, p)
}

type offlinePairingRequest struct {
	PublicKey string `json:"public_key"` // base64 Ed25519 публичный ключ Source
}

// initOfflinePairing (Target, superuser) — оффлайн-пара: запоминается только
// публичный ключ Source, сохранённый заранее вне Gemini (Source показывает его
// на странице pairing). Ни URL, ни HMAC-секрета: webhook'и от Source не
// принимаются (верификатор пропускает пары без секрета), restore-status не
// шлётся, а дампы и сопоставления берутся из бакета (см. fetchCandidates). Всё,
// что восстанавливается, по-прежнему проверяется подписью этим ключом.
//
// Существующую активную пару не заменяет: её надо явно разорвать, чтобы
// случайный ввод ключа не перебил рабочую связь. Обычный pairing позже
// заменит оффлайн-пару сам.
func (s *Server) initOfflinePairing(w http.ResponseWriter, r *http.Request) {
	var req offlinePairingRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.PublicKey))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		writeError(w, http.StatusBadRequest, "public_key must be a base64 Ed25519 public key (32 bytes)")
		return
	}
	if _, err := s.d.Store.ActivePairing(r.Context(), "source"); err == nil {
		writeError(w, http.StatusConflict, "an active pairing with Source already exists — revoke it first")
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		writeStoreError(w, err)
		return
	}
	key := base64.StdEncoding.EncodeToString(raw)
	p, err := s.d.Store.UpsertActivePairingWithSource(r.Context(), "", "", key)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	sum := sha256.Sum256(raw)
	s.audit(r, "pairing.offline", p.ID, map[string]any{"public_key": key, "sha256": hex.EncodeToString(sum[:])})
	writeJSON(w, http.StatusOK, p)
}

// pairingExchange (Source, публичный): принимает одноразовый код от DR,
// сохраняет URL DR и общий секрет, активирует пару.
func (s *Server) pairingExchange(w http.ResponseWriter, r *http.Request) {
	var req pairingExchangeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Code == "" || req.PeerURL == "" || req.Secret == "" || req.PublicKey == "" {
		writeError(w, http.StatusBadRequest, "code, peer_url, secret and public_key are required")
		return
	}
	if !httpsURL(req.PeerURL) {
		writeError(w, http.StatusBadRequest, "peer_url must use https")
		return
	}
	ownPub, err := s.d.Store.EnsureHeadIdentity(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	p, err := s.d.Store.PendingPairingByCodeHash(r.Context(), sha256Hex(req.Code))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid or expired pairing code")
			return
		}
		writeStoreError(w, err)
		return
	}
	if _, err := s.d.Store.ActivatePairing(r.Context(), p.ID,
		strings.TrimSuffix(req.PeerURL, "/"), req.Secret, req.PublicKey); err != nil {
		if errors.Is(err, store.ErrNotFound) { // код уже погасил параллельный запрос
			writeError(w, http.StatusUnauthorized, "invalid or expired pairing code")
			return
		}
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pairingExchangeResponse{
		SourcePublicURL: s.d.Cfg.PublicURL,
		PublicKey:       base64.StdEncoding.EncodeToString(ownPub),
	})
}

func exchangeWithSource(ctx context.Context, sourceURL string, body pairingExchangeRequest) (*pairingExchangeResponse, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sourceURL+"/pairing/exchange", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Без редиректов: 307/308 переслал бы тело с HMAC-секретом на чужой адрес.
	client := &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(strings.TrimSpace(string(data)))
	}
	var out pairingExchangeResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// httpsURL — URL пира только по TLS: по нему уходит общий HMAC-секрет, а потом
// webhook'и с подписью.
func httpsURL(u string) bool {
	return strings.HasPrefix(strings.ToLower(u), "https://")
}

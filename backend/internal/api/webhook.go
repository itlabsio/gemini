package api

import (
	"errors"
	"log"
	"net/http"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/notify"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/operator"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/peer"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/store"
)

type backupReadyRequest struct {
	SourceDatabaseExternalID string `json:"source_database_external_id"`
	SourceDBName             string `json:"source_db_name"`
	S3ObjectKey              string `json:"s3_object_key"`
	SHA256                   string `json:"sha256"`
	Timestamp                string `json:"timestamp"`
}

// webhookBackupReady (DR, HMAC): Source сообщает, что дамп залит и готов.
// Обработчик идемпотентен по event_key; если сопоставления нет — событие
// молча игнорируется (Source не должен ретраить бесконечно).
func (s *Server) webhookBackupReady(w http.ResponseWriter, r *http.Request) {
	var req backupReadyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.SourceDatabaseExternalID == "" || req.S3ObjectKey == "" {
		writeError(w, http.StatusBadRequest, "source_database_external_id and s3_object_key are required")
		return
	}

	m, err := s.d.Store.MappingBySourceExternalID(r.Context(), req.SourceDatabaseExternalID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "no mapping for this source database"})
			return
		}
		writeStoreError(w, err)
		return
	}

	// Ключ должен лежать внутри префикса ЭТОГО сопоставления: пир называет базу,
	// но не выбирает, какой объект бакета мы применим.
	if !keyUnderPrefix(req.S3ObjectKey, m.S3Prefix) {
		log.Printf("api: backup-ready rejected: key %q outside mapping prefix %q", req.S3ObjectKey, m.S3Prefix)
		writeError(w, http.StatusBadRequest, "s3_object_key is outside the mapping s3_prefix")
		return
	}

	eventKey := operator.EventKeyFor("restore", req.S3ObjectKey)
	if existing, err := s.d.Store.RunByEventKey(r.Context(), eventKey); err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "duplicate", "run_id": existing.ID})
		return
	}

	// Бакеты у Target свои: ищем тот, где дамп уже полностью залит. Не нашли —
	// 503, Source повторит с backoff'ом, а дальше подхватит fallback-поллинг.
	ready := s.d.Storage.ReadyClients(r.Context(), req.S3ObjectKey)
	if len(ready) == 0 {
		writeError(w, http.StatusServiceUnavailable, "dump not found in any S3 bucket configured on this side")
		return
	}

	run, err := s.d.Operator.StartRestore(r.Context(), m, req.S3ObjectKey, firstField(req.SHA256),
		ready[0].ID(), model.TriggerWebhook, eventKey)
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "restore-started", "run_id": run.ID})
	case errors.Is(err, store.ErrConflict):
		writeJSON(w, http.StatusOK, map[string]string{"status": "already-running"})
	case errors.Is(err, operator.ErrNoK8s):
		writeError(w, http.StatusServiceUnavailable, "kubernetes client not configured")
	default:
		writeStoreError(w, err)
	}
}

type restoreStatusRequest struct {
	SourceDatabaseExternalID string `json:"source_database_external_id"`
	S3ObjectKey              string `json:"s3_object_key"`
	Status                   string `json:"status"`
	Timestamp                string `json:"timestamp"`
	ErrorMessage             string `json:"error_message"`
}

// webhookRestoreStatus (Source, HMAC): DR сообщает исход применения дампа.
func (s *Server) webhookRestoreStatus(w http.ResponseWriter, r *http.Request) {
	var req restoreStatusRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.S3ObjectKey == "" || req.Status == "" {
		writeError(w, http.StatusBadRequest, "s3_object_key and status are required")
		return
	}
	// Статус пира едет прямиком в колонку и потом в UI — принимаем только
	// известные значения, а не произвольную строку от второй головы.
	switch model.RunStatus(req.Status) {
	case model.StatusPending, model.StatusRunning, model.StatusSucceeded, model.StatusFailed:
	default:
		writeError(w, http.StatusBadRequest, "status must be one of: pending, running, succeeded, failed")
		return
	}
	// id пары установил verifier по совпавшему HMAC-секрету — так Source знает,
	// какой именно Target отчитался (их может быть несколько).
	pairingID := peer.PeerPairingID(r.Context())
	if pairingID == "" {
		writeError(w, http.StatusUnauthorized, "unresolved peer pairing")
		return
	}
	peerURL := ""
	if p, perr := s.d.Store.GetPairing(r.Context(), pairingID); perr == nil {
		peerURL = p.PeerURL
	}
	err := s.d.Store.SetPeerStatus(r.Context(), pairingID, peerURL, req.S3ObjectKey, req.Status, notify.Sanitize(req.ErrorMessage))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// dump-прогон мог быть уже очищен по retention — не ошибка доставки
			writeJSON(w, http.StatusOK, map[string]string{"status": "accepted", "note": "matching dump run not found"})
			return
		}
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

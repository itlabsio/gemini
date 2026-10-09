package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/store"
)

// candidate — база Source, доступная для сопоставления (без секретов).
type candidate struct {
	ExternalID string `json:"external_id"`
	DBName     string `json:"db_name"`
	// S3Prefix — префикс, под который Source кладёт дампы этой базы
	// (<имя инстанса>/<имя базы>). Target подставляет его в сопоставление,
	// чтобы администратору не приходилось угадывать раскладку бакета.
	S3Prefix string `json:"s3_prefix"`
}

// peerListEnabledDatabases (Source, HMAC): отдаёт DR список enabled-баз.
// Наружу уходят external_id, имя базы и префикс дампа в S3 — никаких кред,
// host'ов, паролей.
func (s *Server) peerListEnabledDatabases(w http.ResponseWriter, r *http.Request) {
	dbs, err := s.d.Store.ListEnabledDatabases(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	instName := map[string]string{}
	out := make([]candidate, 0, len(dbs))
	for _, d := range dbs {
		name, ok := instName[d.InstanceID]
		if !ok {
			inst, err := s.d.Store.GetInstance(r.Context(), d.InstanceID)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			name = inst.Name
			instName[d.InstanceID] = name
		}
		out = append(out, candidate{
			ExternalID: d.ExternalID,
			DBName:     d.DBName,
			// см. operator.ensureCronJob: dump-Job получает S3_PREFIX = inst.Name/db.DBName
			S3Prefix: name + "/" + d.DBName,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// mappingCandidates (DR): запрашивает у Source список enabled-баз через peer-клиент.
func (s *Server) mappingCandidates(w http.ResponseWriter, r *http.Request) {
	p, err := s.d.Store.ActivePairing(r.Context(), "source")
	if err != nil {
		writeError(w, http.StatusPreconditionFailed, "no active pairing with Source; run pairing first")
		return
	}
	secret, err := s.d.Store.PairingSecret(r.Context(), "source")
	if err != nil || secret == "" {
		writeError(w, http.StatusPreconditionFailed, "pairing secret unavailable")
		return
	}
	body, err := s.d.Peer.Get(r.Context(), p.PeerURL, "/peer/databases", secret)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to query Source: "+err.Error())
		return
	}
	var cands []candidate
	if err := json.Unmarshal(body, &cands); err != nil {
		writeError(w, http.StatusBadGateway, "bad response from Source")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"pairing_id": p.ID,
		"candidates": cands,
	})
}

// validRoleName — консервативная проверка имени роли PostgreSQL: непусто,
// ≤63 байт (NAMEDATALEN-1), без управляющих символов и кавычек. Дальше имя
// экранируется quoteIdent в restore-Job'е.
func validRoleName(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == '"' {
			return false
		}
	}
	return true
}

func (s *Server) listMappings(w http.ResponseWriter, r *http.Request) {
	items, err := s.d.Store.ListMappings(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

type saveMappingRequest struct {
	HeadPairingID            string `json:"head_pairing_id"`
	SourceDatabaseExternalID string `json:"source_database_external_id"`
	SourceDBName             string `json:"source_db_name"`
	TargetDatabaseID         string `json:"target_database_id"`
	TargetOwner              string `json:"target_owner"`
	S3Prefix                 string `json:"s3_prefix"`
	Enabled                  *bool  `json:"enabled"`
}

func (s *Server) saveMapping(w http.ResponseWriter, r *http.Request) {
	var req saveMappingRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.HeadPairingID == "" || req.SourceDatabaseExternalID == "" || req.TargetDatabaseID == "" {
		writeError(w, http.StatusBadRequest, "head_pairing_id, source_database_external_id and target_database_id are required")
		return
	}
	// target_owner обязателен: без него restore идёт под суперюзером и
	// восстановленная база принадлежала бы ему. Роль создаётся вне Gemini.
	owner := strings.TrimSpace(req.TargetOwner)
	if !validRoleName(owner) {
		writeError(w, http.StatusBadRequest,
			"target_owner is required — pre-created role on the target cluster that will own the restored database")
		return
	}
	// Пустой префикс раньше означал: поллер такое сопоставление пропускает, а
	// webhook принимает ЛЮБОЙ ключ бакета. Теперь префикс обязателен — он же
	// граница доверия при проверке s3_object_key.
	if strings.TrimSuffix(strings.TrimSpace(req.S3Prefix), "/") == "" {
		writeError(w, http.StatusBadRequest, "s3_prefix is required (it bounds which dumps may be restored into this database)")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	id, _ := ctxIdentity(r)
	m, err := s.d.Store.UpsertMapping(r.Context(), store.MappingInput{
		HeadPairingID:            req.HeadPairingID,
		SourceDatabaseExternalID: req.SourceDatabaseExternalID,
		SourceDBName:             req.SourceDBName,
		TargetDatabaseID:         req.TargetDatabaseID,
		TargetOwner:              owner,
		S3Prefix:                 strings.TrimSuffix(strings.TrimSpace(req.S3Prefix), "/"),
		Enabled:                  enabled,
	}, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "mapping.save", m.ID, nil)
	writeJSON(w, http.StatusOK, m)
}

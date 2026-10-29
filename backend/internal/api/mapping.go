package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/auth"
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
	// Enabled — включён ли бэкап базы на Source. Только для информации в UI:
	// восстанавливать или нет, решает галка целевой базы на Target.
	Enabled bool `json:"enabled"`
	// Project — проект инстанса Source, которому принадлежит база. Target
	// сверяет его с проектом своего инстанса (RBAC) и не доверяет ответу без поля.
	Project string `json:"project"`
}

// peerListDatabases (Source, HMAC): отдаёт Target список присутствующих баз
// проекта ?project= (включая выключенные — сопоставить их можно заранее). Наружу
// уходят external_id, имя базы и префикс дампа в S3 — никаких кред, host'ов, паролей.
// Проект обязателен: Source не отдаёт базы чужих проектов.
func (s *Server) peerListDatabases(w http.ResponseWriter, r *http.Request) {
	project := strings.TrimSpace(r.URL.Query().Get("project"))
	if project == "" {
		writeError(w, http.StatusBadRequest, "project is required")
		return
	}
	dbs, err := s.d.Store.ListPresentDatabases(r.Context(), project)
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
			Enabled:  d.Enabled,
			Project:  project,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// fetchCandidates запрашивает у Source базы проекта через peer-клиент. При
// ошибке сам пишет ответ и возвращает ok=false. Кандидаты с чужим project
// отбрасываются (защита от Source, который игнорирует фильтр).
func (s *Server) fetchCandidates(w http.ResponseWriter, r *http.Request, project string) (pairingID string, cands []candidate, ok bool) {
	p, err := s.d.Store.ActivePairing(r.Context(), "source")
	if err != nil {
		writeError(w, http.StatusPreconditionFailed, "no active pairing with Source; run pairing first")
		return "", nil, false
	}
	secret, err := s.d.Store.PairingSecret(r.Context(), "source")
	if err != nil || secret == "" {
		writeError(w, http.StatusPreconditionFailed, "pairing secret unavailable")
		return "", nil, false
	}
	body, err := s.d.Peer.Get(r.Context(), p.PeerURL, "/peer/databases?project="+url.QueryEscape(project), secret)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to query Source: "+err.Error())
		return "", nil, false
	}
	var all []candidate
	if err := json.Unmarshal(body, &all); err != nil {
		writeError(w, http.StatusBadGateway, "bad response from Source")
		return "", nil, false
	}
	cands = make([]candidate, 0, len(all))
	for _, c := range all {
		if c.Project == project {
			cands = append(cands, c)
		}
	}
	return p.ID, cands, true
}

// mappingCandidates (Target): базы Source того же проекта (?project=). Нужна
// роль viewer в этом проекте; superuser может любой.
func (s *Server) mappingCandidates(w http.ResponseWriter, r *http.Request) {
	project := strings.TrimSpace(r.URL.Query().Get("project"))
	if project == "" {
		writeError(w, http.StatusBadRequest, "project is required")
		return
	}
	if !identity(r).Can(project, auth.RoleViewer) {
		writeForbidden(w, project, auth.RoleViewer)
		return
	}
	pairingID, cands, ok := s.fetchCandidates(w, r, project)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"pairing_id": pairingID,
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

// databaseProject возвращает проект инстанса базы (cache — по database id).
func (s *Server) databaseProject(ctx context.Context, databaseID string, cache map[string]string) (string, error) {
	if p, ok := cache[databaseID]; ok {
		return p, nil
	}
	db, err := s.d.Store.GetDatabase(ctx, databaseID)
	if err != nil {
		return "", err
	}
	inst, err := s.d.Store.GetInstance(ctx, db.InstanceID)
	if err != nil {
		return "", err
	}
	cache[databaseID] = inst.Project
	return inst.Project, nil
}

func (s *Server) listMappings(w http.ResponseWriter, r *http.Request) {
	items, err := s.d.Store.ListMappings(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	// Видны только сопоставления баз тех инстансов, к которым есть доступ.
	id := identity(r)
	visible := items[:0]
	cache := map[string]string{}
	for _, m := range items {
		project, err := s.databaseProject(r.Context(), m.TargetDatabaseID, cache)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				project = ""
			} else {
				writeStoreError(w, err)
				return
			}
		}
		if id.Can(project, auth.RoleViewer) {
			visible = append(visible, m)
		}
	}
	writeJSON(w, http.StatusOK, visible)
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
	// RBAC: админ проекта целевого инстанса; база Source — из того же проекта.
	_, inst, ok := s.authorizeDatabase(w, r, req.TargetDatabaseID, auth.RoleAdmin)
	if !ok {
		return
	}
	if inst.Project == "" {
		writeError(w, http.StatusConflict, "target instance is not assigned to a project: assign one before mapping")
		return
	}
	pairingID, cands, ok := s.fetchCandidates(w, r, inst.Project)
	if !ok {
		return
	}
	if pairingID != req.HeadPairingID {
		writeError(w, http.StatusBadRequest, "head_pairing_id does not match the active pairing with Source")
		return
	}
	var cand *candidate
	for i := range cands {
		if cands[i].ExternalID == req.SourceDatabaseExternalID {
			cand = &cands[i]
			break
		}
	}
	if cand == nil {
		writeError(w, http.StatusForbidden, "source database is not in project "+inst.Project+" on Source")
		return
	}
	// Префикс границы доверия: админ проекта не должен подставить префикс дампов
	// чужого проекта. Superuser может задать произвольный.
	prefix := strings.TrimSuffix(strings.TrimSpace(req.S3Prefix), "/")
	if !identity(r).Superuser && prefix != cand.S3Prefix {
		writeError(w, http.StatusForbidden, "s3_prefix must equal the dump prefix of the source database ("+cand.S3Prefix+")")
		return
	}
	// Upsert идёт по (pairing, source id): не даём перепривязать сопоставление,
	// чья целевая база лежит в недоступном пользователю проекте.
	if old, err := s.d.Store.MappingByPairingExternal(r.Context(), req.HeadPairingID, req.SourceDatabaseExternalID); err == nil {
		oldProject, perr := s.databaseProject(r.Context(), old.TargetDatabaseID, map[string]string{})
		if perr != nil && !errors.Is(perr, store.ErrNotFound) {
			writeStoreError(w, perr)
			return
		}
		if !identity(r).Can(oldProject, auth.RoleAdmin) {
			writeForbidden(w, oldProject, auth.RoleAdmin)
			return
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		writeStoreError(w, err)
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
		S3Prefix:                 prefix,
		Enabled:                  enabled,
	}, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "mapping.save", m.ID, nil)
	writeJSON(w, http.StatusOK, m)
}

// deleteMapping разрывает связь source-базы с целевой: webhook и поллер
// перестают запускать restore в эту базу. Сама целевая база не трогается.
func (s *Server) deleteMapping(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	m, err := s.d.Store.GetMapping(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	project, err := s.databaseProject(r.Context(), m.TargetDatabaseID, map[string]string{})
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeStoreError(w, err)
		return
	}
	if !identity(r).Can(project, auth.RoleAdmin) {
		writeForbidden(w, project, auth.RoleAdmin)
		return
	}
	if err := s.d.Store.DeleteMapping(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "mapping.delete", id, map[string]any{"source_db_name": m.SourceDBName})
	w.WriteHeader(http.StatusNoContent)
}

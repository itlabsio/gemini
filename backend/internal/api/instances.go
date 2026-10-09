package api

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/gorilla/mux"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/auth"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/discovery"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/store"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/vault"
)

// validSSLModes — режимы libpq, которые голова готова прокидывать в DSN/PGSSLMODE.
var validSSLModes = []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}

// instanceNameRe ограничивает имя инстанса: оно едет в S3-префикс дампов
// (inst.Name + "/" + db.DBName) и в лейблы k8s. Слеши, пробелы и ".." сделали бы
// префикс одного инстанса вложенным в чужой, а длина >63 не влезает в лейбл.
// Уникальность имени гарантирует constraint instances_name_uniq в БД.
var instanceNameRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9_-]{0,61}[a-zA-Z0-9])?$`)

// validateRootCert проверяет, что переданный текст — валидный PEM с одним или
// несколькими сертификатами. Иначе он молча уехал бы в PGSSLROOTCERT Job'а, где
// libpq отвалился бы уже в кластере с невнятной ошибкой.
func validateRootCert(pemText string) error {
	rest := []byte(pemText)
	n := 0
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			return fmt.Errorf("unexpected PEM block %q, want CERTIFICATE", block.Type)
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return fmt.Errorf("parse certificate: %w", err)
		}
		n++
	}
	if n == 0 {
		return fmt.Errorf("no PEM CERTIFICATE block found")
	}
	return nil
}

type instanceRequest struct {
	Role          model.InstanceRole `json:"role"`
	Name          string             `json:"name"`
	Host          string             `json:"host"`
	Port          int                `json:"port"`
	RestoreMode   model.RestoreMode  `json:"restore_mode"`
	SSLMode       string             `json:"ssl_mode"`
	SSLRootCert   string             `json:"ssl_root_cert"`
	DiscoveryDB   string             `json:"discovery_db"`
	AuthType      model.AuthType     `json:"auth_type"`
	PlainUsername string             `json:"plain_username"`
	PlainPassword string             `json:"plain_password"`
	VaultPath     string             `json:"vault_path"`
	VaultRole     string             `json:"vault_role"`
	// Какие поля секрета считать логином и паролем. Пусто → эвристика
	// по известным именам (username/user/login, password/pass/pwd).
	VaultUsernameKey string `json:"vault_username_key"`
	VaultPasswordKey string `json:"vault_password_key"`
	// Базы, которые discovery игнорирует (сверх всегда пропускаемой postgres).
	ExcludedDatabases []string `json:"excluded_databases"`
}

// cleanDBNames нормализует список исключённых баз: тримит, отбрасывает пустые и
// дубли, сохраняя порядок.
func cleanDBNames(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, n := range in {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func (req instanceRequest) validate(defaultRole model.InstanceRole) (store.InstanceInput, string) {
	in := store.InstanceInput{
		Role:          req.Role,
		Name:          strings.TrimSpace(req.Name),
		Host:          strings.TrimSpace(req.Host),
		Port:          req.Port,
		RestoreMode:   req.RestoreMode,
		SSLMode:       req.SSLMode,
		SSLRootCert:   strings.TrimSpace(req.SSLRootCert),
		DiscoveryDB:   strings.TrimSpace(req.DiscoveryDB),
		AuthType:      req.AuthType,
		PlainUsername: req.PlainUsername,
		PlainPassword: req.PlainPassword,
		VaultPath:     strings.TrimSpace(req.VaultPath),
		VaultRole:     strings.TrimSpace(req.VaultRole),

		VaultUsernameKey: strings.TrimSpace(req.VaultUsernameKey),
		VaultPasswordKey: strings.TrimSpace(req.VaultPasswordKey),

		ExcludedDatabases: cleanDBNames(req.ExcludedDatabases),
	}
	if in.Role == "" {
		in.Role = defaultRole
	}
	if in.Name == "" || in.Host == "" {
		return in, "name and host are required"
	}
	if !instanceNameRe.MatchString(in.Name) {
		return in, "name must be 1-63 chars of [a-zA-Z0-9_-], starting and ending with a letter or digit " +
			"(it becomes the S3 prefix of this instance's dumps)"
	}
	// Allowlist libpq-режимов: опечатка ("verify_full") иначе молча уезжала в DSN
	// и в PGSSLMODE Job'а, где libpq деградировал бы до отсутствия проверки.
	// Пусто → store подставит "require".
	if in.SSLMode != "" && !slices.Contains(validSSLModes, in.SSLMode) {
		return in, "ssl_mode must be one of: " + strings.Join(validSSLModes, ", ")
	}
	if in.RestoreMode != "" &&
		in.RestoreMode != model.RestoreRecreate && in.RestoreMode != model.RestoreInPlace {
		return in, "restore_mode must be 'recreate' or 'in_place'"
	}
	// Кастомный CA имеет смысл только когда libpq действительно проверяет цепочку.
	// При остальных режимах он просто не применяется — не храним мусор.
	if in.SSLMode != "verify-ca" && in.SSLMode != "verify-full" {
		in.SSLRootCert = ""
	}
	if in.SSLRootCert != "" {
		if err := validateRootCert(in.SSLRootCert); err != nil {
			return in, "ssl_root_cert: " + err.Error()
		}
	}
	switch in.AuthType {
	case model.AuthPlain:
		if in.PlainUsername == "" {
			return in, "plain_username is required for auth_type=plain"
		}
	case model.AuthVault:
		if in.VaultPath == "" {
			return in, "vault_path is required for auth_type=vault"
		}
		// Маппинг задаётся либо целиком, либо никак: половинчатый (только логин)
		// молча уводил бы пароль в эвристику — неочевидно и легко не заметить.
		if (in.VaultUsernameKey == "") != (in.VaultPasswordKey == "") {
			return in, "vault_username_key and vault_password_key must be set together (or both left empty for auto-detection)"
		}
	default:
		return in, "auth_type must be 'plain' or 'vault'"
	}
	return in, ""
}

// defaultInstanceRole — роль инстанса по умолчанию соответствует роли головы.
func (s *Server) defaultInstanceRole() model.InstanceRole {
	if s.d.Cfg.IsTarget() {
		return model.InstanceTarget
	}
	return model.InstanceSource
}

// redactVaultCoords прячет координаты секрета в Vault от роли viewer: путь и роль
// описывают устройство секрет-хранилища, наблюдателю журналов они не нужны.
// operator/admin видят их — им это нужно для диагностики и правки инстанса.
func redactVaultCoords(r *http.Request, inst *model.Instance) {
	id, _ := auth.FromContext(r.Context())
	if id.Role.AtLeast(auth.RoleOperator) {
		return
	}
	inst.VaultPath = ""
	inst.VaultRole = ""
	inst.VaultUsernameKey = ""
	inst.VaultPasswordKey = ""
}

func (s *Server) listInstances(w http.ResponseWriter, r *http.Request) {
	items, err := s.d.Store.ListInstances(r.Context(), "")
	if err != nil {
		writeStoreError(w, err)
		return
	}
	for i := range items {
		redactVaultCoords(r, &items[i])
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) getInstance(w http.ResponseWriter, r *http.Request) {
	inst, err := s.d.Store.GetInstance(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeStoreError(w, err)
		return
	}
	redactVaultCoords(r, inst)
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) createInstance(w http.ResponseWriter, r *http.Request) {
	var req instanceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in, msg := req.validate(s.defaultInstanceRole())
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if in.AuthType == model.AuthVault {
		if err := s.checkVault(r.Context(), in); err != nil {
			writeError(w, http.StatusBadRequest, "vault check failed: "+err.Error())
			return
		}
	}
	id, _ := auth.FromContext(r.Context())
	inst, err := s.d.Store.CreateInstance(r.Context(), in, id.Email)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "instance.create", inst.ID, nil)
	writeJSON(w, http.StatusCreated, inst)
}

func (s *Server) updateInstance(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req instanceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in, msg := req.validate(s.defaultInstanceRole())
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if in.AuthType == model.AuthVault {
		if err := s.checkVault(r.Context(), in); err != nil {
			writeError(w, http.StatusBadRequest, "vault check failed: "+err.Error())
			return
		}
	}
	inst, err := s.d.Store.UpdateInstance(r.Context(), id, in)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "instance.update", id, nil)
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) deleteInstance(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if err := s.d.Store.DeleteInstance(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "instance.delete", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

// discoverInstance подключается к инстансу, читает список баз и синхронизирует
// их с таблицей databases.
func (s *Server) discoverInstance(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	inst, err := s.d.Store.GetInstance(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	target, err := s.resolveDiscoveryTarget(r.Context(), inst)
	if err != nil {
		writeError(w, http.StatusBadGateway, "resolve credentials: "+err.Error())
		return
	}
	res, err := discovery.ListDatabases(r.Context(), *target)
	if err != nil {
		writeError(w, http.StatusBadGateway, "discovery failed: "+err.Error())
		return
	}
	names := discovery.Filter(res.Databases, inst.ExcludedDatabases)
	dbs, err := s.d.Store.SyncDiscovered(r.Context(), id, names, res.ServerVersionNum)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "instance.discover", id, map[string]any{
		"found": len(names), "server_version_num": res.ServerVersionNum,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"databases":          dbs,
		"server_version_num": res.ServerVersionNum,
	})
}

// listInstanceRoles (target, admin) подключается к инстансу и отдаёт имена ролей
// кластера — кандидаты на владельца восстановленной базы в UI сопоставления.
func (s *Server) listInstanceRoles(w http.ResponseWriter, r *http.Request) {
	inst, err := s.d.Store.GetInstance(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeStoreError(w, err)
		return
	}
	target, err := s.resolveDiscoveryTarget(r.Context(), inst)
	if err != nil {
		writeError(w, http.StatusBadGateway, "resolve credentials: "+err.Error())
		return
	}
	roles, err := discovery.ListRoles(r.Context(), *target)
	if err != nil {
		writeError(w, http.StatusBadGateway, "list roles failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, roles)
}

func (s *Server) resolveDiscoveryTarget(ctx context.Context, inst *model.Instance) (*discovery.Target, error) {
	creds, err := s.d.Store.Credentials(ctx, inst.ID)
	if err != nil {
		return nil, err
	}
	t := &discovery.Target{
		Host: inst.Host, Port: inst.Port, SSLMode: inst.SSLMode,
		RootCertPEM: inst.SSLRootCert, AdminDB: inst.DiscoveryDB,
	}
	switch creds.AuthType {
	case model.AuthPlain:
		t.User, t.Password = creds.Username, creds.Password
	case model.AuthVault:
		if s.d.Vault == nil {
			return nil, errVaultDisabled
		}
		ds, err := s.d.Vault.ReadDBSecret(ctx, creds.VaultPath, creds.VaultUsernameKey, creds.VaultPasswordKey)
		if err != nil {
			return nil, err
		}
		t.User, t.Password = ds.Username, ds.Password
	}
	return t, nil
}

type vaultSecretKeysRequest struct {
	Path string `json:"path"`
}

// vaultSecretKeys (admin) отдаёт ИМЕНА полей секрета по пути — чтобы в UI
// сопоставить их с логином и паролем: называются они у всех по-разному.
//
// Значения полей не возвращаются никогда: наружу уходит только список имён и
// подсказка-догадка. Путь приходит телом, а не в URL, чтобы не оседать в логах
// ingress и access-логах.
func (s *Server) vaultSecretKeys(w http.ResponseWriter, r *http.Request) {
	var req vaultSecretKeysRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if s.d.Vault == nil {
		writeError(w, http.StatusPreconditionFailed, errVaultDisabled.Error())
		return
	}
	keys, err := s.d.Vault.SecretKeys(r.Context(), path)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.audit(r, "vault.inspect", path, nil)
	userKey, passKey := vault.GuessKeys(keys)
	writeJSON(w, http.StatusOK, map[string]any{
		"keys":               keys,
		"guess_username_key": userKey,
		"guess_password_key": passKey,
	})
}

func (s *Server) checkVault(ctx context.Context, in store.InstanceInput) error {
	if s.d.Vault == nil {
		return errVaultDisabled
	}
	return s.d.Vault.Check(ctx, in.VaultPath, in.VaultUsernameKey, in.VaultPasswordKey)
}

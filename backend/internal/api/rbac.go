package api

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gorilla/mux"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/auth"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
)

// identity достаёт Identity запроса. Middleware гарантирует её наличие на /api/*.
func identity(r *http.Request) auth.Identity {
	id, _ := auth.FromContext(r.Context())
	return id
}

func writeForbidden(w http.ResponseWriter, project string, min auth.Role) {
	if project == "" {
		writeError(w, http.StatusForbidden, "instance is not assigned to a project: superuser only")
		return
	}
	writeError(w, http.StatusForbidden, "role "+string(min)+" required in project "+project)
}

// authorizeInstance загружает инстанс и проверяет, что у пользователя роль не
// ниже min в его проекте. При отказе сам пишет ответ и возвращает ok=false.
func (s *Server) authorizeInstance(w http.ResponseWriter, r *http.Request, instanceID string, min auth.Role) (*model.Instance, bool) {
	inst, err := s.d.Store.GetInstance(r.Context(), instanceID)
	if err != nil {
		writeStoreError(w, err)
		return nil, false
	}
	if !identity(r).Can(inst.Project, min) {
		writeForbidden(w, inst.Project, min)
		return nil, false
	}
	return inst, true
}

// authorizeDatabase — то же для базы: права определяются проектом её инстанса.
func (s *Server) authorizeDatabase(w http.ResponseWriter, r *http.Request, databaseID string, min auth.Role) (*model.Database, *model.Instance, bool) {
	db, err := s.d.Store.GetDatabase(r.Context(), databaseID)
	if err != nil {
		writeStoreError(w, err)
		return nil, nil, false
	}
	inst, ok := s.authorizeInstance(w, r, db.InstanceID, min)
	if !ok {
		return nil, nil, false
	}
	return db, inst, true
}

// managedByHeader — им terraform-provider-gemini помечает свои запросы.
const managedByHeader = "X-Gemini-Managed-By"

// managedBy — метка управления для записи в БД. Принимается только известное
// значение: произвольная строка из заголовка нарушила бы CHECK колонки.
func managedBy(r *http.Request) string {
	if r.Header.Get(managedByHeader) == "terraform" {
		return "terraform"
	}
	return ""
}

// projectNameRe — имя проекта = имя группы Keycloak верхнего уровня: без "/"
// (иначе не совпадёт с сегментом пути), без пробелов по краям и управляющих символов.
var projectNameRe = regexp.MustCompile(`^[^/\s][^/\x00-\x1f]{0,62}$`)

func validProjectName(name string) bool {
	return projectNameRe.MatchString(name) && name == strings.TrimSpace(name)
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	items, err := s.d.Store.ListProjects(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

type projectRequest struct {
	Name string `json:"name"`
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var req projectRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if !validProjectName(name) {
		writeError(w, http.StatusBadRequest,
			"name must be 1-63 chars without '/' — it must equal the top-level Keycloak group name")
		return
	}
	by, _ := ctxIdentity(r)
	p, err := s.d.Store.CreateProject(r.Context(), name, by, managedBy(r))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "project.create", name, nil)
	writeJSON(w, http.StatusCreated, p)
}

// clearProjectManagedBy (superuser) снимает метку Terraform с проекта.
func (s *Server) clearProjectManagedBy(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if err := s.d.Store.ClearProjectManagedBy(r.Context(), name); err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "project.unmanage", name, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if err := s.d.Store.DeleteProject(r.Context(), name); err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "project.delete", name, nil)
	w.WriteHeader(http.StatusNoContent)
}

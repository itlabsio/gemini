package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"gl.sdvor.com/devops/docker/gemini/backend/internal/model"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/storage"
	"gl.sdvor.com/devops/docker/gemini/backend/internal/store"
)

// listS3Buckets — встроенный бакет из env (если задан) + бакеты из БД, без секретов.
func (s *Server) listS3Buckets(w http.ResponseWriter, r *http.Request) {
	items, err := s.d.Store.ListS3Buckets(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if b := s.d.Storage.BuiltIn(); b != nil {
		items = append([]model.S3Bucket{*b}, items...)
	}
	writeJSON(w, http.StatusOK, items)
}

type s3BucketRequest struct {
	Name            string `json:"name"`
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	PathStyle       bool   `json:"path_style"`
	UseSSL          bool   `json:"use_ssl"`
	Enabled         bool   `json:"enabled"`
}

// input валидирует запрос. secretRequired — при создании ключ обязателен,
// при обновлении пустой означает «не менять».
func (req s3BucketRequest) input(secretRequired bool) (store.S3BucketInput, string) {
	in := store.S3BucketInput{
		Name:            strings.TrimSpace(req.Name),
		Endpoint:        strings.TrimSuffix(strings.TrimSpace(req.Endpoint), "/"),
		Region:          strings.TrimSpace(req.Region),
		Bucket:          strings.TrimSpace(req.Bucket),
		AccessKeyID:     strings.TrimSpace(req.AccessKeyID),
		SecretAccessKey: strings.TrimSpace(req.SecretAccessKey),
		PathStyle:       req.PathStyle,
		UseSSL:          req.UseSSL,
		Enabled:         req.Enabled,
	}
	switch {
	case in.Name == "" || in.Endpoint == "" || in.Bucket == "" || in.AccessKeyID == "":
		return in, "name, endpoint, bucket and access_key_id are required"
	case strings.Contains(in.Endpoint, "://") || strings.Contains(in.Endpoint, "/"):
		return in, "endpoint must be host[:port] without scheme or path (TLS is set by use_ssl)"
	case secretRequired && in.SecretAccessKey == "":
		return in, "secret_access_key is required"
	}
	return in, ""
}

func (s *Server) createS3Bucket(w http.ResponseWriter, r *http.Request) {
	var req s3BucketRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in, msg := req.input(true)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	id, _ := ctxIdentity(r)
	b, err := s.d.Store.CreateS3Bucket(r.Context(), in, id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "s3_bucket.create", b.ID, map[string]any{"name": b.Name, "bucket": b.Bucket})
	writeJSON(w, http.StatusCreated, b)
}

func (s *Server) updateS3Bucket(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == storage.BuiltInID {
		writeError(w, http.StatusBadRequest, "built-in bucket is managed by Helm values (s3.*)")
		return
	}
	var req s3BucketRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in, msg := req.input(false)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	b, err := s.d.Store.UpdateS3Bucket(r.Context(), id, in)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "s3_bucket.update", id, map[string]any{"name": b.Name, "enabled": b.Enabled})
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) deleteS3Bucket(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == storage.BuiltInID {
		writeError(w, http.StatusBadRequest, "built-in bucket is managed by Helm values (s3.*)")
		return
	}
	if err := s.d.Store.DeleteS3Bucket(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	s.audit(r, "s3_bucket.delete", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

// testS3Bucket проверяет доступ к включённому бакету (BucketExists).
func (s *Server) testS3Bucket(w http.ResponseWriter, r *http.Request) {
	c, err := s.d.Storage.Client(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := c.Ping(ctx); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

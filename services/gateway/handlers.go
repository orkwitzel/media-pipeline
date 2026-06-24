package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
)

type App struct {
	Obj    ObjStore
	Store  Store
	Broker Broker
	Cache  Cache
	Cfg    Config
}

func (a *App) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /upload", a.handleUpload)
	mux.HandleFunc("GET /jobs", a.handleList)
	mux.HandleFunc("GET /jobs/{id}", a.handleGet)
	mux.HandleFunc("GET /jobs/{id}/result", a.handleResult)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /readyz", a.handleReady)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (a *App) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, a.Cfg.MaxUploadBytes)
	file, hdr, err := r.FormFile("file")
	if err != nil { http.Error(w, "missing file field", http.StatusBadRequest); return }
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil { http.Error(w, "read failed", http.StatusBadRequest); return }

	id := uuid.NewString()
	ext := strings.ToLower(path.Ext(hdr.Filename)); if ext == "" { ext = ".bin" }
	key := "originals/" + id + ext
	ct := hdr.Header.Get("Content-Type"); if ct == "" { ct = "application/octet-stream" }

	if err := a.Obj.Put(r.Context(), a.Cfg.BucketOriginals, key, ct, data); err != nil {
		http.Error(w, "store failed", http.StatusBadGateway); return
	}
	now := nowUTC()
	job := JobSnapshot{ID: id, Status: "pending", OriginalKey: key, CreatedAt: now, UpdatedAt: now}
	if err := a.Store.Insert(r.Context(), job); err != nil {
		http.Error(w, "db failed", http.StatusBadGateway); return
	}
	msg, _ := json.Marshal(map[string]string{"jobId": id, "originalKey": key, "createdAt": now.Format(time.RFC3339)})
	if err := a.Broker.PublishJob(r.Context(), msg); err != nil {
		http.Error(w, "enqueue failed", http.StatusBadGateway); return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": id})
}

func (a *App) handleList(w http.ResponseWriter, r *http.Request) {
	jobs, err := a.Store.List(r.Context())
	if err != nil { http.Error(w, "db failed", http.StatusBadGateway); return }
	writeJSON(w, http.StatusOK, jobs)
}

func (a *App) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if c, _ := a.Cache.GetJob(r.Context(), id); c != nil { writeJSON(w, http.StatusOK, c); return }
	job, err := a.Store.Get(r.Context(), id)
	if err != nil { http.Error(w, "db failed", http.StatusBadGateway); return }
	if job == nil { http.Error(w, "not found", http.StatusNotFound); return }
	writeJSON(w, http.StatusOK, job)
}

func (a *App) handleResult(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	variant := r.URL.Query().Get("variant"); if variant == "" { variant = "processed" }
	job, err := a.Store.Get(r.Context(), id)
	if err != nil || job == nil { http.Error(w, "not found", http.StatusNotFound); return }
	var key *string
	if variant == "thumbnail" { key = job.ThumbnailKey } else { key = job.ProcessedKey }
	if key == nil { http.Error(w, "not ready", http.StatusNotFound); return }
	b, ct, err := a.Obj.Get(r.Context(), a.Cfg.BucketProcessed, *key)
	if err != nil { http.Error(w, "fetch failed", http.StatusBadGateway); return }
	w.Header().Set("Content-Type", ct)
	w.Write(b)
}

func (a *App) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second); defer cancel()
	if err := a.Store.Ping(ctx); err != nil { http.Error(w, "db", 503); return }
	if err := a.Cache.Ping(ctx); err != nil { http.Error(w, "redis", 503); return }
	if err := a.Broker.Ping(); err != nil { http.Error(w, "rabbit", 503); return }
	w.Write([]byte("ready"))
}

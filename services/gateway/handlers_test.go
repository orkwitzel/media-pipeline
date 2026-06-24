package main

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeObj struct{ put map[string][]byte }
func (f *fakeObj) Put(_ context.Context, bucket, key, ct string, b []byte) error { f.put[key] = b; return nil }
func (f *fakeObj) Get(_ context.Context, bucket, key string) ([]byte, string, error) { return f.put[key], "image/png", nil }
func (f *fakeObj) EnsureBuckets(context.Context) error { return nil }

type fakeStore struct{ inserted *JobSnapshot }
func (f *fakeStore) Insert(_ context.Context, j JobSnapshot) error { f.inserted = &j; return nil }
func (f *fakeStore) Get(context.Context, string) (*JobSnapshot, error) { return f.inserted, nil }
func (f *fakeStore) List(context.Context) ([]JobSnapshot, error) { return []JobSnapshot{*f.inserted}, nil }
func (f *fakeStore) Ping(context.Context) error { return nil }

type fakeBroker struct{ published [][]byte }
func (f *fakeBroker) PublishJob(_ context.Context, b []byte) error { f.published = append(f.published, b); return nil }
func (f *fakeBroker) Ping() error { return nil }

type fakeCache struct{}
func (fakeCache) GetJob(context.Context, string) (*JobSnapshot, error) { return nil, nil }
func (fakeCache) Ping(context.Context) error { return nil }

func TestUploadStoresEnqueuesAndReturns202(t *testing.T) {
	obj := &fakeObj{put: map[string][]byte{}}
	st := &fakeStore{}
	br := &fakeBroker{}
	app := &App{Obj: obj, Store: st, Broker: br, Cache: fakeCache{}, Cfg: Config{BucketOriginals: "originals", MaxUploadBytes: 1 << 20}}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, _ := w.CreateFormFile("file", "pic.png")
	fw.Write([]byte("\x89PNGfakebytes"))
	w.Close()
	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()

	app.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted { t.Fatalf("want 202 got %d: %s", rec.Code, rec.Body) }
	var resp map[string]string
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["jobId"] == "" { t.Fatal("no jobId returned") }
	if st.inserted == nil || st.inserted.Status != "pending" { t.Fatal("job not inserted as pending") }
	if len(obj.put) != 1 { t.Fatal("original not stored") }
	if len(br.published) != 1 { t.Fatal("job not published") }
}

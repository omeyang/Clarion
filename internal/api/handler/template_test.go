package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/api/schema"
	"github.com/omeyang/clarion/internal/service"
)

func TestTemplateHandler_Create(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "valid template",
			body:       `{"name":"Sales Script","domain":"sales","opening_script":"Hello"}`,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "missing name",
			body:       `{"domain":"sales"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid json",
			body:       `{bad`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newTemplateSvc()
			h := NewTemplateHandler(svc)
			mux := http.NewServeMux()
			h.Register(mux)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/templates", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)

			if tt.wantStatus == http.StatusCreated {
				var resp schema.TemplateResponse
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
				assert.Equal(t, int64(1), resp.ID)
				assert.Equal(t, "draft", resp.Status)
				assert.Equal(t, 1, resp.Version)
			}
		})
	}
}

func TestTemplateHandler_GetByID(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		seed       bool
		wantStatus int
	}{
		{
			name:       "existing template",
			path:       "/api/v1/templates/1",
			seed:       true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "not found",
			path:       "/api/v1/templates/999",
			seed:       false,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "invalid id",
			path:       "/api/v1/templates/abc",
			seed:       false,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newTemplateSvc()
			if tt.seed {
				seedTemplate(repo)
			}
			h := NewTemplateHandler(svc)
			mux := http.NewServeMux()
			h.Register(mux)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func TestTemplateHandler_List(t *testing.T) {
	svc, repo := newTemplateSvc()
	seedTemplate(repo)

	h := NewTemplateHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp schema.ListResponse[schema.TemplateResponse]
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, 1, resp.Total)
}

func TestTemplateHandler_Update(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		body       string
		seed       bool
		wantStatus int
	}{
		{
			name:       "valid update",
			path:       "/api/v1/templates/1",
			body:       `{"name":"Updated","domain":"support"}`,
			seed:       true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "not found",
			path:       "/api/v1/templates/999",
			body:       `{"name":"Updated"}`,
			seed:       false,
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "invalid json",
			path:       "/api/v1/templates/1",
			body:       `{bad`,
			seed:       true,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newTemplateSvc()
			if tt.seed {
				seedTemplate(repo)
			}
			h := NewTemplateHandler(svc)
			mux := http.NewServeMux()
			h.Register(mux)

			req := httptest.NewRequest(http.MethodPut, tt.path, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func TestTemplateHandler_UpdateStatus(t *testing.T) {
	svc, repo := newTemplateSvc()
	seedTemplate(repo)

	h := NewTemplateHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/templates/1/status",
		strings.NewReader(`{"status":"active"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTemplateHandler_UpdateStatus_EmptyStatus(t *testing.T) {
	svc, repo := newTemplateSvc()
	seedTemplate(repo)

	h := NewTemplateHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/templates/1/status",
		strings.NewReader(`{"status":""}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTemplateHandler_Publish(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		seed       bool
		wantStatus int
	}{
		{
			name:       "publish existing",
			path:       "/api/v1/templates/1/publish",
			seed:       true,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "publish not found",
			path:       "/api/v1/templates/999/publish",
			seed:       false,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newTemplateSvc()
			if tt.seed {
				seedTemplate(repo)
			}
			h := NewTemplateHandler(svc)
			mux := http.NewServeMux()
			h.Register(mux)

			req := httptest.NewRequest(http.MethodPost, tt.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)

			if tt.wantStatus == http.StatusCreated {
				var resp schema.SnapshotResponse
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
				assert.Equal(t, int64(1), resp.ID)
				assert.Equal(t, int64(1), resp.TemplateID)
			}
		})
	}
}

func TestTemplateHandler_GetSnapshot(t *testing.T) {
	svc, repo := newTemplateSvc()
	seedTemplate(repo)

	// Publish to create a snapshot.
	h := NewTemplateHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	pubReq := httptest.NewRequest(http.MethodPost, "/api/v1/templates/1/publish", nil)
	pubRec := httptest.NewRecorder()
	mux.ServeHTTP(pubRec, pubReq)
	require.Equal(t, http.StatusCreated, pubRec.Code)

	// Now get the snapshot.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates/snapshots/1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp schema.SnapshotResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, int64(1), resp.ID)
}

func TestTemplateHandler_GetSnapshot_NotFound(t *testing.T) {
	svc, _ := newTemplateSvc()
	h := NewTemplateHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates/snapshots/999", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestTemplateHandler_List_ServiceError(t *testing.T) {
	svc := service.NewTemplateSvc(failTemplateRepo{})
	h := NewTemplateHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestTemplateHandler_GetByID_ServiceError(t *testing.T) {
	svc := service.NewTemplateSvc(failTemplateRepo{})
	h := NewTemplateHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates/1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestTemplateHandler_Create_ServiceError(t *testing.T) {
	svc := service.NewTemplateSvc(failTemplateRepo{})
	h := NewTemplateHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	body := `{"name":"Template"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/templates", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestTemplateHandler_UpdateStatus_InvalidID(t *testing.T) {
	svc, _ := newTemplateSvc()
	h := NewTemplateHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/templates/abc/status",
		strings.NewReader(`{"status":"active"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTemplateHandler_UpdateStatus_InvalidJSON(t *testing.T) {
	svc, repo := newTemplateSvc()
	seedTemplate(repo)
	h := NewTemplateHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/templates/1/status",
		strings.NewReader(`{bad`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTemplateHandler_UpdateStatus_ServiceError(t *testing.T) {
	svc, _ := newTemplateSvc()
	h := NewTemplateHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/templates/999/status",
		strings.NewReader(`{"status":"active"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestTemplateHandler_Update_InvalidID(t *testing.T) {
	svc, _ := newTemplateSvc()
	h := NewTemplateHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/templates/abc",
		strings.NewReader(`{"name":"Updated"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTemplateHandler_Publish_InvalidID(t *testing.T) {
	svc, _ := newTemplateSvc()
	h := NewTemplateHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/templates/abc/publish", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTemplateHandler_Publish_ServiceError(t *testing.T) {
	svc := service.NewTemplateSvc(failTemplateRepo{})
	h := NewTemplateHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/templates/1/publish", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestTemplateHandler_GetSnapshot_InvalidID(t *testing.T) {
	svc, _ := newTemplateSvc()
	h := NewTemplateHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates/snapshots/abc", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTemplateHandler_GetSnapshot_ServiceError(t *testing.T) {
	svc := service.NewTemplateSvc(failTemplateRepo{})
	h := NewTemplateHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates/snapshots/1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

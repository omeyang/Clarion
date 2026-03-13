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

func TestContactHandler_Create(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "valid contact",
			body:       `{"phone_masked":"138****1234","phone_hash":"abc123","source":"import"}`,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "missing phone_masked",
			body:       `{"phone_hash":"abc123"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing phone_hash",
			body:       `{"phone_masked":"138****1234"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid json",
			body:       `{invalid`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newContactSvc()
			h := NewContactHandler(svc)
			mux := http.NewServeMux()
			h.Register(mux)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/contacts", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)

			if tt.wantStatus == http.StatusCreated {
				var resp schema.ContactResponse
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
				assert.Equal(t, int64(1), resp.ID)
				assert.Equal(t, "new", resp.CurrentStatus)
			}
		})
	}
}

func TestContactHandler_GetByID(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		seed       bool
		wantStatus int
	}{
		{
			name:       "existing contact",
			path:       "/api/v1/contacts/1",
			seed:       true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "not found",
			path:       "/api/v1/contacts/999",
			seed:       false,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "invalid id",
			path:       "/api/v1/contacts/abc",
			seed:       false,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newContactSvc()
			if tt.seed {
				seedContact(repo)
			}
			h := NewContactHandler(svc)
			mux := http.NewServeMux()
			h.Register(mux)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func TestContactHandler_List(t *testing.T) {
	svc, repo := newContactSvc()
	seedContact(repo)
	seedContact(repo)

	h := NewContactHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/contacts?offset=0&limit=10", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp schema.ListResponse[schema.ContactResponse]
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, 2, resp.Total)
	assert.Len(t, resp.Items, 2)
}

func TestContactHandler_List_Empty(t *testing.T) {
	svc, _ := newContactSvc()
	h := NewContactHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/contacts", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp schema.ListResponse[schema.ContactResponse]
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, 0, resp.Total)
	assert.NotNil(t, resp.Items)
}

func TestContactHandler_UpdateStatus(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		body       string
		seed       bool
		wantStatus int
	}{
		{
			name:       "valid update",
			path:       "/api/v1/contacts/1/status",
			body:       `{"status":"called"}`,
			seed:       true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "empty status",
			path:       "/api/v1/contacts/1/status",
			body:       `{"status":""}`,
			seed:       true,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid id",
			path:       "/api/v1/contacts/abc/status",
			body:       `{"status":"called"}`,
			seed:       false,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newContactSvc()
			if tt.seed {
				seedContact(repo)
			}
			h := NewContactHandler(svc)
			mux := http.NewServeMux()
			h.Register(mux)

			req := httptest.NewRequest(http.MethodPatch, tt.path, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func TestContactHandler_BulkCreate(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCount  int
	}{
		{
			name:       "valid bulk create",
			body:       `{"contacts":[{"phone_masked":"138****1234","phone_hash":"h1"},{"phone_masked":"139****5678","phone_hash":"h2"}]}`,
			wantStatus: http.StatusCreated,
			wantCount:  2,
		},
		{
			name:       "empty contacts",
			body:       `{"contacts":[]}`,
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
			svc, _ := newContactSvc()
			h := NewContactHandler(svc)
			mux := http.NewServeMux()
			h.Register(mux)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/contacts/bulk", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)

			if tt.wantStatus == http.StatusCreated {
				var resp map[string]int
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
				assert.Equal(t, tt.wantCount, resp["inserted"])
			}
		})
	}
}

func TestContactHandler_List_ServiceError(t *testing.T) {
	svc := service.NewContactSvc(failContactRepo{})
	h := NewContactHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/contacts", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestContactHandler_GetByID_ServiceError(t *testing.T) {
	svc := service.NewContactSvc(failContactRepo{})
	h := NewContactHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/contacts/1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestContactHandler_Create_ServiceError(t *testing.T) {
	svc := service.NewContactSvc(failContactRepo{})
	h := NewContactHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	body := `{"phone_masked":"138****1234","phone_hash":"abc123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/contacts", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestContactHandler_Create_WithProfileJSON(t *testing.T) {
	svc, _ := newContactSvc()
	h := NewContactHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	body := `{"phone_masked":"138****1234","phone_hash":"abc123","profile_json":{"name":"test"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/contacts", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestContactHandler_UpdateStatus_InvalidJSON(t *testing.T) {
	svc, repo := newContactSvc()
	seedContact(repo)
	h := NewContactHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/contacts/1/status", strings.NewReader(`{bad`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestContactHandler_UpdateStatus_ServiceError(t *testing.T) {
	svc := service.NewContactSvc(failContactRepo{})
	h := NewContactHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/contacts/1/status",
		strings.NewReader(`{"status":"called"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestContactHandler_BulkCreate_ServiceError(t *testing.T) {
	svc := service.NewContactSvc(failContactRepo{})
	h := NewContactHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	body := `{"contacts":[{"phone_masked":"138****1234","phone_hash":"h1"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/contacts/bulk", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

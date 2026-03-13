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

func TestTaskHandler_Create(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "valid task",
			body:       `{"name":"Campaign 1","scenario_template_id":1,"daily_limit":100,"max_concurrent":5}`,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "missing name",
			body:       `{"scenario_template_id":1}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing template id",
			body:       `{"name":"Campaign 1"}`,
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
			svc, _ := newTaskSvc()
			h := NewTaskHandler(svc)
			mux := http.NewServeMux()
			h.Register(mux)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)

			if tt.wantStatus == http.StatusCreated {
				var resp schema.TaskResponse
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
				assert.Equal(t, int64(1), resp.ID)
				assert.Equal(t, "draft", resp.Status)
			}
		})
	}
}

func TestTaskHandler_GetByID(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		seed       bool
		wantStatus int
	}{
		{
			name:       "existing task",
			path:       "/api/v1/tasks/1",
			seed:       true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "not found",
			path:       "/api/v1/tasks/999",
			seed:       false,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "invalid id",
			path:       "/api/v1/tasks/abc",
			seed:       false,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newTaskSvc()
			if tt.seed {
				seedTask(repo)
			}
			h := NewTaskHandler(svc)
			mux := http.NewServeMux()
			h.Register(mux)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func TestTaskHandler_List(t *testing.T) {
	svc, repo := newTaskSvc()
	seedTask(repo)
	seedTask(repo)

	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp schema.ListResponse[schema.TaskResponse]
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, 2, resp.Total)
}

func TestTaskHandler_Update(t *testing.T) {
	svc, repo := newTaskSvc()
	seedTask(repo)

	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/tasks/1",
		strings.NewReader(`{"name":"Updated Task","daily_limit":200}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTaskHandler_UpdateStatus(t *testing.T) {
	svc, repo := newTaskSvc()
	seedTask(repo)

	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/1/status",
		strings.NewReader(`{"status":"running"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTaskHandler_Start(t *testing.T) {
	svc, repo := newTaskSvc()
	seedTask(repo)

	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/1/start", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "running", resp["status"])
}

func TestTaskHandler_Pause(t *testing.T) {
	svc, repo := newTaskSvc()
	seedTask(repo)

	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/1/pause", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "paused", resp["status"])
}

func TestTaskHandler_Cancel(t *testing.T) {
	svc, repo := newTaskSvc()
	seedTask(repo)

	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/1/cancel", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "cancelled", resp["status"])
}

func TestTaskHandler_Start_NotFound(t *testing.T) {
	svc, _ := newTaskSvc()
	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/999/start", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestTaskHandler_List_ServiceError(t *testing.T) {
	svc := service.NewTaskSvc(failTaskRepo{})
	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestTaskHandler_GetByID_ServiceError(t *testing.T) {
	svc := service.NewTaskSvc(failTaskRepo{})
	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestTaskHandler_Create_ServiceError(t *testing.T) {
	svc := service.NewTaskSvc(failTaskRepo{})
	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	body := `{"name":"Task","scenario_template_id":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestTaskHandler_Update_InvalidID(t *testing.T) {
	svc, _ := newTaskSvc()
	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/tasks/abc",
		strings.NewReader(`{"name":"Updated"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTaskHandler_Update_InvalidJSON(t *testing.T) {
	svc, repo := newTaskSvc()
	seedTask(repo)
	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/tasks/1", strings.NewReader(`{bad`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTaskHandler_Update_NotFound(t *testing.T) {
	svc, _ := newTaskSvc()
	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/tasks/999",
		strings.NewReader(`{"name":"Updated"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestTaskHandler_UpdateStatus_InvalidID(t *testing.T) {
	svc, _ := newTaskSvc()
	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/abc/status",
		strings.NewReader(`{"status":"running"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTaskHandler_UpdateStatus_InvalidJSON(t *testing.T) {
	svc, repo := newTaskSvc()
	seedTask(repo)
	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/1/status",
		strings.NewReader(`{bad`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTaskHandler_UpdateStatus_EmptyStatus(t *testing.T) {
	svc, repo := newTaskSvc()
	seedTask(repo)
	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/1/status",
		strings.NewReader(`{"status":""}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTaskHandler_UpdateStatus_ServiceError(t *testing.T) {
	svc, _ := newTaskSvc()
	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/tasks/999/status",
		strings.NewReader(`{"status":"running"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestTaskHandler_Pause_NotFound(t *testing.T) {
	svc, _ := newTaskSvc()
	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/999/pause", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestTaskHandler_Cancel_NotFound(t *testing.T) {
	svc, _ := newTaskSvc()
	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/999/cancel", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestTaskHandler_Start_InvalidID(t *testing.T) {
	svc, _ := newTaskSvc()
	h := NewTaskHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/abc/start", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

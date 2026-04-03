package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omeyang/clarion/internal/api/schema"
	"github.com/omeyang/clarion/internal/model"
	"github.com/omeyang/clarion/internal/service"
)

func TestCallHandler_ListByTask(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		seedCalls  int
		wantStatus int
		wantTotal  int
	}{
		{
			name:       "list calls for task",
			path:       "/api/v1/tasks/1/calls",
			seedCalls:  2,
			wantStatus: http.StatusOK,
			wantTotal:  2,
		},
		{
			name:       "empty list",
			path:       "/api/v1/tasks/1/calls",
			seedCalls:  0,
			wantStatus: http.StatusOK,
			wantTotal:  0,
		},
		{
			name:       "invalid task id",
			path:       "/api/v1/tasks/abc/calls",
			seedCalls:  0,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newCallSvc()
			for range tt.seedCalls {
				seedCall(repo)
			}
			h := NewCallHandler(svc)
			mux := http.NewServeMux()
			h.Register(mux)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, withTenantCtx(req))

			assert.Equal(t, tt.wantStatus, rec.Code)

			if tt.wantStatus == http.StatusOK {
				var resp schema.ListResponse[schema.CallResponse]
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
				assert.Equal(t, tt.wantTotal, resp.Total)
			}
		})
	}
}

func TestCallHandler_GetByID(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		seed       bool
		wantStatus int
	}{
		{
			name:       "existing call",
			path:       "/api/v1/calls/1",
			seed:       true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "not found",
			path:       "/api/v1/calls/999",
			seed:       false,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "invalid id",
			path:       "/api/v1/calls/abc",
			seed:       false,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newCallSvc()
			if tt.seed {
				seedCall(repo)
			}
			h := NewCallHandler(svc)
			mux := http.NewServeMux()
			h.Register(mux)

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, withTenantCtx(req))

			assert.Equal(t, tt.wantStatus, rec.Code)

			if tt.wantStatus == http.StatusOK {
				var resp schema.CallResponse
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
				assert.Equal(t, int64(1), resp.ID)
			}
		})
	}
}

func TestCallHandler_GetDetail(t *testing.T) {
	svc, repo := newCallSvc()
	seedCall(repo)

	// 添加对话轮次。
	repo.createTurn(&model.DialogueTurn{
		CallID:     1,
		TurnNumber: 1,
		Speaker:    "bot",
		Content:    "Hello, this is a test call.",
	})
	repo.createTurn(&model.DialogueTurn{
		CallID:     1,
		TurnNumber: 2,
		Speaker:    "user",
		Content:    "Yes, I am interested.",
	})

	h := NewCallHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/calls/1/detail", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, withTenantCtx(req))

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp schema.CallDetailResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, int64(1), resp.Call.ID)
	assert.Len(t, resp.Turns, 2)
	assert.Equal(t, "bot", resp.Turns[0].Speaker)
	assert.Equal(t, "user", resp.Turns[1].Speaker)
}

func TestCallHandler_GetDetail_NotFound(t *testing.T) {
	svc, _ := newCallSvc()
	h := NewCallHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/calls/999/detail", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, withTenantCtx(req))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCallHandler_GetDetail_NoTurns(t *testing.T) {
	svc, repo := newCallSvc()
	seedCall(repo)

	h := NewCallHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/calls/1/detail", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, withTenantCtx(req))

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp schema.CallDetailResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.NotNil(t, resp.Turns)
	assert.Len(t, resp.Turns, 0)
}

func TestCallHandler_ListByTask_ServiceError(t *testing.T) {
	svc := service.NewCallSvc(failCallRepo{})
	h := NewCallHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/1/calls", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, withTenantCtx(req))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestCallHandler_GetByID_ServiceError(t *testing.T) {
	svc := service.NewCallSvc(failCallRepo{})
	h := NewCallHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/calls/1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, withTenantCtx(req))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestCallHandler_GetDetail_ServiceError(t *testing.T) {
	svc := service.NewCallSvc(failCallRepo{})
	h := NewCallHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/calls/1/detail", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, withTenantCtx(req))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestCallHandler_GetDetail_InvalidID(t *testing.T) {
	svc, _ := newCallSvc()
	h := NewCallHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/calls/abc/detail", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, withTenantCtx(req))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCallHandler_GetDetail_ListTurnsError(t *testing.T) {
	mem := newMemCallRepo()
	seedCall(mem)
	repo := &failTurnsCallRepo{memCallRepo: mem}

	svc := service.NewCallSvc(repo)
	h := NewCallHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/calls/1/detail", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, withTenantCtx(req))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

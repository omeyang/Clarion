package handler

import (
	"net/http"

	"github.com/omeyang/clarion/internal/api/schema"
	"github.com/omeyang/clarion/internal/service"
)

// CallHandler 处理通话相关的 HTTP 请求。
type CallHandler struct {
	svc *service.CallSvc
}

// NewCallHandler 创建新的 CallHandler。
func NewCallHandler(svc *service.CallSvc) *CallHandler {
	return &CallHandler{svc: svc}
}

// Register 在给定的 mux 上注册通话路由。
func (h *CallHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/tasks/{taskId}/calls", h.ListByTask)
	mux.HandleFunc("GET /api/v1/calls/{id}", h.GetByID)
	mux.HandleFunc("GET /api/v1/calls/{id}/detail", h.GetDetail)
}

// ListByTask 返回某个任务下的分页通话列表。
func (h *CallHandler) ListByTask(w http.ResponseWriter, r *http.Request) {
	taskID, ok := pathID(w, r, "taskId")
	if !ok {
		return
	}

	offset, limit := schema.Pagination(r)

	calls, total, err := h.svc.ListByTask(r.Context(), taskID, offset, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list calls", err.Error())
		return
	}

	items := schema.CallsFromModels(calls)
	if items == nil {
		items = []schema.CallResponse{}
	}

	writeJSON(w, http.StatusOK, schema.ListResponse[schema.CallResponse]{
		Items:  items,
		Total:  total,
		Offset: offset,
		Limit:  limit,
	})
}

// GetByID 根据 ID 返回单条通话记录。
func (h *CallHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	c, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get call", err.Error())
		return
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "call not found", "")
		return
	}

	writeJSON(w, http.StatusOK, schema.CallFromModel(c))
}

// GetDetail 返回通话详情及其对话轮次。
func (h *CallHandler) GetDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	c, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get call", err.Error())
		return
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "call not found", "")
		return
	}

	turns, err := h.svc.ListTurns(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list turns", err.Error())
		return
	}

	turnResponses := schema.TurnsFromModels(turns)
	if turnResponses == nil {
		turnResponses = []schema.TurnResponse{}
	}

	writeJSON(w, http.StatusOK, schema.CallDetailResponse{
		Call:  schema.CallFromModel(c),
		Turns: turnResponses,
	})
}

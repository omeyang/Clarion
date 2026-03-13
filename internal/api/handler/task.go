package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/omeyang/clarion/internal/api/schema"
	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/model"
	"github.com/omeyang/clarion/internal/service"
)

// TaskHandler 处理任务相关的 HTTP 请求。
type TaskHandler struct {
	svc *service.TaskSvc
}

// NewTaskHandler 创建新的 TaskHandler。
func NewTaskHandler(svc *service.TaskSvc) *TaskHandler {
	return &TaskHandler{svc: svc}
}

// Register 在给定的 mux 上注册任务路由。
func (h *TaskHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/tasks", h.List)
	mux.HandleFunc("POST /api/v1/tasks", h.Create)
	mux.HandleFunc("GET /api/v1/tasks/{id}", h.GetByID)
	mux.HandleFunc("PUT /api/v1/tasks/{id}", h.Update)
	mux.HandleFunc("PATCH /api/v1/tasks/{id}/status", h.UpdateStatus)
	mux.HandleFunc("POST /api/v1/tasks/{id}/start", h.Start)
	mux.HandleFunc("POST /api/v1/tasks/{id}/pause", h.Pause)
	mux.HandleFunc("POST /api/v1/tasks/{id}/cancel", h.Cancel)
}

// List 返回分页的任务列表。
func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	offset, limit := schema.Pagination(r)

	tasks, total, err := h.svc.List(r.Context(), offset, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tasks", err.Error())
		return
	}

	items := schema.TasksFromModels(tasks)
	if items == nil {
		items = []schema.TaskResponse{}
	}

	writeJSON(w, http.StatusOK, schema.ListResponse[schema.TaskResponse]{
		Items:  items,
		Total:  total,
		Offset: offset,
		Limit:  limit,
	})
}

// Create 创建新的外呼任务。
func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req schema.CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required", "")
		return
	}
	if req.ScenarioTemplateID == 0 {
		writeError(w, http.StatusBadRequest, "scenario_template_id is required", "")
		return
	}

	t := &model.CallTask{
		Name:               req.Name,
		ScenarioTemplateID: req.ScenarioTemplateID,
		ContactFilter:      defaultJSON(req.ContactFilter),
		ScheduleConfig:     defaultJSON(req.ScheduleConfig),
		DailyLimit:         req.DailyLimit,
		MaxConcurrent:      req.MaxConcurrent,
	}

	id, err := h.svc.Create(r.Context(), t)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create task", err.Error())
		return
	}

	t.ID = id
	writeJSON(w, http.StatusCreated, schema.TaskFromModel(t))
}

// GetByID 根据 ID 返回单个任务。
func (h *TaskHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	t, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get task", err.Error())
		return
	}
	if t == nil {
		writeError(w, http.StatusNotFound, "task not found", "")
		return
	}

	writeJSON(w, http.StatusOK, schema.TaskFromModel(t))
}

// Update 更新任务。
func (h *TaskHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	var req schema.UpdateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	t := &model.CallTask{
		ID:             id,
		Name:           req.Name,
		ContactFilter:  defaultJSON(req.ContactFilter),
		ScheduleConfig: defaultJSON(req.ScheduleConfig),
		DailyLimit:     req.DailyLimit,
		MaxConcurrent:  req.MaxConcurrent,
		TotalContacts:  req.TotalContacts,
	}

	if err := h.svc.Update(r.Context(), t); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update task", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// UpdateStatus 变更任务状态。
func (h *TaskHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	var req schema.UpdateTaskStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if req.Status == "" {
		writeError(w, http.StatusBadRequest, "status is required", "")
		return
	}

	if err := h.svc.UpdateStatus(r.Context(), id, engine.TaskStatus(req.Status)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update status", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// Start 启动任务。
func (h *TaskHandler) Start(w http.ResponseWriter, r *http.Request) {
	h.actionByID(w, r, h.svc.Start, "running")
}

// Pause 暂停任务。
func (h *TaskHandler) Pause(w http.ResponseWriter, r *http.Request) {
	h.actionByID(w, r, h.svc.Pause, "paused")
}

// Cancel 取消任务。
func (h *TaskHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	h.actionByID(w, r, h.svc.Cancel, "cancelled")
}

// actionByID 提取 ID 并执行带 context 的操作。
func (h *TaskHandler) actionByID(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, id int64) error, status string) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	if err := fn(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update status", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

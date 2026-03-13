package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/omeyang/clarion/internal/api/schema"
	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/model"
	"github.com/omeyang/clarion/internal/service"
)

// TemplateHandler 处理模板相关的 HTTP 请求。
type TemplateHandler struct {
	svc *service.TemplateSvc
}

// NewTemplateHandler 创建新的 TemplateHandler。
func NewTemplateHandler(svc *service.TemplateSvc) *TemplateHandler {
	return &TemplateHandler{svc: svc}
}

// Register 在给定的 mux 上注册模板路由。
func (h *TemplateHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/templates", h.List)
	mux.HandleFunc("POST /api/v1/templates", h.Create)
	mux.HandleFunc("GET /api/v1/templates/{id}", h.GetByID)
	mux.HandleFunc("PUT /api/v1/templates/{id}", h.Update)
	mux.HandleFunc("PATCH /api/v1/templates/{id}/status", h.UpdateStatus)
	mux.HandleFunc("POST /api/v1/templates/{id}/publish", h.Publish)
	mux.HandleFunc("GET /api/v1/templates/snapshots/{id}", h.GetSnapshot)
}

// List 返回分页的模板列表。
func (h *TemplateHandler) List(w http.ResponseWriter, r *http.Request) {
	offset, limit := schema.Pagination(r)

	templates, total, err := h.svc.List(r.Context(), offset, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list templates", err.Error())
		return
	}

	items := schema.TemplatesFromModels(templates)
	if items == nil {
		items = []schema.TemplateResponse{}
	}

	writeJSON(w, http.StatusOK, schema.ListResponse[schema.TemplateResponse]{
		Items:  items,
		Total:  total,
		Offset: offset,
		Limit:  limit,
	})
}

// Create 创建新的场景模板。
func (h *TemplateHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req schema.CreateTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required", "")
		return
	}

	t := &model.ScenarioTemplate{
		Name:                 req.Name,
		Domain:               req.Domain,
		OpeningScript:        req.OpeningScript,
		StateMachineConfig:   defaultJSON(req.StateMachineConfig),
		ExtractionSchema:     defaultJSON(req.ExtractionSchema),
		GradingRules:         defaultJSON(req.GradingRules),
		PromptTemplates:      defaultJSON(req.PromptTemplates),
		NotificationConfig:   defaultJSON(req.NotificationConfig),
		CallProtectionConfig: defaultJSON(req.CallProtectionConfig),
		PrecompiledAudios:    defaultJSON(req.PrecompiledAudios),
	}

	id, err := h.svc.Create(r.Context(), t)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create template", err.Error())
		return
	}

	t.ID = id
	writeJSON(w, http.StatusCreated, schema.TemplateFromModel(t))
}

// GetByID 根据 ID 返回单个模板。
func (h *TemplateHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	t, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get template", err.Error())
		return
	}
	if t == nil {
		writeError(w, http.StatusNotFound, "template not found", "")
		return
	}

	writeJSON(w, http.StatusOK, schema.TemplateFromModel(t))
}

// Update 更新模板。
func (h *TemplateHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	var req schema.UpdateTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	t := &model.ScenarioTemplate{
		ID:                   id,
		Name:                 req.Name,
		Domain:               req.Domain,
		OpeningScript:        req.OpeningScript,
		StateMachineConfig:   defaultJSON(req.StateMachineConfig),
		ExtractionSchema:     defaultJSON(req.ExtractionSchema),
		GradingRules:         defaultJSON(req.GradingRules),
		PromptTemplates:      defaultJSON(req.PromptTemplates),
		NotificationConfig:   defaultJSON(req.NotificationConfig),
		CallProtectionConfig: defaultJSON(req.CallProtectionConfig),
		PrecompiledAudios:    defaultJSON(req.PrecompiledAudios),
	}

	if err := h.svc.Update(r.Context(), t); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update template", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// UpdateStatus 变更模板状态。
func (h *TemplateHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	var req schema.UpdateTemplateStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if req.Status == "" {
		writeError(w, http.StatusBadRequest, "status is required", "")
		return
	}

	if err := h.svc.UpdateStatus(r.Context(), id, engine.TemplateStatus(req.Status)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update status", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// Publish 创建模板的不可变快照，并将状态设为 published。
func (h *TemplateHandler) Publish(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	snap, err := h.svc.Publish(r.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			writeError(w, http.StatusNotFound, "template not found", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to publish template", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, schema.SnapshotFromModel(snap))
}

// GetSnapshot 根据 ID 返回模板快照。
func (h *TemplateHandler) GetSnapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	snap, err := h.svc.GetSnapshot(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get snapshot", err.Error())
		return
	}
	if snap == nil {
		writeError(w, http.StatusNotFound, "snapshot not found", "")
		return
	}

	writeJSON(w, http.StatusOK, schema.SnapshotFromModel(snap))
}

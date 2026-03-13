package handler

import (
	"encoding/json"
	"net/http"

	"github.com/omeyang/clarion/internal/api/schema"
	"github.com/omeyang/clarion/internal/model"
	"github.com/omeyang/clarion/internal/service"
)

// ContactHandler 处理联系人相关的 HTTP 请求。
type ContactHandler struct {
	svc *service.ContactSvc
}

// NewContactHandler 创建新的 ContactHandler。
func NewContactHandler(svc *service.ContactSvc) *ContactHandler {
	return &ContactHandler{svc: svc}
}

// Register 在给定的 mux 上注册联系人路由。
func (h *ContactHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/contacts", h.List)
	mux.HandleFunc("POST /api/v1/contacts", h.Create)
	mux.HandleFunc("GET /api/v1/contacts/{id}", h.GetByID)
	mux.HandleFunc("PATCH /api/v1/contacts/{id}/status", h.UpdateStatus)
	mux.HandleFunc("POST /api/v1/contacts/bulk", h.BulkCreate)
}

// List 返回分页的联系人列表。
func (h *ContactHandler) List(w http.ResponseWriter, r *http.Request) {
	offset, limit := schema.Pagination(r)

	contacts, total, err := h.svc.List(r.Context(), offset, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list contacts", err.Error())
		return
	}

	items := schema.ContactsFromModels(contacts)
	if items == nil {
		items = []schema.ContactResponse{}
	}

	writeJSON(w, http.StatusOK, schema.ListResponse[schema.ContactResponse]{
		Items:  items,
		Total:  total,
		Offset: offset,
		Limit:  limit,
	})
}

// Create 创建新联系人。
func (h *ContactHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req schema.CreateContactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if req.PhoneMasked == "" || req.PhoneHash == "" {
		writeError(w, http.StatusBadRequest, "phone_masked and phone_hash are required", "")
		return
	}

	c := &model.Contact{
		PhoneMasked: req.PhoneMasked,
		PhoneHash:   req.PhoneHash,
		Source:      req.Source,
		ProfileJSON: defaultJSON(req.ProfileJSON),
	}

	id, err := h.svc.Create(r.Context(), c)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create contact", err.Error())
		return
	}

	c.ID = id
	writeJSON(w, http.StatusCreated, schema.ContactFromModel(c))
}

// GetByID 根据 ID 返回单个联系人。
func (h *ContactHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	c, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get contact", err.Error())
		return
	}
	if c == nil {
		writeError(w, http.StatusNotFound, "contact not found", "")
		return
	}

	writeJSON(w, http.StatusOK, schema.ContactFromModel(c))
}

// UpdateStatus 更新联系人状态。
func (h *ContactHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	var req schema.UpdateContactStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if req.Status == "" {
		writeError(w, http.StatusBadRequest, "status is required", "")
		return
	}

	if err := h.svc.UpdateStatus(r.Context(), id, req.Status); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update status", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// BulkCreate 批量创建联系人。
func (h *ContactHandler) BulkCreate(w http.ResponseWriter, r *http.Request) {
	var req schema.BulkCreateContactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if len(req.Contacts) == 0 {
		writeError(w, http.StatusBadRequest, "contacts array is required", "")
		return
	}

	contacts := make([]model.Contact, len(req.Contacts))
	for i, cr := range req.Contacts {
		contacts[i] = model.Contact{
			PhoneMasked: cr.PhoneMasked,
			PhoneHash:   cr.PhoneHash,
			Source:      cr.Source,
			ProfileJSON: defaultJSON(cr.ProfileJSON),
		}
	}

	inserted, err := h.svc.BulkCreate(r.Context(), contacts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to bulk create contacts", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]int{"inserted": inserted})
}

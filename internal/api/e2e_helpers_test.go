package api

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/model"
)

// e2eRepos 聚合所有端到端测试使用的内存仓储。
type e2eRepos struct {
	contacts  *e2eContactRepo
	templates *e2eTemplateRepo
	tasks     *e2eTaskRepo
	calls     *e2eCallRepo
}

func newE2ERepos() *e2eRepos {
	return &e2eRepos{
		contacts:  &e2eContactRepo{data: make(map[int64]*model.Contact), nextID: 1},
		templates: &e2eTemplateRepo{templates: make(map[int64]*model.ScenarioTemplate), snapshots: make(map[int64]*model.TemplateSnapshot), nextID: 1, nextSnapID: 1},
		tasks:     &e2eTaskRepo{data: make(map[int64]*model.CallTask), nextID: 1},
		calls:     &e2eCallRepo{calls: make(map[int64]*model.Call), turns: make(map[int64][]model.DialogueTurn), nextID: 1},
	}
}

// seedCallWithTurns 预置一条通话记录和两个对话轮次。
func (r *e2eRepos) seedCallWithTurns() {
	c := &model.Call{
		ContactID:       1,
		TaskID:          1,
		SessionID:       "e2e-session",
		Status:          engine.CallCompleted,
		AnswerType:      engine.AnswerHuman,
		ExtractedFields: json.RawMessage(`{"company":"测试公司"}`),
		RuleTrace:       json.RawMessage(`{}`),
	}
	r.calls.mu.Lock()
	id := r.calls.nextID
	r.calls.nextID++
	c.ID = id
	clone := *c
	r.calls.calls[id] = &clone
	r.calls.turns[id] = []model.DialogueTurn{
		{ID: 1, CallID: id, TurnNumber: 1, Speaker: "bot", Content: "您好，我是小李"},
		{ID: 2, CallID: id, TurnNumber: 2, Speaker: "user", Content: "你好，什么事？"},
	}
	r.calls.mu.Unlock()
}

// --- 联系人仓储 ---

type e2eContactRepo struct {
	mu     sync.RWMutex
	data   map[int64]*model.Contact
	nextID int64
}

func (r *e2eContactRepo) Create(_ context.Context, c *model.Contact) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.nextID
	r.nextID++
	c.ID = id
	clone := *c
	r.data[id] = &clone
	return id, nil
}

func (r *e2eContactRepo) GetByID(_ context.Context, id int64) (*model.Contact, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.data[id]
	if !ok {
		return nil, nil
	}
	clone := *c
	return &clone, nil
}

func (r *e2eContactRepo) List(_ context.Context, offset, limit int) ([]model.Contact, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []model.Contact
	for _, c := range r.data {
		all = append(all, *c)
	}
	total := len(all)
	if offset >= total {
		return nil, total, nil
	}
	end := min(offset+limit, total)
	return all[offset:end], total, nil
}

func (r *e2eContactRepo) UpdateStatus(_ context.Context, id int64, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.data[id]
	if !ok {
		return fmt.Errorf("contact %d not found", id)
	}
	c.CurrentStatus = status
	return nil
}

func (r *e2eContactRepo) BulkCreate(_ context.Context, contacts []model.Contact) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range contacts {
		id := r.nextID
		r.nextID++
		contacts[i].ID = id
		clone := contacts[i]
		r.data[id] = &clone
	}
	return len(contacts), nil
}

// --- 模板仓储 ---

type e2eTemplateRepo struct {
	mu         sync.RWMutex
	templates  map[int64]*model.ScenarioTemplate
	snapshots  map[int64]*model.TemplateSnapshot
	nextID     int64
	nextSnapID int64
}

func (r *e2eTemplateRepo) Create(_ context.Context, t *model.ScenarioTemplate) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.nextID
	r.nextID++
	t.ID = id
	clone := *t
	r.templates[id] = &clone
	return id, nil
}

func (r *e2eTemplateRepo) GetByID(_ context.Context, id int64) (*model.ScenarioTemplate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.templates[id]
	if !ok {
		return nil, nil
	}
	clone := *t
	return &clone, nil
}

func (r *e2eTemplateRepo) List(_ context.Context, offset, limit int) ([]model.ScenarioTemplate, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []model.ScenarioTemplate
	for _, t := range r.templates {
		all = append(all, *t)
	}
	total := len(all)
	if offset >= total {
		return nil, total, nil
	}
	end := min(offset+limit, total)
	return all[offset:end], total, nil
}

func (r *e2eTemplateRepo) Update(_ context.Context, t *model.ScenarioTemplate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.templates[t.ID]
	if !ok {
		return fmt.Errorf("template %d not found", t.ID)
	}
	existing.Name = t.Name
	existing.Domain = t.Domain
	existing.OpeningScript = t.OpeningScript
	existing.Version++
	return nil
}

func (r *e2eTemplateRepo) UpdateStatus(_ context.Context, id int64, status engine.TemplateStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.templates[id]
	if !ok {
		return fmt.Errorf("template %d not found", id)
	}
	t.Status = status
	return nil
}

func (r *e2eTemplateRepo) CreateSnapshot(_ context.Context, snap *model.TemplateSnapshot) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.nextSnapID
	r.nextSnapID++
	snap.ID = id
	clone := *snap
	r.snapshots[id] = &clone
	return id, nil
}

func (r *e2eTemplateRepo) GetSnapshotByID(_ context.Context, id int64) (*model.TemplateSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snap, ok := r.snapshots[id]
	if !ok {
		return nil, nil
	}
	clone := *snap
	return &clone, nil
}

// --- 任务仓储 ---

type e2eTaskRepo struct {
	mu     sync.RWMutex
	data   map[int64]*model.CallTask
	nextID int64
}

func (r *e2eTaskRepo) Create(_ context.Context, t *model.CallTask) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.nextID
	r.nextID++
	t.ID = id
	clone := *t
	r.data[id] = &clone
	return id, nil
}

func (r *e2eTaskRepo) GetByID(_ context.Context, id int64) (*model.CallTask, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.data[id]
	if !ok {
		return nil, nil
	}
	clone := *t
	return &clone, nil
}

func (r *e2eTaskRepo) List(_ context.Context, offset, limit int) ([]model.CallTask, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []model.CallTask
	for _, t := range r.data {
		all = append(all, *t)
	}
	total := len(all)
	if offset >= total {
		return nil, total, nil
	}
	end := min(offset+limit, total)
	return all[offset:end], total, nil
}

func (r *e2eTaskRepo) Update(_ context.Context, t *model.CallTask) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.data[t.ID]
	if !ok {
		return fmt.Errorf("task %d not found", t.ID)
	}
	existing.Name = t.Name
	existing.DailyLimit = t.DailyLimit
	existing.MaxConcurrent = t.MaxConcurrent
	existing.TotalContacts = t.TotalContacts
	return nil
}

func (r *e2eTaskRepo) UpdateStatus(_ context.Context, id int64, status engine.TaskStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.data[id]
	if !ok {
		return fmt.Errorf("task %d not found", id)
	}
	t.Status = status
	return nil
}

// --- 通话仓储 ---

type e2eCallRepo struct {
	mu     sync.RWMutex
	calls  map[int64]*model.Call
	turns  map[int64][]model.DialogueTurn
	nextID int64
}

func (r *e2eCallRepo) GetByID(_ context.Context, id int64) (*model.Call, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.calls[id]
	if !ok {
		return nil, nil
	}
	clone := *c
	return &clone, nil
}

func (r *e2eCallRepo) ListByTask(_ context.Context, taskID int64, offset, limit int) ([]model.Call, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var filtered []model.Call
	for _, c := range r.calls {
		if c.TaskID == taskID {
			filtered = append(filtered, *c)
		}
	}
	total := len(filtered)
	if offset >= total {
		return nil, total, nil
	}
	end := min(offset+limit, total)
	return filtered[offset:end], total, nil
}

func (r *e2eCallRepo) ListTurns(_ context.Context, callID int64) ([]model.DialogueTurn, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	turns := r.turns[callID]
	result := make([]model.DialogueTurn, len(turns))
	copy(result, turns)
	return result, nil
}

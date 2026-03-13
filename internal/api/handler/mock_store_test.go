package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/model"
	"github.com/omeyang/clarion/internal/service"
)

// memContactRepo 是用于测试的内存联系人仓储。
type memContactRepo struct {
	mu       sync.RWMutex
	contacts map[int64]*model.Contact
	nextID   int64
}

func newMemContactRepo() *memContactRepo {
	return &memContactRepo{contacts: make(map[int64]*model.Contact), nextID: 1}
}

func (s *memContactRepo) Create(_ context.Context, c *model.Contact) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID
	s.nextID++
	c.ID = id
	clone := *c
	s.contacts[id] = &clone
	return id, nil
}

func (s *memContactRepo) GetByID(_ context.Context, id int64) (*model.Contact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.contacts[id]
	if !ok {
		return nil, nil
	}
	clone := *c
	return &clone, nil
}

func (s *memContactRepo) List(_ context.Context, offset, limit int) ([]model.Contact, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var all []model.Contact
	for _, c := range s.contacts {
		all = append(all, *c)
	}
	total := len(all)
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

func (s *memContactRepo) UpdateStatus(_ context.Context, id int64, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.contacts[id]
	if !ok {
		return fmt.Errorf("contact %d not found", id)
	}
	c.CurrentStatus = status
	return nil
}

func (s *memContactRepo) BulkCreate(_ context.Context, contacts []model.Contact) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inserted := 0
	for _, c := range contacts {
		id := s.nextID
		s.nextID++
		c.ID = id
		clone := c
		s.contacts[id] = &clone
		inserted++
	}
	return inserted, nil
}

// memTemplateRepo 是用于测试的内存模板仓储。
type memTemplateRepo struct {
	mu         sync.RWMutex
	templates  map[int64]*model.ScenarioTemplate
	snapshots  map[int64]*model.TemplateSnapshot
	nextID     int64
	nextSnapID int64
}

func newMemTemplateRepo() *memTemplateRepo {
	return &memTemplateRepo{
		templates:  make(map[int64]*model.ScenarioTemplate),
		snapshots:  make(map[int64]*model.TemplateSnapshot),
		nextID:     1,
		nextSnapID: 1,
	}
}

func (s *memTemplateRepo) Create(_ context.Context, t *model.ScenarioTemplate) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID
	s.nextID++
	t.ID = id
	clone := *t
	s.templates[id] = &clone
	return id, nil
}

func (s *memTemplateRepo) GetByID(_ context.Context, id int64) (*model.ScenarioTemplate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.templates[id]
	if !ok {
		return nil, nil
	}
	clone := *t
	return &clone, nil
}

func (s *memTemplateRepo) List(_ context.Context, offset, limit int) ([]model.ScenarioTemplate, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var all []model.ScenarioTemplate
	for _, t := range s.templates {
		all = append(all, *t)
	}
	total := len(all)
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

func (s *memTemplateRepo) Update(_ context.Context, t *model.ScenarioTemplate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.templates[t.ID]
	if !ok {
		return fmt.Errorf("template %d not found", t.ID)
	}
	existing.Name = t.Name
	existing.Domain = t.Domain
	existing.OpeningScript = t.OpeningScript
	existing.Version++
	return nil
}

func (s *memTemplateRepo) UpdateStatus(_ context.Context, id int64, status engine.TemplateStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.templates[id]
	if !ok {
		return fmt.Errorf("template %d not found", id)
	}
	t.Status = status
	return nil
}

func (s *memTemplateRepo) CreateSnapshot(_ context.Context, snap *model.TemplateSnapshot) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextSnapID
	s.nextSnapID++
	snap.ID = id
	clone := *snap
	s.snapshots[id] = &clone
	return id, nil
}

func (s *memTemplateRepo) GetSnapshotByID(_ context.Context, id int64) (*model.TemplateSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, ok := s.snapshots[id]
	if !ok {
		return nil, nil
	}
	clone := *snap
	return &clone, nil
}

// memTaskRepo 是用于测试的内存任务仓储。
type memTaskRepo struct {
	mu     sync.RWMutex
	tasks  map[int64]*model.CallTask
	nextID int64
}

func newMemTaskRepo() *memTaskRepo {
	return &memTaskRepo{tasks: make(map[int64]*model.CallTask), nextID: 1}
}

func (s *memTaskRepo) Create(_ context.Context, t *model.CallTask) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID
	s.nextID++
	t.ID = id
	clone := *t
	s.tasks[id] = &clone
	return id, nil
}

func (s *memTaskRepo) GetByID(_ context.Context, id int64) (*model.CallTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, nil
	}
	clone := *t
	return &clone, nil
}

func (s *memTaskRepo) List(_ context.Context, offset, limit int) ([]model.CallTask, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var all []model.CallTask
	for _, t := range s.tasks {
		all = append(all, *t)
	}
	total := len(all)
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

func (s *memTaskRepo) Update(_ context.Context, t *model.CallTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.tasks[t.ID]
	if !ok {
		return fmt.Errorf("task %d not found", t.ID)
	}
	existing.Name = t.Name
	existing.DailyLimit = t.DailyLimit
	existing.MaxConcurrent = t.MaxConcurrent
	existing.TotalContacts = t.TotalContacts
	return nil
}

func (s *memTaskRepo) UpdateStatus(_ context.Context, id int64, status engine.TaskStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task %d not found", id)
	}
	t.Status = status
	return nil
}

// memCallRepo 是用于测试的内存通话仓储。
type memCallRepo struct {
	mu     sync.RWMutex
	calls  map[int64]*model.Call
	turns  map[int64][]model.DialogueTurn
	nextID int64
}

func newMemCallRepo() *memCallRepo {
	return &memCallRepo{
		calls:  make(map[int64]*model.Call),
		turns:  make(map[int64][]model.DialogueTurn),
		nextID: 1,
	}
}

func (s *memCallRepo) GetByID(_ context.Context, id int64) (*model.Call, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.calls[id]
	if !ok {
		return nil, nil
	}
	clone := *c
	return &clone, nil
}

func (s *memCallRepo) ListByTask(_ context.Context, taskID int64, offset, limit int) ([]model.Call, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var filtered []model.Call
	for _, c := range s.calls {
		if c.TaskID == taskID {
			filtered = append(filtered, *c)
		}
	}
	total := len(filtered)
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return filtered[offset:end], total, nil
}

func (s *memCallRepo) ListTurns(_ context.Context, callID int64) ([]model.DialogueTurn, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	turns := s.turns[callID]
	result := make([]model.DialogueTurn, len(turns))
	copy(result, turns)
	return result, nil
}

// create 是测试辅助方法，用于插入测试数据。
func (s *memCallRepo) create(c *model.Call) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID
	s.nextID++
	c.ID = id
	clone := *c
	s.calls[id] = &clone
}

// createTurn 是测试辅助方法，用于插入对话轮次。
func (s *memCallRepo) createTurn(turn *model.DialogueTurn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turns[turn.CallID] = append(s.turns[turn.CallID], *turn)
}

// errAlways 是一个固定返回错误的标记值。
var errAlways = errors.New("mock error")

// failContactRepo 所有操作均返回错误，用于测试 500 路径。
type failContactRepo struct{}

func (failContactRepo) Create(context.Context, *model.Contact) (int64, error) {
	return 0, errAlways
}
func (failContactRepo) GetByID(context.Context, int64) (*model.Contact, error) {
	return nil, errAlways
}
func (failContactRepo) List(context.Context, int, int) ([]model.Contact, int, error) {
	return nil, 0, errAlways
}
func (failContactRepo) UpdateStatus(context.Context, int64, string) error { return errAlways }
func (failContactRepo) BulkCreate(context.Context, []model.Contact) (int, error) {
	return 0, errAlways
}

// failCallRepo 所有操作均返回错误。
type failCallRepo struct{}

func (failCallRepo) GetByID(context.Context, int64) (*model.Call, error) {
	return nil, errAlways
}
func (failCallRepo) ListByTask(context.Context, int64, int, int) ([]model.Call, int, error) {
	return nil, 0, errAlways
}
func (failCallRepo) ListTurns(context.Context, int64) ([]model.DialogueTurn, error) {
	return nil, errAlways
}

// failTaskRepo 所有操作均返回错误。
type failTaskRepo struct{}

func (failTaskRepo) Create(context.Context, *model.CallTask) (int64, error) { return 0, errAlways }
func (failTaskRepo) GetByID(context.Context, int64) (*model.CallTask, error) {
	return nil, errAlways
}
func (failTaskRepo) List(context.Context, int, int) ([]model.CallTask, int, error) {
	return nil, 0, errAlways
}
func (failTaskRepo) Update(context.Context, *model.CallTask) error { return errAlways }
func (failTaskRepo) UpdateStatus(context.Context, int64, engine.TaskStatus) error {
	return errAlways
}

// failTemplateRepo 所有操作均返回错误。
type failTemplateRepo struct{}

func (failTemplateRepo) Create(context.Context, *model.ScenarioTemplate) (int64, error) {
	return 0, errAlways
}
func (failTemplateRepo) GetByID(context.Context, int64) (*model.ScenarioTemplate, error) {
	return nil, errAlways
}
func (failTemplateRepo) List(context.Context, int, int) ([]model.ScenarioTemplate, int, error) {
	return nil, 0, errAlways
}
func (failTemplateRepo) Update(context.Context, *model.ScenarioTemplate) error { return errAlways }
func (failTemplateRepo) UpdateStatus(context.Context, int64, engine.TemplateStatus) error {
	return errAlways
}
func (failTemplateRepo) CreateSnapshot(context.Context, *model.TemplateSnapshot) (int64, error) {
	return 0, errAlways
}
func (failTemplateRepo) GetSnapshotByID(context.Context, int64) (*model.TemplateSnapshot, error) {
	return nil, errAlways
}

// failTurnsCallRepo 仅 ListTurns 返回错误，GetByID/ListByTask 正常。
type failTurnsCallRepo struct {
	*memCallRepo
}

func (*failTurnsCallRepo) ListTurns(context.Context, int64) ([]model.DialogueTurn, error) {
	return nil, errAlways
}

// 以下辅助函数用于在测试中构建 service 实例。

func newContactSvc() (*service.ContactSvc, *memContactRepo) {
	repo := newMemContactRepo()
	return service.NewContactSvc(repo), repo
}

func newTemplateSvc() (*service.TemplateSvc, *memTemplateRepo) {
	repo := newMemTemplateRepo()
	return service.NewTemplateSvc(repo), repo
}

func newTaskSvc() (*service.TaskSvc, *memTaskRepo) {
	repo := newMemTaskRepo()
	return service.NewTaskSvc(repo), repo
}

func newCallSvc() (*service.CallSvc, *memCallRepo) {
	repo := newMemCallRepo()
	return service.NewCallSvc(repo), repo
}

// seedContact 创建带默认值的测试联系人。
func seedContact(repo *memContactRepo) {
	c := &model.Contact{
		PhoneMasked:   "138****1234",
		PhoneHash:     "hash123",
		Source:        "import",
		ProfileJSON:   json.RawMessage(`{"name":"test"}`),
		CurrentStatus: "new",
	}
	repo.Create(context.Background(), c)
}

// seedTemplate 创建带默认值的测试模板。
func seedTemplate(repo *memTemplateRepo) {
	t := &model.ScenarioTemplate{
		Name:                 "Test Template",
		Domain:               "sales",
		OpeningScript:        "Hello",
		StateMachineConfig:   json.RawMessage(`{}`),
		ExtractionSchema:     json.RawMessage(`{}`),
		GradingRules:         json.RawMessage(`{}`),
		PromptTemplates:      json.RawMessage(`{}`),
		NotificationConfig:   json.RawMessage(`{}`),
		CallProtectionConfig: json.RawMessage(`{}`),
		PrecompiledAudios:    json.RawMessage(`{}`),
		Status:               engine.TemplateDraft,
		Version:              1,
	}
	repo.Create(context.Background(), t)
}

// seedTask 创建带默认值的测试任务。
func seedTask(repo *memTaskRepo) {
	t := &model.CallTask{
		Name:               "Test Task",
		ScenarioTemplateID: 1,
		ContactFilter:      json.RawMessage(`{}`),
		ScheduleConfig:     json.RawMessage(`{}`),
		DailyLimit:         100,
		MaxConcurrent:      5,
		Status:             engine.TaskDraft,
	}
	repo.Create(context.Background(), t)
}

// seedCall 创建带默认值的测试通话记录。
func seedCall(repo *memCallRepo) {
	c := &model.Call{
		ContactID:       1,
		TaskID:          1,
		SessionID:       "sess-001",
		Status:          engine.CallPending,
		AnswerType:      engine.AnswerUnknown,
		ExtractedFields: json.RawMessage(`{}`),
		RuleTrace:       json.RawMessage(`{}`),
	}
	repo.create(c)
}

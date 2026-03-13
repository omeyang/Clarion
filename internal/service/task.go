package service

import (
	"context"
	"fmt"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/model"
)

// TaskRepo 定义任务服务所需的数据访问接口。
type TaskRepo interface {
	Create(ctx context.Context, t *model.CallTask) (int64, error)
	GetByID(ctx context.Context, id int64) (*model.CallTask, error)
	List(ctx context.Context, offset, limit int) ([]model.CallTask, int, error)
	Update(ctx context.Context, t *model.CallTask) error
	UpdateStatus(ctx context.Context, id int64, status engine.TaskStatus) error
}

// TaskSvc 封装外呼任务相关业务逻辑。
type TaskSvc struct {
	repo TaskRepo
}

// NewTaskSvc 创建任务服务实例。
func NewTaskSvc(repo TaskRepo) *TaskSvc {
	return &TaskSvc{repo: repo}
}

// Create 创建新的外呼任务，设置初始状态和默认并发数。
func (s *TaskSvc) Create(ctx context.Context, t *model.CallTask) (int64, error) {
	t.Status = engine.TaskDraft
	if t.MaxConcurrent == 0 {
		t.MaxConcurrent = 1
	}
	id, err := s.repo.Create(ctx, t)
	if err != nil {
		return 0, fmt.Errorf("创建任务: %w", err)
	}
	return id, nil
}

// GetByID 按 ID 获取任务。
func (s *TaskSvc) GetByID(ctx context.Context, id int64) (*model.CallTask, error) {
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("获取任务 %d: %w", id, err)
	}
	return t, nil
}

// List 分页获取任务列表。
func (s *TaskSvc) List(ctx context.Context, offset, limit int) ([]model.CallTask, int, error) {
	tasks, total, err := s.repo.List(ctx, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("列出任务: %w", err)
	}
	return tasks, total, nil
}

// Update 更新任务内容。
func (s *TaskSvc) Update(ctx context.Context, t *model.CallTask) error {
	if err := s.repo.Update(ctx, t); err != nil {
		return fmt.Errorf("更新任务 %d: %w", t.ID, err)
	}
	return nil
}

// UpdateStatus 更新任务状态。
func (s *TaskSvc) UpdateStatus(ctx context.Context, id int64, status engine.TaskStatus) error {
	if err := s.repo.UpdateStatus(ctx, id, status); err != nil {
		return fmt.Errorf("更新任务 %d 状态: %w", id, err)
	}
	return nil
}

// Start 启动任务，将状态设为 running。
func (s *TaskSvc) Start(ctx context.Context, id int64) error {
	if err := s.repo.UpdateStatus(ctx, id, engine.TaskRunning); err != nil {
		return fmt.Errorf("启动任务 %d: %w", id, err)
	}
	return nil
}

// Pause 暂停任务。
func (s *TaskSvc) Pause(ctx context.Context, id int64) error {
	if err := s.repo.UpdateStatus(ctx, id, engine.TaskPaused); err != nil {
		return fmt.Errorf("暂停任务 %d: %w", id, err)
	}
	return nil
}

// Cancel 取消任务。
func (s *TaskSvc) Cancel(ctx context.Context, id int64) error {
	if err := s.repo.UpdateStatus(ctx, id, engine.TaskCancelled); err != nil {
		return fmt.Errorf("取消任务 %d: %w", id, err)
	}
	return nil
}

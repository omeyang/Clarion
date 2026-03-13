package service

import (
	"context"
	"errors"
	"testing"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTaskRepo 是 TaskRepo 的测试替身。
type mockTaskRepo struct {
	create       func(ctx context.Context, t *model.CallTask) (int64, error)
	getByID      func(ctx context.Context, id int64) (*model.CallTask, error)
	list         func(ctx context.Context, offset, limit int) ([]model.CallTask, int, error)
	update       func(ctx context.Context, t *model.CallTask) error
	updateStatus func(ctx context.Context, id int64, status engine.TaskStatus) error
}

func (m *mockTaskRepo) Create(ctx context.Context, t *model.CallTask) (int64, error) {
	return m.create(ctx, t)
}

func (m *mockTaskRepo) GetByID(ctx context.Context, id int64) (*model.CallTask, error) {
	return m.getByID(ctx, id)
}

func (m *mockTaskRepo) List(ctx context.Context, offset, limit int) ([]model.CallTask, int, error) {
	return m.list(ctx, offset, limit)
}

func (m *mockTaskRepo) Update(ctx context.Context, t *model.CallTask) error {
	return m.update(ctx, t)
}

func (m *mockTaskRepo) UpdateStatus(ctx context.Context, id int64, status engine.TaskStatus) error {
	return m.updateStatus(ctx, id, status)
}

func TestTaskSvc_Create(t *testing.T) {
	tests := []struct {
		name           string
		task           *model.CallTask
		repo           *mockTaskRepo
		wantID         int64
		wantStatus     engine.TaskStatus
		wantConcurrent int
		wantErr        bool
	}{
		{
			name: "创建任务设置默认状态和并发数",
			task: &model.CallTask{Name: "测试任务"},
			repo: &mockTaskRepo{
				create: func(_ context.Context, task *model.CallTask) (int64, error) {
					if task.Status != engine.TaskDraft {
						return 0, errors.New("状态不是 draft")
					}
					if task.MaxConcurrent != 1 {
						return 0, errors.New("并发数不是 1")
					}
					return 1, nil
				},
			},
			wantID:         1,
			wantStatus:     engine.TaskDraft,
			wantConcurrent: 1,
		},
		{
			name: "已设置并发数时保留用户指定值",
			task: &model.CallTask{Name: "测试任务", MaxConcurrent: 5},
			repo: &mockTaskRepo{
				create: func(_ context.Context, task *model.CallTask) (int64, error) {
					if task.MaxConcurrent != 5 {
						return 0, errors.New("并发数不是 5")
					}
					return 2, nil
				},
			},
			wantID:         2,
			wantStatus:     engine.TaskDraft,
			wantConcurrent: 5,
		},
		{
			name: "仓库返回错误",
			task: &model.CallTask{},
			repo: &mockTaskRepo{
				create: func(_ context.Context, _ *model.CallTask) (int64, error) {
					return 0, errors.New("db error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewTaskSvc(tt.repo)
			id, err := svc.Create(context.Background(), tt.task)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "创建任务")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, id)
			assert.Equal(t, string(tt.wantStatus), string(tt.task.Status))
			assert.Equal(t, tt.wantConcurrent, tt.task.MaxConcurrent)
		})
	}
}

func TestTaskSvc_GetByID(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		repo    *mockTaskRepo
		want    *model.CallTask
		wantErr bool
	}{
		{
			name: "成功获取任务",
			id:   1,
			repo: &mockTaskRepo{
				getByID: func(_ context.Context, id int64) (*model.CallTask, error) {
					return &model.CallTask{ID: id, Name: "任务1"}, nil
				},
			},
			want: &model.CallTask{ID: 1, Name: "任务1"},
		},
		{
			name: "仓库返回错误时包装错误信息",
			id:   99,
			repo: &mockTaskRepo{
				getByID: func(_ context.Context, _ int64) (*model.CallTask, error) {
					return nil, errors.New("not found")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewTaskSvc(tt.repo)
			got, err := svc.GetByID(context.Background(), tt.id)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "获取任务")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTaskSvc_List(t *testing.T) {
	tests := []struct {
		name      string
		offset    int
		limit     int
		repo      *mockTaskRepo
		wantTasks []model.CallTask
		wantTotal int
		wantErr   bool
	}{
		{
			name:   "成功获取任务列表",
			offset: 0,
			limit:  10,
			repo: &mockTaskRepo{
				list: func(_ context.Context, _, _ int) ([]model.CallTask, int, error) {
					return []model.CallTask{{ID: 1}}, 1, nil
				},
			},
			wantTasks: []model.CallTask{{ID: 1}},
			wantTotal: 1,
		},
		{
			name: "仓库返回错误时包装错误信息",
			repo: &mockTaskRepo{
				list: func(_ context.Context, _, _ int) ([]model.CallTask, int, error) {
					return nil, 0, errors.New("db error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewTaskSvc(tt.repo)
			tasks, total, err := svc.List(context.Background(), tt.offset, tt.limit)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "列出任务")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantTasks, tasks)
			assert.Equal(t, tt.wantTotal, total)
		})
	}
}

func TestTaskSvc_Update(t *testing.T) {
	tests := []struct {
		name    string
		task    *model.CallTask
		repo    *mockTaskRepo
		wantErr bool
	}{
		{
			name: "成功更新任务",
			task: &model.CallTask{ID: 1, Name: "更新后的任务"},
			repo: &mockTaskRepo{
				update: func(_ context.Context, t *model.CallTask) error {
					return nil
				},
			},
		},
		{
			name: "仓库返回错误时包装错误信息",
			task: &model.CallTask{ID: 1},
			repo: &mockTaskRepo{
				update: func(_ context.Context, _ *model.CallTask) error {
					return errors.New("db error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewTaskSvc(tt.repo)
			err := svc.Update(context.Background(), tt.task)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "更新任务")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestTaskSvc_UpdateStatus(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		status  engine.TaskStatus
		repo    *mockTaskRepo
		wantErr bool
	}{
		{
			name:   "成功更新任务状态",
			id:     1,
			status: engine.TaskRunning,
			repo: &mockTaskRepo{
				updateStatus: func(_ context.Context, id int64, status engine.TaskStatus) error {
					assert.Equal(t, int64(1), id)
					assert.Equal(t, engine.TaskRunning, status)
					return nil
				},
			},
		},
		{
			name:   "仓库返回错误时包装错误信息",
			id:     1,
			status: engine.TaskRunning,
			repo: &mockTaskRepo{
				updateStatus: func(_ context.Context, _ int64, _ engine.TaskStatus) error {
					return errors.New("db error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewTaskSvc(tt.repo)
			err := svc.UpdateStatus(context.Background(), tt.id, tt.status)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "更新任务")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestTaskSvc_Start(t *testing.T) {
	tests := []struct {
		name       string
		id         int64
		repo       *mockTaskRepo
		wantStatus engine.TaskStatus
		wantErr    bool
	}{
		{
			name: "成功启动任务",
			id:   1,
			repo: &mockTaskRepo{
				updateStatus: func(_ context.Context, _ int64, status engine.TaskStatus) error {
					assert.Equal(t, engine.TaskRunning, status)
					return nil
				},
			},
		},
		{
			name: "仓库返回错误时包装错误信息",
			id:   1,
			repo: &mockTaskRepo{
				updateStatus: func(_ context.Context, _ int64, _ engine.TaskStatus) error {
					return errors.New("db error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewTaskSvc(tt.repo)
			err := svc.Start(context.Background(), tt.id)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "启动任务")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestTaskSvc_Pause(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		repo    *mockTaskRepo
		wantErr bool
	}{
		{
			name: "成功暂停任务",
			id:   1,
			repo: &mockTaskRepo{
				updateStatus: func(_ context.Context, _ int64, status engine.TaskStatus) error {
					assert.Equal(t, engine.TaskPaused, status)
					return nil
				},
			},
		},
		{
			name: "仓库返回错误时包装错误信息",
			id:   1,
			repo: &mockTaskRepo{
				updateStatus: func(_ context.Context, _ int64, _ engine.TaskStatus) error {
					return errors.New("db error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewTaskSvc(tt.repo)
			err := svc.Pause(context.Background(), tt.id)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "暂停任务")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestTaskSvc_Cancel(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		repo    *mockTaskRepo
		wantErr bool
	}{
		{
			name: "成功取消任务",
			id:   1,
			repo: &mockTaskRepo{
				updateStatus: func(_ context.Context, _ int64, status engine.TaskStatus) error {
					assert.Equal(t, engine.TaskCancelled, status)
					return nil
				},
			},
		},
		{
			name: "仓库返回错误时包装错误信息",
			id:   1,
			repo: &mockTaskRepo{
				updateStatus: func(_ context.Context, _ int64, _ engine.TaskStatus) error {
					return errors.New("db error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewTaskSvc(tt.repo)
			err := svc.Cancel(context.Background(), tt.id)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "取消任务")
				return
			}
			require.NoError(t, err)
		})
	}
}

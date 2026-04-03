package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/model"
	"github.com/omeyang/xkit/pkg/context/xtenant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTemplateRepo 是 TemplateRepo 的测试替身。
type mockTemplateRepo struct {
	create          func(ctx context.Context, t *model.ScenarioTemplate) (int64, error)
	getByID         func(ctx context.Context, id int64) (*model.ScenarioTemplate, error)
	list            func(ctx context.Context, tenantID string, offset, limit int) ([]model.ScenarioTemplate, int, error)
	update          func(ctx context.Context, t *model.ScenarioTemplate) error
	updateStatus    func(ctx context.Context, id int64, status engine.TemplateStatus) error
	createSnapshot  func(ctx context.Context, snap *model.TemplateSnapshot) (int64, error)
	getSnapshotByID func(ctx context.Context, id int64) (*model.TemplateSnapshot, error)
}

func (m *mockTemplateRepo) Create(ctx context.Context, t *model.ScenarioTemplate) (int64, error) {
	return m.create(ctx, t)
}

func (m *mockTemplateRepo) GetByID(ctx context.Context, id int64) (*model.ScenarioTemplate, error) {
	return m.getByID(ctx, id)
}

func (m *mockTemplateRepo) List(ctx context.Context, tenantID string, offset, limit int) ([]model.ScenarioTemplate, int, error) {
	return m.list(ctx, tenantID, offset, limit)
}

func (m *mockTemplateRepo) Update(ctx context.Context, t *model.ScenarioTemplate) error {
	return m.update(ctx, t)
}

func (m *mockTemplateRepo) UpdateStatus(ctx context.Context, id int64, status engine.TemplateStatus) error {
	return m.updateStatus(ctx, id, status)
}

func (m *mockTemplateRepo) CreateSnapshot(ctx context.Context, snap *model.TemplateSnapshot) (int64, error) {
	return m.createSnapshot(ctx, snap)
}

func (m *mockTemplateRepo) GetSnapshotByID(ctx context.Context, id int64) (*model.TemplateSnapshot, error) {
	return m.getSnapshotByID(ctx, id)
}

func TestTemplateSvc_Create(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    *model.ScenarioTemplate
		repo    *mockTemplateRepo
		wantID  int64
		wantErr bool
	}{
		{
			name: "创建模板设置初始状态和版本",
			tmpl: &model.ScenarioTemplate{Name: "测试模板"},
			repo: &mockTemplateRepo{
				create: func(_ context.Context, tmpl *model.ScenarioTemplate) (int64, error) {
					if tmpl.Status != engine.TemplateDraft {
						return 0, errors.New("状态不是 draft")
					}
					if tmpl.Version != 1 {
						return 0, errors.New("版本不是 1")
					}
					return 1, nil
				},
			},
			wantID: 1,
		},
		{
			name: "仓库返回错误时包装错误信息",
			tmpl: &model.ScenarioTemplate{},
			repo: &mockTemplateRepo{
				create: func(_ context.Context, _ *model.ScenarioTemplate) (int64, error) {
					return 0, errors.New("db error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewTemplateSvc(tt.repo)
			ctx, _ := xtenant.WithTenantID(context.Background(), "test-tenant")
			id, err := svc.Create(ctx, tt.tmpl)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "创建模板")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, id)
			assert.Equal(t, engine.TemplateDraft, tt.tmpl.Status)
			assert.Equal(t, 1, tt.tmpl.Version)
		})
	}
}

func TestTemplateSvc_GetByID(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		repo    *mockTemplateRepo
		want    *model.ScenarioTemplate
		wantErr bool
	}{
		{
			name: "成功获取模板",
			id:   1,
			repo: &mockTemplateRepo{
				getByID: func(_ context.Context, id int64) (*model.ScenarioTemplate, error) {
					return &model.ScenarioTemplate{ID: id, Name: "模板1"}, nil
				},
			},
			want: &model.ScenarioTemplate{ID: 1, Name: "模板1"},
		},
		{
			name: "仓库返回错误时包装错误信息",
			id:   99,
			repo: &mockTemplateRepo{
				getByID: func(_ context.Context, _ int64) (*model.ScenarioTemplate, error) {
					return nil, errors.New("not found")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewTemplateSvc(tt.repo)
			got, err := svc.GetByID(context.Background(), tt.id)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "获取模板")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTemplateSvc_List(t *testing.T) {
	tests := []struct {
		name      string
		offset    int
		limit     int
		repo      *mockTemplateRepo
		wantLen   int
		wantTotal int
		wantErr   bool
	}{
		{
			name:   "成功获取模板列表",
			offset: 0,
			limit:  10,
			repo: &mockTemplateRepo{
				list: func(_ context.Context, _ string, _, _ int) ([]model.ScenarioTemplate, int, error) {
					return []model.ScenarioTemplate{{ID: 1}, {ID: 2}}, 2, nil
				},
			},
			wantLen:   2,
			wantTotal: 2,
		},
		{
			name: "仓库返回错误时包装错误信息",
			repo: &mockTemplateRepo{
				list: func(_ context.Context, _ string, _, _ int) ([]model.ScenarioTemplate, int, error) {
					return nil, 0, errors.New("db error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewTemplateSvc(tt.repo)
			ctx, _ := xtenant.WithTenantID(context.Background(), "test-tenant")
			templates, total, err := svc.List(ctx, tt.offset, tt.limit)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "列出模板")
				return
			}
			require.NoError(t, err)
			assert.Len(t, templates, tt.wantLen)
			assert.Equal(t, tt.wantTotal, total)
		})
	}
}

func TestTemplateSvc_Update(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    *model.ScenarioTemplate
		repo    *mockTemplateRepo
		wantErr bool
	}{
		{
			name: "成功更新模板",
			tmpl: &model.ScenarioTemplate{ID: 1, Name: "更新后的模板"},
			repo: &mockTemplateRepo{
				update: func(_ context.Context, _ *model.ScenarioTemplate) error {
					return nil
				},
			},
		},
		{
			name: "仓库返回错误时包装错误信息",
			tmpl: &model.ScenarioTemplate{ID: 1},
			repo: &mockTemplateRepo{
				update: func(_ context.Context, _ *model.ScenarioTemplate) error {
					return errors.New("db error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewTemplateSvc(tt.repo)
			err := svc.Update(context.Background(), tt.tmpl)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "更新模板")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestTemplateSvc_UpdateStatus(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		status  engine.TemplateStatus
		repo    *mockTemplateRepo
		wantErr bool
	}{
		{
			name:   "成功更新模板状态",
			id:     1,
			status: engine.TemplateActive,
			repo: &mockTemplateRepo{
				updateStatus: func(_ context.Context, id int64, status engine.TemplateStatus) error {
					assert.Equal(t, int64(1), id)
					assert.Equal(t, engine.TemplateActive, status)
					return nil
				},
			},
		},
		{
			name:   "仓库返回错误时包装错误信息",
			id:     1,
			status: engine.TemplateActive,
			repo: &mockTemplateRepo{
				updateStatus: func(_ context.Context, _ int64, _ engine.TemplateStatus) error {
					return errors.New("db error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewTemplateSvc(tt.repo)
			err := svc.UpdateStatus(context.Background(), tt.id, tt.status)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "更新模板")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestTemplateSvc_Publish(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		repo    *mockTemplateRepo
		wantErr bool
		errMsg  string
	}{
		{
			name: "成功发布模板并创建快照",
			id:   1,
			repo: &mockTemplateRepo{
				getByID: func(_ context.Context, id int64) (*model.ScenarioTemplate, error) {
					return &model.ScenarioTemplate{ID: id, Name: "模板1", Version: 1}, nil
				},
				createSnapshot: func(_ context.Context, snap *model.TemplateSnapshot) (int64, error) {
					assert.Equal(t, int64(1), snap.TemplateID)
					// 验证快照数据可以正常反序列化
					var tmpl model.ScenarioTemplate
					require.NoError(t, json.Unmarshal(snap.SnapshotData, &tmpl))
					assert.Equal(t, "模板1", tmpl.Name)
					return 100, nil
				},
				updateStatus: func(_ context.Context, id int64, status engine.TemplateStatus) error {
					assert.Equal(t, int64(1), id)
					assert.Equal(t, engine.TemplatePublished, status)
					return nil
				},
			},
		},
		{
			name: "模板不存在时返回 ErrNotFound",
			id:   99,
			repo: &mockTemplateRepo{
				getByID: func(_ context.Context, _ int64) (*model.ScenarioTemplate, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errMsg:  "资源不存在",
		},
		{
			name: "获取模板失败时包装错误",
			id:   1,
			repo: &mockTemplateRepo{
				getByID: func(_ context.Context, _ int64) (*model.ScenarioTemplate, error) {
					return nil, errors.New("db error")
				},
			},
			wantErr: true,
			errMsg:  "获取模板",
		},
		{
			name: "创建快照失败时包装错误",
			id:   1,
			repo: &mockTemplateRepo{
				getByID: func(_ context.Context, id int64) (*model.ScenarioTemplate, error) {
					return &model.ScenarioTemplate{ID: id, Name: "模板1"}, nil
				},
				createSnapshot: func(_ context.Context, _ *model.TemplateSnapshot) (int64, error) {
					return 0, errors.New("snapshot error")
				},
			},
			wantErr: true,
			errMsg:  "创建快照",
		},
		{
			name: "更新状态失败时包装错误",
			id:   1,
			repo: &mockTemplateRepo{
				getByID: func(_ context.Context, id int64) (*model.ScenarioTemplate, error) {
					return &model.ScenarioTemplate{ID: id, Name: "模板1"}, nil
				},
				createSnapshot: func(_ context.Context, _ *model.TemplateSnapshot) (int64, error) {
					return 100, nil
				},
				updateStatus: func(_ context.Context, _ int64, _ engine.TemplateStatus) error {
					return errors.New("status error")
				},
			},
			wantErr: true,
			errMsg:  "更新状态",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewTemplateSvc(tt.repo)
			snap, err := svc.Publish(context.Background(), tt.id)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}
			require.NoError(t, err)
			require.NotNil(t, snap)
			assert.Equal(t, int64(100), snap.ID)
			assert.Equal(t, tt.id, snap.TemplateID)
			assert.NotEmpty(t, snap.SnapshotData)
		})
	}
}

func TestTemplateSvc_GetSnapshot(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		repo    *mockTemplateRepo
		want    *model.TemplateSnapshot
		wantErr bool
	}{
		{
			name: "成功获取快照",
			id:   100,
			repo: &mockTemplateRepo{
				getSnapshotByID: func(_ context.Context, id int64) (*model.TemplateSnapshot, error) {
					return &model.TemplateSnapshot{ID: id, TemplateID: 1}, nil
				},
			},
			want: &model.TemplateSnapshot{ID: 100, TemplateID: 1},
		},
		{
			name: "仓库返回错误时包装错误信息",
			id:   99,
			repo: &mockTemplateRepo{
				getSnapshotByID: func(_ context.Context, _ int64) (*model.TemplateSnapshot, error) {
					return nil, errors.New("not found")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewTemplateSvc(tt.repo)
			got, err := svc.GetSnapshot(context.Background(), tt.id)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "获取快照")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Package service 提供业务编排层，协调领域逻辑与数据访问。
package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/model"
	"github.com/omeyang/xkit/pkg/context/xtenant"
)

// TemplateRepo 定义模板服务所需的数据访问接口。
type TemplateRepo interface {
	Create(ctx context.Context, t *model.ScenarioTemplate) (int64, error)
	GetByID(ctx context.Context, id int64) (*model.ScenarioTemplate, error)
	List(ctx context.Context, tenantID string, offset, limit int) ([]model.ScenarioTemplate, int, error)
	Update(ctx context.Context, t *model.ScenarioTemplate) error
	UpdateStatus(ctx context.Context, id int64, status engine.TemplateStatus) error
	CreateSnapshot(ctx context.Context, snap *model.TemplateSnapshot) (int64, error)
	GetSnapshotByID(ctx context.Context, id int64) (*model.TemplateSnapshot, error)
}

// TemplateSvc 封装场景模板相关业务逻辑。
type TemplateSvc struct {
	repo TemplateRepo
}

// NewTemplateSvc 创建模板服务实例。
func NewTemplateSvc(repo TemplateRepo) *TemplateSvc {
	return &TemplateSvc{repo: repo}
}

// Create 创建新的场景模板，设置初始状态和版本。
func (s *TemplateSvc) Create(ctx context.Context, t *model.ScenarioTemplate) (int64, error) {
	tenantID, err := xtenant.RequireTenantID(ctx)
	if err != nil {
		return 0, fmt.Errorf("获取租户 ID: %w", err)
	}
	t.TenantID = tenantID
	t.Status = engine.TemplateDraft
	t.Version = 1
	id, err := s.repo.Create(ctx, t)
	if err != nil {
		return 0, fmt.Errorf("创建模板: %w", err)
	}
	return id, nil
}

// GetByID 按 ID 获取模板。
func (s *TemplateSvc) GetByID(ctx context.Context, id int64) (*model.ScenarioTemplate, error) {
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("获取模板 %d: %w", id, err)
	}
	return t, nil
}

// List 分页获取当前租户的模板列表。
func (s *TemplateSvc) List(ctx context.Context, offset, limit int) ([]model.ScenarioTemplate, int, error) {
	tenantID, err := xtenant.RequireTenantID(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("获取租户 ID: %w", err)
	}
	templates, total, err := s.repo.List(ctx, tenantID, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("列出模板: %w", err)
	}
	return templates, total, nil
}

// Update 更新模板内容。
func (s *TemplateSvc) Update(ctx context.Context, t *model.ScenarioTemplate) error {
	if err := s.repo.Update(ctx, t); err != nil {
		return fmt.Errorf("更新模板 %d: %w", t.ID, err)
	}
	return nil
}

// UpdateStatus 更新模板状态。
func (s *TemplateSvc) UpdateStatus(ctx context.Context, id int64, status engine.TemplateStatus) error {
	if err := s.repo.UpdateStatus(ctx, id, status); err != nil {
		return fmt.Errorf("更新模板 %d 状态: %w", id, err)
	}
	return nil
}

// Publish 发布模板：创建不可变快照并将状态设为 published。
func (s *TemplateSvc) Publish(ctx context.Context, id int64) (*model.TemplateSnapshot, error) {
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("获取模板: %w", err)
	}
	if t == nil {
		return nil, ErrNotFound
	}

	snapData, err := json.Marshal(t)
	if err != nil {
		return nil, fmt.Errorf("序列化模板: %w", err)
	}

	snap := &model.TemplateSnapshot{
		TemplateID:   id,
		SnapshotData: snapData,
	}

	snapID, err := s.repo.CreateSnapshot(ctx, snap)
	if err != nil {
		return nil, fmt.Errorf("创建快照: %w", err)
	}
	snap.ID = snapID

	if err := s.repo.UpdateStatus(ctx, id, engine.TemplatePublished); err != nil {
		return nil, fmt.Errorf("更新状态: %w", err)
	}

	return snap, nil
}

// GetSnapshot 按 ID 获取模板快照。
func (s *TemplateSvc) GetSnapshot(ctx context.Context, id int64) (*model.TemplateSnapshot, error) {
	snap, err := s.repo.GetSnapshotByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("获取快照 %d: %w", id, err)
	}
	return snap, nil
}

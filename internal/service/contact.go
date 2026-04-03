package service

import (
	"context"
	"fmt"

	"github.com/omeyang/clarion/internal/model"
	"github.com/omeyang/xkit/pkg/context/xtenant"
)

// ContactRepo 定义联系人服务所需的数据访问接口。
type ContactRepo interface {
	Create(ctx context.Context, c *model.Contact) (int64, error)
	GetByID(ctx context.Context, id int64) (*model.Contact, error)
	List(ctx context.Context, tenantID string, offset, limit int) ([]model.Contact, int, error)
	UpdateStatus(ctx context.Context, id int64, status string) error
	BulkCreate(ctx context.Context, contacts []model.Contact) (int, error)
}

// ContactSvc 封装联系人相关业务逻辑。
type ContactSvc struct {
	repo ContactRepo
}

// NewContactSvc 创建联系人服务实例。
func NewContactSvc(repo ContactRepo) *ContactSvc {
	return &ContactSvc{repo: repo}
}

// Create 创建新联系人，设置初始状态。
func (s *ContactSvc) Create(ctx context.Context, c *model.Contact) (int64, error) {
	tenantID, err := xtenant.RequireTenantID(ctx)
	if err != nil {
		return 0, fmt.Errorf("获取租户 ID: %w", err)
	}
	c.TenantID = tenantID
	c.CurrentStatus = "new"

	id, err := s.repo.Create(ctx, c)
	if err != nil {
		return 0, fmt.Errorf("创建联系人: %w", err)
	}
	return id, nil
}

// GetByID 按 ID 获取联系人。
func (s *ContactSvc) GetByID(ctx context.Context, id int64) (*model.Contact, error) {
	c, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("获取联系人 %d: %w", id, err)
	}
	return c, nil
}

// List 分页获取当前租户的联系人列表。
func (s *ContactSvc) List(ctx context.Context, offset, limit int) ([]model.Contact, int, error) {
	tenantID, err := xtenant.RequireTenantID(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("获取租户 ID: %w", err)
	}
	contacts, total, err := s.repo.List(ctx, tenantID, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("列出联系人: %w", err)
	}
	return contacts, total, nil
}

// UpdateStatus 更新联系人状态。
func (s *ContactSvc) UpdateStatus(ctx context.Context, id int64, status string) error {
	if err := s.repo.UpdateStatus(ctx, id, status); err != nil {
		return fmt.Errorf("更新联系人 %d 状态: %w", id, err)
	}
	return nil
}

// BulkCreate 批量创建联系人。
func (s *ContactSvc) BulkCreate(ctx context.Context, contacts []model.Contact) (int, error) {
	tenantID, err := xtenant.RequireTenantID(ctx)
	if err != nil {
		return 0, fmt.Errorf("获取租户 ID: %w", err)
	}
	for i := range contacts {
		contacts[i].TenantID = tenantID
		contacts[i].CurrentStatus = "new"
	}
	n, err := s.repo.BulkCreate(ctx, contacts)
	if err != nil {
		return 0, fmt.Errorf("批量创建联系人: %w", err)
	}
	return n, nil
}

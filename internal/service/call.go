package service

import (
	"context"
	"fmt"

	"github.com/omeyang/clarion/internal/model"
	"github.com/omeyang/xkit/pkg/context/xtenant"
)

// CallRepo 定义通话服务所需的数据访问接口。
type CallRepo interface {
	GetByID(ctx context.Context, id int64) (*model.Call, error)
	ListByTask(ctx context.Context, tenantID string, taskID int64, offset, limit int) ([]model.Call, int, error)
	ListTurns(ctx context.Context, callID int64) ([]model.DialogueTurn, error)
}

// CallSvc 封装通话记录相关业务逻辑。
type CallSvc struct {
	repo CallRepo
}

// NewCallSvc 创建通话服务实例。
func NewCallSvc(repo CallRepo) *CallSvc {
	return &CallSvc{repo: repo}
}

// GetByID 按 ID 获取通话记录。
func (s *CallSvc) GetByID(ctx context.Context, id int64) (*model.Call, error) {
	c, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("获取通话 %d: %w", id, err)
	}
	return c, nil
}

// ListByTask 按任务 ID 分页获取当前租户的通话列表。
func (s *CallSvc) ListByTask(ctx context.Context, taskID int64, offset, limit int) ([]model.Call, int, error) {
	tenantID, err := xtenant.RequireTenantID(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("获取租户 ID: %w", err)
	}
	calls, total, err := s.repo.ListByTask(ctx, tenantID, taskID, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("列出任务 %d 的通话: %w", taskID, err)
	}
	return calls, total, nil
}

// ListTurns 获取通话的对话轮次列表。
func (s *CallSvc) ListTurns(ctx context.Context, callID int64) ([]model.DialogueTurn, error) {
	turns, err := s.repo.ListTurns(ctx, callID)
	if err != nil {
		return nil, fmt.Errorf("列出通话 %d 的轮次: %w", callID, err)
	}
	return turns, nil
}

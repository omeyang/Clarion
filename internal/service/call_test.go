package service

import (
	"context"
	"errors"
	"testing"

	"github.com/omeyang/clarion/internal/model"
	"github.com/omeyang/xkit/pkg/context/xtenant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCallRepo 是 CallRepo 的测试替身。
type mockCallRepo struct {
	getByID    func(ctx context.Context, id int64) (*model.Call, error)
	listByTask func(ctx context.Context, tenantID string, taskID int64, offset, limit int) ([]model.Call, int, error)
	listTurns  func(ctx context.Context, callID int64) ([]model.DialogueTurn, error)
}

func (m *mockCallRepo) GetByID(ctx context.Context, id int64) (*model.Call, error) {
	return m.getByID(ctx, id)
}

func (m *mockCallRepo) ListByTask(ctx context.Context, tenantID string, taskID int64, offset, limit int) ([]model.Call, int, error) {
	return m.listByTask(ctx, tenantID, taskID, offset, limit)
}

func (m *mockCallRepo) ListTurns(ctx context.Context, callID int64) ([]model.DialogueTurn, error) {
	return m.listTurns(ctx, callID)
}

func TestCallSvc_GetByID(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		repo    *mockCallRepo
		want    *model.Call
		wantErr bool
	}{
		{
			name: "成功获取通话记录",
			id:   1,
			repo: &mockCallRepo{
				getByID: func(_ context.Context, id int64) (*model.Call, error) {
					return &model.Call{ID: id, SessionID: "sess-1"}, nil
				},
			},
			want: &model.Call{ID: 1, SessionID: "sess-1"},
		},
		{
			name: "仓库返回错误时包装错误信息",
			id:   99,
			repo: &mockCallRepo{
				getByID: func(_ context.Context, _ int64) (*model.Call, error) {
					return nil, errors.New("db error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewCallSvc(tt.repo)
			got, err := svc.GetByID(context.Background(), tt.id)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "获取通话")
				assert.ErrorIs(t, err, errors.Unwrap(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCallSvc_ListByTask(t *testing.T) {
	tests := []struct {
		name      string
		taskID    int64
		offset    int
		limit     int
		repo      *mockCallRepo
		wantCalls []model.Call
		wantTotal int
		wantErr   bool
	}{
		{
			name:   "成功获取任务下的通话列表",
			taskID: 10,
			offset: 0,
			limit:  20,
			repo: &mockCallRepo{
				listByTask: func(_ context.Context, _ string, _ int64, _, _ int) ([]model.Call, int, error) {
					return []model.Call{{ID: 1}, {ID: 2}}, 2, nil
				},
			},
			wantCalls: []model.Call{{ID: 1}, {ID: 2}},
			wantTotal: 2,
		},
		{
			name:   "仓库返回错误时包装错误信息",
			taskID: 10,
			repo: &mockCallRepo{
				listByTask: func(_ context.Context, _ string, _ int64, _, _ int) ([]model.Call, int, error) {
					return nil, 0, errors.New("db error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewCallSvc(tt.repo)
			ctx, _ := xtenant.WithTenantID(context.Background(), "test-tenant")
			calls, total, err := svc.ListByTask(ctx, tt.taskID, tt.offset, tt.limit)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "列出任务")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantCalls, calls)
			assert.Equal(t, tt.wantTotal, total)
		})
	}
}

func TestCallSvc_ListTurns(t *testing.T) {
	tests := []struct {
		name    string
		callID  int64
		repo    *mockCallRepo
		want    []model.DialogueTurn
		wantErr bool
	}{
		{
			name:   "成功获取对话轮次列表",
			callID: 1,
			repo: &mockCallRepo{
				listTurns: func(_ context.Context, _ int64) ([]model.DialogueTurn, error) {
					return []model.DialogueTurn{
						{ID: 1, TurnNumber: 1, Speaker: "bot"},
						{ID: 2, TurnNumber: 2, Speaker: "user"},
					}, nil
				},
			},
			want: []model.DialogueTurn{
				{ID: 1, TurnNumber: 1, Speaker: "bot"},
				{ID: 2, TurnNumber: 2, Speaker: "user"},
			},
		},
		{
			name:   "仓库返回错误时包装错误信息",
			callID: 1,
			repo: &mockCallRepo{
				listTurns: func(_ context.Context, _ int64) ([]model.DialogueTurn, error) {
					return nil, errors.New("db error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewCallSvc(tt.repo)
			got, err := svc.ListTurns(context.Background(), tt.callID)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "列出通话")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

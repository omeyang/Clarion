package service

import (
	"context"
	"errors"
	"testing"

	"github.com/omeyang/clarion/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockContactRepo 是 ContactRepo 的测试替身。
type mockContactRepo struct {
	create       func(ctx context.Context, c *model.Contact) (int64, error)
	getByID      func(ctx context.Context, id int64) (*model.Contact, error)
	list         func(ctx context.Context, offset, limit int) ([]model.Contact, int, error)
	updateStatus func(ctx context.Context, id int64, status string) error
	bulkCreate   func(ctx context.Context, contacts []model.Contact) (int, error)
}

func (m *mockContactRepo) Create(ctx context.Context, c *model.Contact) (int64, error) {
	return m.create(ctx, c)
}

func (m *mockContactRepo) GetByID(ctx context.Context, id int64) (*model.Contact, error) {
	return m.getByID(ctx, id)
}

func (m *mockContactRepo) List(ctx context.Context, offset, limit int) ([]model.Contact, int, error) {
	return m.list(ctx, offset, limit)
}

func (m *mockContactRepo) UpdateStatus(ctx context.Context, id int64, status string) error {
	return m.updateStatus(ctx, id, status)
}

func (m *mockContactRepo) BulkCreate(ctx context.Context, contacts []model.Contact) (int, error) {
	return m.bulkCreate(ctx, contacts)
}

func TestContactSvc_Create(t *testing.T) {
	tests := []struct {
		name    string
		contact *model.Contact
		repo    *mockContactRepo
		wantID  int64
		wantErr bool
	}{
		{
			name:    "创建联系人并设置初始状态为 new",
			contact: &model.Contact{PhoneMasked: "138****0001"},
			repo: &mockContactRepo{
				create: func(_ context.Context, c *model.Contact) (int64, error) {
					assert.Equal(t, "new", c.CurrentStatus)
					return 1, nil
				},
			},
			wantID: 1,
		},
		{
			name:    "仓库返回错误时包装错误信息",
			contact: &model.Contact{},
			repo: &mockContactRepo{
				create: func(_ context.Context, _ *model.Contact) (int64, error) {
					return 0, errors.New("db error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewContactSvc(tt.repo)
			id, err := svc.Create(context.Background(), tt.contact)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "创建联系人")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, id)
			assert.Equal(t, "new", tt.contact.CurrentStatus)
		})
	}
}

func TestContactSvc_GetByID(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		repo    *mockContactRepo
		want    *model.Contact
		wantErr bool
	}{
		{
			name: "成功获取联系人",
			id:   1,
			repo: &mockContactRepo{
				getByID: func(_ context.Context, id int64) (*model.Contact, error) {
					return &model.Contact{ID: id, PhoneMasked: "138****0001"}, nil
				},
			},
			want: &model.Contact{ID: 1, PhoneMasked: "138****0001"},
		},
		{
			name: "仓库返回错误时包装错误信息",
			id:   99,
			repo: &mockContactRepo{
				getByID: func(_ context.Context, _ int64) (*model.Contact, error) {
					return nil, errors.New("not found")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewContactSvc(tt.repo)
			got, err := svc.GetByID(context.Background(), tt.id)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "获取联系人")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestContactSvc_List(t *testing.T) {
	tests := []struct {
		name         string
		offset       int
		limit        int
		repo         *mockContactRepo
		wantContacts []model.Contact
		wantTotal    int
		wantErr      bool
	}{
		{
			name:   "成功获取联系人列表",
			offset: 0,
			limit:  10,
			repo: &mockContactRepo{
				list: func(_ context.Context, _, _ int) ([]model.Contact, int, error) {
					return []model.Contact{{ID: 1}, {ID: 2}}, 5, nil
				},
			},
			wantContacts: []model.Contact{{ID: 1}, {ID: 2}},
			wantTotal:    5,
		},
		{
			name: "仓库返回错误时包装错误信息",
			repo: &mockContactRepo{
				list: func(_ context.Context, _, _ int) ([]model.Contact, int, error) {
					return nil, 0, errors.New("db error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewContactSvc(tt.repo)
			contacts, total, err := svc.List(context.Background(), tt.offset, tt.limit)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "列出联系人")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantContacts, contacts)
			assert.Equal(t, tt.wantTotal, total)
		})
	}
}

func TestContactSvc_UpdateStatus(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		status  string
		repo    *mockContactRepo
		wantErr bool
	}{
		{
			name:   "成功更新联系人状态",
			id:     1,
			status: "called",
			repo: &mockContactRepo{
				updateStatus: func(_ context.Context, id int64, status string) error {
					assert.Equal(t, int64(1), id)
					assert.Equal(t, "called", status)
					return nil
				},
			},
		},
		{
			name:   "仓库返回错误时包装错误信息",
			id:     1,
			status: "called",
			repo: &mockContactRepo{
				updateStatus: func(_ context.Context, _ int64, _ string) error {
					return errors.New("db error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewContactSvc(tt.repo)
			err := svc.UpdateStatus(context.Background(), tt.id, tt.status)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "更新联系人")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestContactSvc_BulkCreate(t *testing.T) {
	tests := []struct {
		name     string
		contacts []model.Contact
		repo     *mockContactRepo
		wantN    int
		wantErr  bool
	}{
		{
			name: "批量创建联系人并全部设置状态为 new",
			contacts: []model.Contact{
				{PhoneMasked: "138****0001"},
				{PhoneMasked: "138****0002", CurrentStatus: "old"},
			},
			repo: &mockContactRepo{
				bulkCreate: func(_ context.Context, cs []model.Contact) (int, error) {
					// 验证所有联系人状态都被设置为 new
					for _, c := range cs {
						assert.Equal(t, "new", c.CurrentStatus)
					}
					return len(cs), nil
				},
			},
			wantN: 2,
		},
		{
			name:     "仓库返回错误时包装错误信息",
			contacts: []model.Contact{{PhoneMasked: "138****0001"}},
			repo: &mockContactRepo{
				bulkCreate: func(_ context.Context, _ []model.Contact) (int, error) {
					return 0, errors.New("db error")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewContactSvc(tt.repo)
			n, err := svc.BulkCreate(context.Background(), tt.contacts)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "批量创建联系人")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantN, n)
		})
	}
}

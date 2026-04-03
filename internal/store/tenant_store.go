package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/omeyang/clarion/internal/auth"
	"github.com/omeyang/clarion/internal/model"
)

// 编译时接口一致性检查。
var _ auth.TenantStore = (*PgTenantStore)(nil)

// PgTenantStore 提供基于 PostgreSQL 的租户数据操作。
type PgTenantStore struct {
	pool PoolQuerier
}

// NewPgTenantStore 创建新的 PgTenantStore。
func NewPgTenantStore(pool PoolQuerier) *PgTenantStore {
	return &PgTenantStore{pool: pool}
}

// Create 插入新租户。
func (s *PgTenantStore) Create(ctx context.Context, t *model.Tenant) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO tenants (id, slug, name, contact_person, contact_phone, status,
		    daily_call_limit, max_concurrent, settings)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		t.ID, t.Slug, t.Name, t.ContactPerson, t.ContactPhone, t.Status,
		t.DailyCallLimit, t.MaxConcurrent, t.Settings,
	)
	if err != nil {
		return fmt.Errorf("create tenant: %w", err)
	}
	return nil
}

// GetByID 按 UUID 查询租户，同时满足 auth.TenantStore 接口。
func (s *PgTenantStore) GetByID(ctx context.Context, id string) (*auth.TenantRecord, error) {
	var rec auth.TenantRecord
	err := s.pool.QueryRow(ctx,
		`SELECT id, status FROM tenants WHERE id = $1`, id,
	).Scan(&rec.ID, &rec.Status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("tenant %s not found", id)
		}
		return nil, fmt.Errorf("get tenant %s: %w", id, err)
	}
	return &rec, nil
}

// GetFull 按 UUID 查询完整租户信息。
func (s *PgTenantStore) GetFull(ctx context.Context, id string) (*model.Tenant, error) {
	t := &model.Tenant{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, slug, name, contact_person, contact_phone, status,
		    daily_call_limit, max_concurrent, settings, created_at, updated_at
		 FROM tenants WHERE id = $1`, id,
	).Scan(&t.ID, &t.Slug, &t.Name, &t.ContactPerson, &t.ContactPhone, &t.Status,
		&t.DailyCallLimit, &t.MaxConcurrent, &t.Settings, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get tenant %s: %w", id, err)
	}
	return t, nil
}

// GetBySlug 按 slug 查询完整租户信息。
func (s *PgTenantStore) GetBySlug(ctx context.Context, slug string) (*model.Tenant, error) {
	t := &model.Tenant{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, slug, name, contact_person, contact_phone, status,
		    daily_call_limit, max_concurrent, settings, created_at, updated_at
		 FROM tenants WHERE slug = $1`, slug,
	).Scan(&t.ID, &t.Slug, &t.Name, &t.ContactPerson, &t.ContactPhone, &t.Status,
		&t.DailyCallLimit, &t.MaxConcurrent, &t.Settings, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get tenant by slug %s: %w", slug, err)
	}
	return t, nil
}

// List 返回所有租户。
func (s *PgTenantStore) List(ctx context.Context) ([]model.Tenant, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, slug, name, contact_person, contact_phone, status,
		    daily_call_limit, max_concurrent, settings, created_at, updated_at
		 FROM tenants ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []model.Tenant
	for rows.Next() {
		var t model.Tenant
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.ContactPerson, &t.ContactPhone, &t.Status,
			&t.DailyCallLimit, &t.MaxConcurrent, &t.Settings, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}
		tenants = append(tenants, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenants: %w", err)
	}
	return tenants, nil
}

// UpdateStatus 变更租户状态。
func (s *PgTenantStore) UpdateStatus(ctx context.Context, id, status string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE tenants SET status = $1, updated_at = $2 WHERE id = $3`,
		status, time.Now(), id)
	if err != nil {
		return fmt.Errorf("update tenant status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tenant %s not found", id)
	}
	return nil
}

// UpdateQuota 更新租户配额。
func (s *PgTenantStore) UpdateQuota(ctx context.Context, id string, dailyLimit, maxConcurrent int) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE tenants SET daily_call_limit = $1, max_concurrent = $2, updated_at = $3 WHERE id = $4`,
		dailyLimit, maxConcurrent, time.Now(), id)
	if err != nil {
		return fmt.Errorf("update tenant quota: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tenant %s not found", id)
	}
	return nil
}

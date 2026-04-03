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
var _ auth.APIKeyStore = (*PgAPIKeyStore)(nil)

// PgAPIKeyStore 提供基于 PostgreSQL 的 API Key 数据操作。
type PgAPIKeyStore struct {
	pool PoolQuerier
}

// NewPgAPIKeyStore 创建新的 PgAPIKeyStore。
func NewPgAPIKeyStore(pool PoolQuerier) *PgAPIKeyStore {
	return &PgAPIKeyStore{pool: pool}
}

// Create 插入新 API Key，返回其 ID。
func (s *PgAPIKeyStore) Create(ctx context.Context, key *model.APIKey) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO api_keys (tenant_id, key_prefix, key_hash, name, status)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		key.TenantID, key.KeyPrefix, key.KeyHash, key.Name, key.Status,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create api key: %w", err)
	}
	return id, nil
}

// GetByHash 按 SHA-256 哈希查找 API Key，满足 auth.APIKeyStore 接口。
func (s *PgAPIKeyStore) GetByHash(ctx context.Context, hash string) (*auth.KeyRecord, error) {
	var rec auth.KeyRecord
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, status FROM api_keys WHERE key_hash = $1`, hash,
	).Scan(&rec.ID, &rec.TenantID, &rec.Status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("api key not found")
		}
		return nil, fmt.Errorf("get api key by hash: %w", err)
	}
	return &rec, nil
}

// TouchLastUsed 更新 API Key 的最近使用时间。
func (s *PgAPIKeyStore) TouchLastUsed(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE api_keys SET last_used_at = $1 WHERE id = $2`,
		time.Now(), id)
	if err != nil {
		return fmt.Errorf("touch api key last_used_at: %w", err)
	}
	return nil
}

// ListByTenant 返回指定租户的所有 API Key。
func (s *PgAPIKeyStore) ListByTenant(ctx context.Context, tenantID string) ([]model.APIKey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, key_prefix, name, status, last_used_at, created_at
		 FROM api_keys WHERE tenant_id = $1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []model.APIKey
	for rows.Next() {
		var k model.APIKey
		if err := rows.Scan(&k.ID, &k.TenantID, &k.KeyPrefix, &k.Name,
			&k.Status, &k.LastUsedAt, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api keys: %w", err)
	}
	return keys, nil
}

// Revoke 吊销 API Key。
func (s *PgAPIKeyStore) Revoke(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE api_keys SET status = 'revoked' WHERE id = $1 AND status = 'active'`, id)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("api key %d not found or already revoked", id)
	}
	return nil
}

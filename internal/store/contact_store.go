package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/omeyang/clarion/internal/model"
	"github.com/omeyang/clarion/internal/service"
)

// 编译时接口一致性检查。
var _ service.ContactRepo = (*PgContactStore)(nil)

// PgContactStore 提供基于 PostgreSQL 的联系人数据操作。
type PgContactStore struct {
	pool PoolQuerier
}

// NewPgContactStore 创建新的 PgContactStore。
func NewPgContactStore(pool PoolQuerier) *PgContactStore {
	return &PgContactStore{pool: pool}
}

// Create 插入新联系人并返回其 ID。
func (s *PgContactStore) Create(ctx context.Context, c *model.Contact) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO contacts (phone_masked, phone_hash, source, profile_json, current_status, do_not_call)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id`,
		c.PhoneMasked, c.PhoneHash, c.Source, c.ProfileJSON, c.CurrentStatus, c.DoNotCall,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create contact: %w", err)
	}
	return id, nil
}

// GetByID 根据 ID 查询联系人，未找到时返回 nil。
func (s *PgContactStore) GetByID(ctx context.Context, id int64) (*model.Contact, error) {
	c := &model.Contact{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, phone_masked, phone_hash, source, profile_json, current_status, do_not_call, created_at, updated_at
		 FROM contacts WHERE id = $1`, id,
	).Scan(&c.ID, &c.PhoneMasked, &c.PhoneHash, &c.Source, &c.ProfileJSON,
		&c.CurrentStatus, &c.DoNotCall, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get contact %d: %w", id, err)
	}
	return c, nil
}

// List 返回分页的联系人列表及总数。
func (s *PgContactStore) List(ctx context.Context, offset, limit int) ([]model.Contact, int, error) {
	var total int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM contacts`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count contacts: %w", err)
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, phone_masked, phone_hash, source, profile_json, current_status, do_not_call, created_at, updated_at
		 FROM contacts ORDER BY id LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list contacts: %w", err)
	}
	defer rows.Close()

	var contacts []model.Contact
	for rows.Next() {
		var c model.Contact
		if err := rows.Scan(&c.ID, &c.PhoneMasked, &c.PhoneHash, &c.Source, &c.ProfileJSON,
			&c.CurrentStatus, &c.DoNotCall, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan contact: %w", err)
		}
		contacts = append(contacts, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate contacts: %w", err)
	}

	return contacts, total, nil
}

// UpdateStatus 变更联系人当前状态。
func (s *PgContactStore) UpdateStatus(ctx context.Context, id int64, status string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE contacts SET current_status = $1, updated_at = $2 WHERE id = $3`,
		status, time.Now(), id)
	if err != nil {
		return fmt.Errorf("update contact status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("contact %d not found", id)
	}
	return nil
}

// BulkCreate 批量插入联系人，根据手机号哈希跳过重复项。
func (s *PgContactStore) BulkCreate(ctx context.Context, contacts []model.Contact) (inserted int, err error) {
	batch := &pgx.Batch{}
	for _, c := range contacts {
		batch.Queue(
			`INSERT INTO contacts (phone_masked, phone_hash, source, profile_json, current_status, do_not_call)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (phone_hash) DO NOTHING`,
			c.PhoneMasked, c.PhoneHash, c.Source, c.ProfileJSON, c.CurrentStatus, c.DoNotCall,
		)
	}

	br := s.pool.SendBatch(ctx, batch)
	defer func() {
		if closeErr := br.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()

	for range contacts {
		tag, execErr := br.Exec()
		if execErr != nil {
			return inserted, fmt.Errorf("bulk create contact: %w", execErr)
		}
		inserted += int(tag.RowsAffected())
	}

	return inserted, nil
}

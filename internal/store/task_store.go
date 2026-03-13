package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/omeyang/clarion/internal/engine"
	"github.com/omeyang/clarion/internal/model"
	"github.com/omeyang/clarion/internal/service"
)

// 编译时接口一致性检查。
var _ service.TaskRepo = (*PgTaskStore)(nil)

// PgTaskStore 提供基于 PostgreSQL 的任务数据操作。
type PgTaskStore struct {
	pool PoolQuerier
}

// NewPgTaskStore 创建新的 PgTaskStore。
func NewPgTaskStore(pool PoolQuerier) *PgTaskStore {
	return &PgTaskStore{pool: pool}
}

// Create 插入新的外呼任务并返回其 ID。
func (s *PgTaskStore) Create(ctx context.Context, t *model.CallTask) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO call_tasks (name, scenario_template_id, template_snapshot_id,
		 contact_filter, schedule_config, daily_limit, max_concurrent, status,
		 total_contacts, completed_contacts)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING id`,
		t.Name, t.ScenarioTemplateID, t.TemplateSnapshotID,
		t.ContactFilter, t.ScheduleConfig, t.DailyLimit, t.MaxConcurrent,
		t.Status, t.TotalContacts, t.CompletedContacts,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create task: %w", err)
	}
	return id, nil
}

// GetByID 根据 ID 查询外呼任务，未找到时返回 nil。
func (s *PgTaskStore) GetByID(ctx context.Context, id int64) (*model.CallTask, error) {
	t := &model.CallTask{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, scenario_template_id, template_snapshot_id,
		 contact_filter, schedule_config, daily_limit, max_concurrent,
		 status, total_contacts, completed_contacts, created_at, updated_at
		 FROM call_tasks WHERE id = $1`, id,
	).Scan(&t.ID, &t.Name, &t.ScenarioTemplateID, &t.TemplateSnapshotID,
		&t.ContactFilter, &t.ScheduleConfig, &t.DailyLimit, &t.MaxConcurrent,
		&t.Status, &t.TotalContacts, &t.CompletedContacts, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get task %d: %w", id, err)
	}
	return t, nil
}

// List 返回分页的外呼任务列表及总数。
func (s *PgTaskStore) List(ctx context.Context, offset, limit int) ([]model.CallTask, int, error) {
	var total int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM call_tasks`).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count tasks: %w", err)
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, name, scenario_template_id, template_snapshot_id,
		 contact_filter, schedule_config, daily_limit, max_concurrent,
		 status, total_contacts, completed_contacts, created_at, updated_at
		 FROM call_tasks ORDER BY id LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []model.CallTask
	for rows.Next() {
		var t model.CallTask
		if err := rows.Scan(&t.ID, &t.Name, &t.ScenarioTemplateID, &t.TemplateSnapshotID,
			&t.ContactFilter, &t.ScheduleConfig, &t.DailyLimit, &t.MaxConcurrent,
			&t.Status, &t.TotalContacts, &t.CompletedContacts, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate tasks: %w", err)
	}

	return tasks, total, nil
}

// Update 修改已有的外呼任务。
func (s *PgTaskStore) Update(ctx context.Context, t *model.CallTask) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE call_tasks SET name = $1, contact_filter = $2, schedule_config = $3,
		 daily_limit = $4, max_concurrent = $5, total_contacts = $6, updated_at = $7
		 WHERE id = $8`,
		t.Name, t.ContactFilter, t.ScheduleConfig,
		t.DailyLimit, t.MaxConcurrent, t.TotalContacts, time.Now(), t.ID)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %d not found", t.ID)
	}
	return nil
}

// UpdateStatus 变更任务状态。
func (s *PgTaskStore) UpdateStatus(ctx context.Context, id int64, status engine.TaskStatus) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE call_tasks SET status = $1, updated_at = $2 WHERE id = $3`,
		status, time.Now(), id)
	if err != nil {
		return fmt.Errorf("update task status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %d not found", id)
	}
	return nil
}

// IncrementCompleted 将任务的已完成联系人计数加一。
func (s *PgTaskStore) IncrementCompleted(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE call_tasks SET completed_contacts = completed_contacts + 1, updated_at = $1
		 WHERE id = $2`,
		time.Now(), id)
	if err != nil {
		return fmt.Errorf("increment completed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %d not found", id)
	}
	return nil
}

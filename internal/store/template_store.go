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
var _ service.TemplateRepo = (*PgTemplateStore)(nil)

// PgTemplateStore 提供基于 PostgreSQL 的模板数据操作。
type PgTemplateStore struct {
	pool PoolQuerier
}

// NewPgTemplateStore 创建新的 PgTemplateStore。
func NewPgTemplateStore(pool PoolQuerier) *PgTemplateStore {
	return &PgTemplateStore{pool: pool}
}

// Create 插入新的场景模板并返回其 ID。
func (s *PgTemplateStore) Create(ctx context.Context, t *model.ScenarioTemplate) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO scenario_templates (tenant_id, name, domain, opening_script, state_machine_config,
		 extraction_schema, grading_rules, prompt_templates, notification_config,
		 call_protection_config, precompiled_audios, status, version)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		 RETURNING id`,
		t.TenantID, t.Name, t.Domain, t.OpeningScript, t.StateMachineConfig,
		t.ExtractionSchema, t.GradingRules, t.PromptTemplates, t.NotificationConfig,
		t.CallProtectionConfig, t.PrecompiledAudios, t.Status, t.Version,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create template: %w", err)
	}
	return id, nil
}

// GetByID 根据 ID 查询场景模板，未找到时返回 nil。
func (s *PgTemplateStore) GetByID(ctx context.Context, id int64) (*model.ScenarioTemplate, error) {
	t := &model.ScenarioTemplate{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, domain, opening_script, state_machine_config,
		 extraction_schema, grading_rules, prompt_templates, notification_config,
		 call_protection_config, precompiled_audios, status, version, created_at, updated_at
		 FROM scenario_templates WHERE id = $1`, id,
	).Scan(&t.ID, &t.TenantID, &t.Name, &t.Domain, &t.OpeningScript, &t.StateMachineConfig,
		&t.ExtractionSchema, &t.GradingRules, &t.PromptTemplates, &t.NotificationConfig,
		&t.CallProtectionConfig, &t.PrecompiledAudios, &t.Status, &t.Version,
		&t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get template %d: %w", id, err)
	}
	return t, nil
}

// List 返回指定租户的分页场景模板列表及总数。
func (s *PgTemplateStore) List(ctx context.Context, tenantID string, offset, limit int) ([]model.ScenarioTemplate, int, error) {
	var total int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM scenario_templates WHERE tenant_id = $1`, tenantID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count templates: %w", err)
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, name, domain, opening_script, state_machine_config,
		 extraction_schema, grading_rules, prompt_templates, notification_config,
		 call_protection_config, precompiled_audios, status, version, created_at, updated_at
		 FROM scenario_templates WHERE tenant_id = $1 ORDER BY id LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list templates: %w", err)
	}
	defer rows.Close()

	var templates []model.ScenarioTemplate
	for rows.Next() {
		var t model.ScenarioTemplate
		if err := rows.Scan(&t.ID, &t.TenantID, &t.Name, &t.Domain, &t.OpeningScript, &t.StateMachineConfig,
			&t.ExtractionSchema, &t.GradingRules, &t.PromptTemplates, &t.NotificationConfig,
			&t.CallProtectionConfig, &t.PrecompiledAudios, &t.Status, &t.Version,
			&t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan template: %w", err)
		}
		templates = append(templates, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate templates: %w", err)
	}

	return templates, total, nil
}

// Update 修改已有的场景模板并递增版本号。
func (s *PgTemplateStore) Update(ctx context.Context, t *model.ScenarioTemplate) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE scenario_templates SET name = $1, domain = $2, opening_script = $3,
		 state_machine_config = $4, extraction_schema = $5, grading_rules = $6,
		 prompt_templates = $7, notification_config = $8, call_protection_config = $9,
		 precompiled_audios = $10, version = version + 1, updated_at = $11
		 WHERE id = $12`,
		t.Name, t.Domain, t.OpeningScript, t.StateMachineConfig,
		t.ExtractionSchema, t.GradingRules, t.PromptTemplates, t.NotificationConfig,
		t.CallProtectionConfig, t.PrecompiledAudios, time.Now(), t.ID)
	if err != nil {
		return fmt.Errorf("update template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("template %d not found", t.ID)
	}
	return nil
}

// UpdateStatus 变更模板状态。
func (s *PgTemplateStore) UpdateStatus(ctx context.Context, id int64, status engine.TemplateStatus) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE scenario_templates SET status = $1, updated_at = $2 WHERE id = $3`,
		status, time.Now(), id)
	if err != nil {
		return fmt.Errorf("update template status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("template %d not found", id)
	}
	return nil
}

// CreateSnapshot 插入新的模板快照并返回其 ID。
func (s *PgTemplateStore) CreateSnapshot(ctx context.Context, snap *model.TemplateSnapshot) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO template_snapshots (template_id, snapshot_data)
		 VALUES ($1, $2) RETURNING id`,
		snap.TemplateID, snap.SnapshotData,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create snapshot: %w", err)
	}
	return id, nil
}

// GetSnapshotByID 根据 ID 查询模板快照，未找到时返回 nil。
func (s *PgTemplateStore) GetSnapshotByID(ctx context.Context, id int64) (*model.TemplateSnapshot, error) {
	snap := &model.TemplateSnapshot{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, template_id, snapshot_data, created_at
		 FROM template_snapshots WHERE id = $1`, id,
	).Scan(&snap.ID, &snap.TemplateID, &snap.SnapshotData, &snap.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get snapshot %d: %w", id, err)
	}
	return snap, nil
}

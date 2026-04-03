package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/gofrs/uuid/v5"
	"github.com/urfave/cli/v3"

	"github.com/omeyang/clarion/internal/auth"
	"github.com/omeyang/clarion/internal/config"
	"github.com/omeyang/clarion/internal/model"
	"github.com/omeyang/clarion/internal/store"
)

func adminCommands() *cli.Command {
	return &cli.Command{
		Name:  "admin",
		Usage: "管理租户和 API Key",
		Commands: []*cli.Command{
			adminCreateTenant(),
			adminListTenants(),
			adminSetTenantStatus(),
			adminSetQuota(),
			adminCreateAPIKey(),
			adminRevokeAPIKey(),
		},
	}
}

// withDB 是管理命令的通用数据库连接辅助函数。
func withDB(cmd *cli.Command, fn func(ctx context.Context, pool store.PoolQuerier) error) error {
	cfgPath := cmd.Root().String("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx := context.Background()
	logger := newLogger(cfg.Server.LogLevel)

	db, err := store.NewDB(ctx, cfg.Database, logger)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer db.Close()

	return fn(ctx, db.Pool)
}

func adminCreateTenant() *cli.Command {
	return &cli.Command{
		Name:  "create-tenant",
		Usage: "创建新租户",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "slug", Required: true, Usage: "人类可读标识（如 xian-fangchan）"},
			&cli.StringFlag{Name: "name", Required: true, Usage: "显示名称"},
			&cli.StringFlag{Name: "contact", Value: "", Usage: "联系人姓名"},
			&cli.StringFlag{Name: "phone", Value: "", Usage: "联系人手机号"},
			&cli.IntFlag{Name: "daily-limit", Value: 100, Usage: "每日外呼上限"},
			&cli.IntFlag{Name: "max-concurrent", Value: 3, Usage: "最大并发通话数"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return withDB(cmd, func(ctx context.Context, pool store.PoolQuerier) error {
				tenantID := uuid.Must(uuid.NewV7()).String()
				tenantStore := store.NewPgTenantStore(pool)

				t := &model.Tenant{
					ID:             tenantID,
					Slug:           cmd.String("slug"),
					Name:           cmd.String("name"),
					ContactPerson:  cmd.String("contact"),
					ContactPhone:   cmd.String("phone"),
					Status:         "active",
					DailyCallLimit: int(cmd.Int("daily-limit")),
					MaxConcurrent:  int(cmd.Int("max-concurrent")),
					Settings:       json.RawMessage("{}"),
				}

				if err := tenantStore.Create(ctx, t); err != nil {
					return fmt.Errorf("创建租户: %w", err)
				}

				fmt.Fprintf(os.Stdout, "租户已创建：id=%s slug=%s\n", t.ID, t.Slug)
				return nil
			})
		},
	}
}

func adminListTenants() *cli.Command {
	return &cli.Command{
		Name:  "list-tenants",
		Usage: "查看所有租户",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return withDB(cmd, func(ctx context.Context, pool store.PoolQuerier) error {
				tenants, err := store.NewPgTenantStore(pool).List(ctx)
				if err != nil {
					return err
				}

				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				fmt.Fprintln(w, "ID\tSLUG\tNAME\tSTATUS\tDAILY\tCONCURRENT")
				for _, t := range tenants {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%d\n",
						t.ID, t.Slug, t.Name, t.Status, t.DailyCallLimit, t.MaxConcurrent)
				}
				return w.Flush()
			})
		},
	}
}

func adminSetTenantStatus() *cli.Command {
	return &cli.Command{
		Name:  "set-tenant-status",
		Usage: "暂停或恢复租户",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "tenant", Required: true, Usage: "租户 slug 或 ID"},
			&cli.StringFlag{Name: "status", Required: true, Usage: "active 或 suspended"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return withDB(cmd, func(ctx context.Context, pool store.PoolQuerier) error {
				ts := store.NewPgTenantStore(pool)
				tenantID, err := resolveTenantID(ctx, ts, cmd.String("tenant"))
				if err != nil {
					return err
				}

				status := cmd.String("status")
				if status != "active" && status != "suspended" {
					return fmt.Errorf("status 必须为 active 或 suspended，got: %s", status)
				}

				if err := ts.UpdateStatus(ctx, tenantID, status); err != nil {
					return err
				}
				fmt.Fprintf(os.Stdout, "租户 %s 状态已更新为 %s\n", tenantID, status)
				return nil
			})
		},
	}
}

func adminSetQuota() *cli.Command {
	return &cli.Command{
		Name:  "set-quota",
		Usage: "调整租户配额",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "tenant", Required: true, Usage: "租户 slug 或 ID"},
			&cli.IntFlag{Name: "daily-limit", Value: -1, Usage: "每日外呼上限"},
			&cli.IntFlag{Name: "max-concurrent", Value: -1, Usage: "最大并发通话数"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return withDB(cmd, func(ctx context.Context, pool store.PoolQuerier) error {
				ts := store.NewPgTenantStore(pool)
				tenantID, err := resolveTenantID(ctx, ts, cmd.String("tenant"))
				if err != nil {
					return err
				}

				// 查询当前值，按需覆盖。
				tenant, err := ts.GetFull(ctx, tenantID)
				if err != nil {
					return fmt.Errorf("查询租户: %w", err)
				}
				if tenant == nil {
					return fmt.Errorf("租户 %s 不存在", tenantID)
				}

				daily := tenant.DailyCallLimit
				concurrent := tenant.MaxConcurrent
				if v := cmd.Int("daily-limit"); v >= 0 {
					daily = int(v)
				}
				if v := cmd.Int("max-concurrent"); v >= 0 {
					concurrent = int(v)
				}

				if err := ts.UpdateQuota(ctx, tenantID, daily, concurrent); err != nil {
					return err
				}
				fmt.Fprintf(os.Stdout, "租户 %s 配额已更新：daily=%d concurrent=%d\n", tenantID, daily, concurrent)
				return nil
			})
		},
	}
}

func adminCreateAPIKey() *cli.Command {
	return &cli.Command{
		Name:  "create-apikey",
		Usage: "创建 API Key",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "tenant", Required: true, Usage: "租户 slug 或 ID"},
			&cli.StringFlag{Name: "name", Value: "", Usage: "Key 描述（如 '生产环境'）"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return withDB(cmd, func(ctx context.Context, pool store.PoolQuerier) error {
				ts := store.NewPgTenantStore(pool)
				tenantID, err := resolveTenantID(ctx, ts, cmd.String("tenant"))
				if err != nil {
					return err
				}

				fullKey, displayPrefix, err := auth.GenerateAPIKey(auth.KeyPrefixLive)
				if err != nil {
					return fmt.Errorf("生成 API Key: %w", err)
				}

				keyHash := auth.HashAPIKey(fullKey)
				key := &model.APIKey{
					TenantID:  tenantID,
					KeyPrefix: displayPrefix,
					KeyHash:   keyHash,
					Name:      cmd.String("name"),
					Status:    "active",
				}

				id, err := store.NewPgAPIKeyStore(pool).Create(ctx, key)
				if err != nil {
					return fmt.Errorf("创建 API Key: %w", err)
				}

				fmt.Fprintf(os.Stdout, "API Key 已创建（id=%d）：\n", id)
				fmt.Fprintf(os.Stdout, "  %s\n", fullKey)
				fmt.Fprintln(os.Stdout, "  请妥善保存，此密钥无法再次查看。")
				return nil
			})
		},
	}
}

func adminRevokeAPIKey() *cli.Command {
	return &cli.Command{
		Name:  "revoke-apikey",
		Usage: "吊销 API Key",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "id", Required: true, Usage: "API Key ID"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return withDB(cmd, func(ctx context.Context, pool store.PoolQuerier) error {
				if err := store.NewPgAPIKeyStore(pool).Revoke(ctx, int64(cmd.Int("id"))); err != nil {
					return err
				}
				fmt.Fprintf(os.Stdout, "API Key %d 已吊销\n", int64(cmd.Int("id")))
				return nil
			})
		},
	}
}

// resolveTenantID 将 slug 或 UUID 解析为 tenant ID。
func resolveTenantID(ctx context.Context, ts *store.PgTenantStore, input string) (string, error) {
	// 先尝试当作 UUID。
	if _, err := uuid.FromString(input); err == nil {
		return input, nil
	}
	// 再尝试当作 slug。
	t, err := ts.GetBySlug(ctx, input)
	if err != nil {
		return "", fmt.Errorf("查询租户 %s: %w", input, err)
	}
	if t == nil {
		return "", fmt.Errorf("租户 %s 不存在", input)
	}
	return t.ID, nil
}

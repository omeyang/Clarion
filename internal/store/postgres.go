// Package store 提供 PostgreSQL、Redis、OSS 的数据访问封装。
package store

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/omeyang/clarion/internal/config"
)

// PoolQuerier 抽象 pgxpool.Pool 的查询接口，便于测试时替换。
type PoolQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

// DB 封装 pgxpool.Pool，提供 PostgreSQL 连接池访问。
type DB struct {
	Pool *pgxpool.Pool
}

// NewDB 创建 PostgreSQL 连接池。
func NewDB(ctx context.Context, cfg config.Database, logger *slog.Logger) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse database DSN: %w", err)
	}

	poolCfg.MaxConns = int32(cfg.MaxOpenConns)
	poolCfg.MinConns = int32(cfg.MaxIdleConns)

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	logger.Info("database connected",
		slog.String("host", poolCfg.ConnConfig.Host),
		slog.String("database", poolCfg.ConnConfig.Database),
		slog.Int("max_conns", int(poolCfg.MaxConns)),
	)

	return &DB{Pool: pool}, nil
}

// Close 关闭数据库连接池。
func (db *DB) Close() {
	db.Pool.Close()
}

// Ping 验证数据库连接是否存活。
func (db *DB) Ping(ctx context.Context) error {
	if err := db.Pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

package store

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"

	"github.com/omeyang/clarion/internal/config"
)

// RDS 封装 go-redis 客户端。
type RDS struct {
	Client *redis.Client
}

// NewRDS 创建新的 Redis 客户端并验证连通性。
func NewRDS(ctx context.Context, cfg config.Redis, logger *slog.Logger) (*RDS, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	logger.Info("redis connected", slog.String("addr", cfg.Addr), slog.Int("db", cfg.DB))

	return &RDS{Client: client}, nil
}

// Close 关闭 Redis 客户端连接。
func (r *RDS) Close() error {
	if err := r.Client.Close(); err != nil {
		return fmt.Errorf("close redis: %w", err)
	}
	return nil
}

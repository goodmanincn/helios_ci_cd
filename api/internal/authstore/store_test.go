package authstore_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/helios-cicd/helios/api/internal/authstore"
)

func openRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("HELIOS_REDIS_ADDR")
	if addr == "" {
		t.Skip("HELIOS_REDIS_ADDR 未设置,跳过 redis 集成测试")
	}
	c := redis.NewClient(&redis.Options{Addr: addr, DB: 15}) // 用 db15 隔离
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, c.Ping(ctx).Err())
	t.Cleanup(func() { _ = c.FlushDB(context.Background()); _ = c.Close() })
	return c
}

func TestAccessBlacklist(t *testing.T) {
	rdb := openRedis(t)
	s := authstore.New(rdb)
	ctx := context.Background()

	yes, err := s.IsAccessBlacklisted(ctx, "abc")
	require.NoError(t, err)
	require.False(t, yes)

	require.NoError(t, s.BlacklistAccess(ctx, "abc", time.Now().Add(time.Second)))
	yes, err = s.IsAccessBlacklisted(ctx, "abc")
	require.NoError(t, err)
	require.True(t, yes)

	// 过期 token 不该写入
	require.NoError(t, s.BlacklistAccess(ctx, "stale", time.Now().Add(-time.Second)))
	yes, _ = s.IsAccessBlacklisted(ctx, "stale")
	require.False(t, yes)
}

func TestRefreshLifecycle(t *testing.T) {
	rdb := openRedis(t)
	s := authstore.New(rdb)
	ctx := context.Background()

	err := s.RegisterRefresh(ctx, "jti1", 42, time.Now().Add(time.Minute))
	require.NoError(t, err)

	uid, ok, err := s.ConsumeRefresh(ctx, "jti1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(42), uid)

	// 第二次消费应该失败 (rotate)
	_, ok, err = s.ConsumeRefresh(ctx, "jti1")
	require.NoError(t, err)
	require.False(t, ok)

	// revoke 路径
	require.NoError(t, s.RegisterRefresh(ctx, "jti2", 7, time.Now().Add(time.Minute)))
	require.NoError(t, s.RevokeRefresh(ctx, "jti2"))
	_, ok, _ = s.ConsumeRefresh(ctx, "jti2")
	require.False(t, ok)
}

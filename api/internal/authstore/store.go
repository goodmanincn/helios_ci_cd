// Package authstore 用 Redis 维护 token 黑名单 (jti) 与 refresh token 关联。
//
// 设计:
//   - access token 撤销:把 jti 写入黑名单,TTL = token 剩余有效期。中间件每次解 token 后查 Redis。
//   - refresh token 旋转:每个 refresh 的 jti 存活态 (value=user_id) 在 Redis,TTL=RefreshTTL。
//     登出 / 用旧 refresh 换新时,旧 jti 立即删除。被刷新过的 refresh 拒绝复用。
package authstore

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	prefixAccessBlacklist  = "helios:auth:bl:access:" // + jti
	prefixRefreshAlive     = "helios:auth:refresh:"   // + jti
)

type Store struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Store { return &Store{rdb: rdb} }

// BlacklistAccess 把 access token jti 拉黑直到 expiresAt。
func (s *Store) BlacklistAccess(ctx context.Context, jti string, expiresAt time.Time) error {
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return nil // 已过期,无需拉黑
	}
	return s.rdb.Set(ctx, prefixAccessBlacklist+jti, "1", ttl).Err()
}

// IsAccessBlacklisted 查 jti 是否在黑名单中。
func (s *Store) IsAccessBlacklisted(ctx context.Context, jti string) (bool, error) {
	n, err := s.rdb.Exists(ctx, prefixAccessBlacklist+jti).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// RegisterRefresh 登记一个 refresh token 为 "活的"。
func (s *Store) RegisterRefresh(ctx context.Context, jti string, userID int64, expiresAt time.Time) error {
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return fmt.Errorf("authstore: refresh already expired")
	}
	return s.rdb.Set(ctx, prefixRefreshAlive+jti, userID, ttl).Err()
}

// ConsumeRefresh 验证 refresh 仍 "活" 并删除 (rotate)。返回 (userID, ok)。
// 用 Lua 保证 GET+DEL 原子,避免双花。
func (s *Store) ConsumeRefresh(ctx context.Context, jti string) (int64, bool, error) {
	const lua = `
local v = redis.call('GET', KEYS[1])
if v then
  redis.call('DEL', KEYS[1])
  return v
end
return nil
`
	res, err := s.rdb.Eval(ctx, lua, []string{prefixRefreshAlive + jti}).Result()
	if err != nil {
		if err == redis.Nil {
			return 0, false, nil
		}
		return 0, false, err
	}
	if res == nil {
		return 0, false, nil
	}
	uidStr, _ := res.(string)
	var uid int64
	if _, err := fmt.Sscanf(uidStr, "%d", &uid); err != nil {
		return 0, false, fmt.Errorf("authstore: corrupt refresh value: %w", err)
	}
	return uid, true, nil
}

// RevokeRefresh 撤销一个 refresh (logout 用)。
func (s *Store) RevokeRefresh(ctx context.Context, jti string) error {
	return s.rdb.Del(ctx, prefixRefreshAlive+jti).Err()
}

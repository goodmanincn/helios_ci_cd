package service_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/helios-cicd/helios/api/internal/db"
	"github.com/helios-cicd/helios/api/internal/service"
)

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("HELIOS_DB_DSN")
	if dsn == "" {
		t.Skip("HELIOS_DB_DSN 未设置,跳过 service 集成测试")
	}
	require.NoError(t, db.Migrate(dsn))
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	return gdb
}

func uniq() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func TestUserService_CreateAndVerify(t *testing.T) {
	gdb := openDB(t)
	tx := gdb.Begin()
	t.Cleanup(func() { tx.Rollback() })
	svc := service.NewUserService(tx)
	ctx := context.Background()

	uname := "u_" + uniq()
	email := uname + "@example.com"

	// 1. 创建
	u, err := svc.Create(ctx, service.CreateUserInput{
		Username: uname, Email: email, Password: "supersecret", DisplayName: "U",
	})
	require.NoError(t, err)
	require.NotZero(t, u.ID)
	require.NotEqual(t, "supersecret", u.PasswordHash, "应该是 bcrypt hash")
	require.True(t, len(u.PasswordHash) > 50)

	// 2. 弱密码拒绝
	_, err = svc.Create(ctx, service.CreateUserInput{
		Username: "x_" + uniq(), Email: "x@example.com", Password: "short",
	})
	require.ErrorIs(t, err, service.ErrPasswordTooShort)

	// 3. 重复 username
	_, err = svc.Create(ctx, service.CreateUserInput{
		Username: uname, Email: "other@example.com", Password: "supersecret",
	})
	require.ErrorIs(t, err, service.ErrUsernameTaken)

	// 4. 重复 email (大小写不敏感)
	_, err = svc.Create(ctx, service.CreateUserInput{
		Username: "other_" + uniq(), Email: strings.ToUpper(email), Password: "supersecret",
	})
	require.ErrorIs(t, err, service.ErrEmailTaken)

	// 5. 密码校验
	got, err := svc.VerifyPassword(ctx, uname, "supersecret")
	require.NoError(t, err)
	require.Equal(t, u.ID, got.ID)

	// 6. 错密码
	_, err = svc.VerifyPassword(ctx, uname, "wrong-password")
	require.ErrorIs(t, err, service.ErrInvalidCredentials)

	// 7. 不存在用户 — 也是 ErrInvalidCredentials (防止信息泄漏)
	_, err = svc.VerifyPassword(ctx, "ghost_"+uniq(), "anything")
	require.ErrorIs(t, err, service.ErrInvalidCredentials)
}

func TestUserService_InactiveUser(t *testing.T) {
	gdb := openDB(t)
	tx := gdb.Begin()
	t.Cleanup(func() { tx.Rollback() })
	svc := service.NewUserService(tx)
	ctx := context.Background()

	uname := "inact_" + uniq()
	u, err := svc.Create(ctx, service.CreateUserInput{
		Username: uname, Email: uname + "@x.com", Password: "supersecret",
	})
	require.NoError(t, err)

	// 禁用
	require.NoError(t, tx.Model(u).Update("is_active", false).Error)

	_, err = svc.VerifyPassword(ctx, uname, "supersecret")
	require.ErrorIs(t, err, service.ErrUserInactive)
}

func TestUserService_GetByID(t *testing.T) {
	gdb := openDB(t)
	tx := gdb.Begin()
	t.Cleanup(func() { tx.Rollback() })
	svc := service.NewUserService(tx)
	ctx := context.Background()

	_, err := svc.GetByID(ctx, 9999999)
	require.ErrorIs(t, err, service.ErrUserNotFound)

	u, err := svc.Create(ctx, service.CreateUserInput{
		Username: "g_" + uniq(), Email: uniq() + "@x.com", Password: "supersecret",
	})
	require.NoError(t, err)
	got, err := svc.GetByID(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, u.Username, got.Username)
}

// 保底:外部 test helper 用到的 imports
var _ = fmt.Sprintf

// Package model 单元测试 — 使用真实 dev PG (需 docker-compose 起着)
package model_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/helios-cicd/helios/api/internal/db"
	"github.com/helios-cicd/helios/api/internal/model"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("HELIOS_TEST_DB_DSN")
	if dsn == "" {
		dsn = os.Getenv("HELIOS_DB_DSN")
	}
	if dsn == "" {
		t.Skip("HELIOS_DB_DSN / HELIOS_TEST_DB_DSN 未设置,跳过 model 集成测试")
	}
	require.NoError(t, db.Migrate(dsn), "确保 schema 是最新的")

	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	return gdb
}

// 在事务里跑测试,t.Cleanup 自动回滚,互不污染
func withTx(t *testing.T, fn func(tx *gorm.DB)) {
	t.Helper()
	gdb := openTestDB(t)
	tx := gdb.Begin()
	t.Cleanup(func() { tx.Rollback() })
	fn(tx)
}

func TestUserCRUD(t *testing.T) {
	withTx(t, func(tx *gorm.DB) {
		u := model.User{
			Username:     "alice_" + time.Now().Format("150405.000"),
			Email:        "alice@example.com",
			PasswordHash: "fake-hash",
			DisplayName:  "Alice",
			IsActive:     true,
		}
		require.NoError(t, tx.Create(&u).Error)
		require.NotZero(t, u.ID)
		require.False(t, u.CreatedAt.IsZero())

		var got model.User
		require.NoError(t, tx.First(&got, u.ID).Error)
		require.Equal(t, u.Username, got.Username)

		// 更新
		require.NoError(t, tx.Model(&got).Update("display_name", "Alice Wonderland").Error)

		// 软删除
		require.NoError(t, tx.Delete(&got).Error)
		var afterSoft model.User
		err := tx.First(&afterSoft, u.ID).Error
		require.ErrorIs(t, err, gorm.ErrRecordNotFound, "软删除后默认查不到")

		require.NoError(t, tx.Unscoped().First(&afterSoft, u.ID).Error, "Unscoped 能查到")
		require.True(t, afterSoft.DeletedAt.Valid)
	})
}

func TestOrgAndProject(t *testing.T) {
	withTx(t, func(tx *gorm.DB) {
		u := model.User{Username: "orgowner_" + time.Now().Format("150405.000"), Email: "o@example.com"}
		require.NoError(t, tx.Create(&u).Error)

		org := model.Organization{Name: "Acme", Slug: "acme-" + time.Now().Format("150405.000"), OwnerID: &u.ID}
		require.NoError(t, tx.Create(&org).Error)

		mem := model.OrgMember{OrgID: org.ID, UserID: u.ID, Role: "owner"}
		require.NoError(t, tx.Create(&mem).Error)

		proj := model.Project{
			OrgID: org.ID, Name: "api-gateway", Slug: "api-gateway",
			RepoURL: "https://github.com/acme/api.git", RepoType: "github",
		}
		require.NoError(t, tx.Create(&proj).Error)
		require.NotZero(t, proj.ID)

		// 重复 slug 在同 org 应该冲突 (复合唯一)
		dup := model.Project{
			OrgID: org.ID, Name: "x", Slug: "api-gateway",
			RepoURL: "https://example.com/x.git", RepoType: "github",
		}
		require.Error(t, tx.Create(&dup).Error, "复合唯一约束应阻止重复")
	})
}

func TestPipelineVersionAppend(t *testing.T) {
	withTx(t, func(tx *gorm.DB) {
		u := model.User{Username: "pl_" + time.Now().Format("150405.000"), Email: "pl@example.com"}
		require.NoError(t, tx.Create(&u).Error)
		org := model.Organization{Name: "X", Slug: "x-" + time.Now().Format("150405.000"), OwnerID: &u.ID}
		require.NoError(t, tx.Create(&org).Error)
		proj := model.Project{
			OrgID: org.ID, Name: "p", Slug: "p-" + time.Now().Format("150405.000"),
			RepoURL: "https://x.com/p.git", RepoType: "github",
		}
		require.NoError(t, tx.Create(&proj).Error)
		pipe := model.Pipeline{ProjectID: proj.ID, Name: "build"}
		require.NoError(t, tx.Create(&pipe).Error)

		v1 := model.PipelineVersion{
			PipelineID: pipe.ID, Version: 1,
			Spec:    []byte(`{"stages":[{"id":"build"}]}`),
			SpecRaw: "stages:\n  - id: build\n",
			Message: "init",
		}
		require.NoError(t, tx.Create(&v1).Error)

		// 同 (pipeline_id, version) 应该唯一
		v1dup := v1
		v1dup.ID = 0
		require.Error(t, tx.Create(&v1dup).Error)
	})
}

// Package db 提供数据库连接与迁移
package db

import (
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // 注册 postgres driver
	_ "github.com/jackc/pgx/v5/stdlib"                         // 注册 pgx driver
)

// 编译期把 migrations 目录嵌入二进制
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate 应用所有 pending up 迁移。
// dsn: postgres://user:***@host:port/db?sslmode=disable
func Migrate(dsn string) error {
	if dsn == "" {
		return errors.New("db.Migrate: empty DSN")
	}
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("db.Migrate: build embed source: %w", err)
	}
	defer src.Close()

	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		return fmt.Errorf("db.Migrate: open: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("db.Migrate: up: %w", err)
	}
	return nil
}

// Version 返回当前 schema 版本号与是否 dirty。0,false,nil 表示空库。
func Version(dsn string) (uint, bool, error) {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return 0, false, err
	}
	defer src.Close()
	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		return 0, false, err
	}
	defer m.Close()
	v, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	return v, dirty, err
}

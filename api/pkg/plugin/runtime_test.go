package plugin

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// runtime 测试走真实 PG, 需要 HELIOS_TEST_DB_DSN. 没设跳过.
//
// 不在 tx 里跑 — SQLResolver 用裸 *sql.DB, 看不见 gorm tx. 用 unique slug + 末尾清理.
func openRuntimeDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("HELIOS_TEST_DB_DSN")
	if dsn == "" {
		dsn = os.Getenv("HELIOS_DB_DSN")
	}
	if dsn == "" {
		t.Skip("HELIOS_TEST_DB_DSN not set, skip resolver integration")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestResolver_OfficialImplicitUse(t *testing.T) {
	db := openRuntimeDB(t)
	defer db.Close()

	ns := "resolver-off-" + randSuffix4()
	_, err := db.Exec(`INSERT INTO plugins (namespace, name, description, verified, official, latest_version)
		VALUES ($1, 'echo', 'test', TRUE, TRUE, 'v1')`, ns)
	if err != nil {
		t.Fatal(err)
	}
	var pluginID int64
	if err := db.QueryRow(`SELECT id FROM plugins WHERE namespace=$1 AND name='echo'`, ns).Scan(&pluginID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM plugin_installations WHERE plugin_id = $1`, pluginID)
		_, _ = db.Exec(`DELETE FROM plugin_versions WHERE plugin_id = $1`, pluginID)
		_, _ = db.Exec(`DELETE FROM plugins WHERE id = $1`, pluginID)
	})

	const yml = `name: echo
runs:
  using: container
  image: alpine:3
`
	_, err = db.Exec(`INSERT INTO plugin_versions (plugin_id, version, action_yml, action_spec, is_latest)
		VALUES ($1, 'v1', $2, '{}'::jsonb, TRUE)`, pluginID, yml)
	if err != nil {
		t.Fatal(err)
	}

	r := NewSQLResolver(db)
	ref, err := ParseRef(ns + "/echo@v1")
	if err != nil {
		t.Fatal(err)
	}
	// orgID=0: 走"官方 verified 隐式可用"路径
	resolved, err := r.Resolve(context.Background(), 0, ref)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Action.Runs.Image != "alpine:3" {
		t.Errorf("image=%s", resolved.Action.Runs.Image)
	}
	if resolved.Version != "v1" {
		t.Errorf("version=%s", resolved.Version)
	}
}

func TestResolver_NonOfficial_RequiresInstall(t *testing.T) {
	db := openRuntimeDB(t)
	defer db.Close()

	ns := "resolver-acme-" + randSuffix4()
	_, err := db.Exec(`INSERT INTO plugins (namespace, name, verified, official, latest_version)
		VALUES ($1, 'foo', FALSE, FALSE, 'v1')`, ns)
	if err != nil {
		t.Fatal(err)
	}
	var pluginID int64
	if err := db.QueryRow(`SELECT id FROM plugins WHERE namespace=$1`, ns).Scan(&pluginID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM plugin_installations WHERE plugin_id = $1`, pluginID)
		_, _ = db.Exec(`DELETE FROM plugin_versions WHERE plugin_id = $1`, pluginID)
		_, _ = db.Exec(`DELETE FROM plugins WHERE id = $1`, pluginID)
	})
	const yml = `name: foo
runs:
  using: container
  image: nginx
`
	_, err = db.Exec(`INSERT INTO plugin_versions (plugin_id, version, action_yml, action_spec, is_latest)
		VALUES ($1, 'v1', $2, '{}'::jsonb, TRUE)`, pluginID, yml)
	if err != nil {
		t.Fatal(err)
	}

	r := NewSQLResolver(db)
	ref, _ := ParseRef(ns + "/foo@v1")
	_, err = r.Resolve(context.Background(), 0, ref)
	if err == nil {
		t.Fatal("expected ErrNotInstalled")
	}
	if _, ok := err.(ErrNotInstalled); !ok {
		t.Errorf("err type %T: %v", err, err)
	}
}

// 简易 unique suffix 生成器 (不引 hex/rand 包).
func randSuffix4() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	now := os.Getpid() // 进程级足够 + 测试内 ns 相同就 race 上 unique violation
	b := make([]byte, 6)
	for i := range b {
		b[i] = charset[(now+i*31)%len(charset)]
	}
	return string(b)
}

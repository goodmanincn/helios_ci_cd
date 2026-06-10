package gitrunner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// makeLocalRepo 在 tmp 下做一个 bare git 仓库 + 一次 commit, 返回路径。
func makeLocalRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed, skipping")
	}
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRun(t, srcDir, "git", "init", "-b", "main")
	mustRun(t, srcDir, "git", "config", "user.email", "test@example.com")
	mustRun(t, srcDir, "git", "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(srcDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, srcDir, "git", "add", "README.md")
	mustRun(t, srcDir, "git", "commit", "-m", "init")
	// 创建 bare 镜像方便被 clone (避免 working tree clone 警告)
	bareDir := filepath.Join(root, "bare.git")
	mustRun(t, root, "git", "clone", "--bare", srcDir, bareDir)
	return bareDir
}

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, string(out))
	}
}

func TestShellCloner_Clone(t *testing.T) {
	repoURL := makeLocalRepo(t)
	dest := filepath.Join(t.TempDir(), "ws", "src")

	c := NewShell()
	if err := c.Clone(context.Background(), repoURL, "main", "", dest); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Fatalf("README.md missing after clone: %v", err)
	}
}

func TestShellCloner_DestExists(t *testing.T) {
	repoURL := makeLocalRepo(t)
	dest := t.TempDir() // 已存在
	c := NewShell()
	err := c.Clone(context.Background(), repoURL, "main", "", dest)
	if err == nil {
		t.Fatal("expect error when dest exists")
	}
}

func TestShellCloner_BadBranch(t *testing.T) {
	repoURL := makeLocalRepo(t)
	dest := filepath.Join(t.TempDir(), "ws")
	c := NewShell()
	err := c.Clone(context.Background(), repoURL, "non-existent-branch", "", dest)
	if err == nil {
		t.Fatal("expect error on non-existent branch")
	}
}

package dockerrun

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// 集成测试: 需要本地 dockerd, 没有 docker 时整体 skip。
//
// 跑法: 默认就跑 (docker socket 存在);CI 上可以用 DOCKER_HOST 注入。
//
// 注: 不直接 docker.Ping 然后再判断 skip 是为了让用户在没 docker 时也能跑 `go test`。
func requireDocker(t *testing.T) *Client {
	t.Helper()
	// 优先检查 socket / DOCKER_HOST 任一存在
	if os.Getenv("DOCKER_HOST") == "" {
		if _, err := os.Stat("/var/run/docker.sock"); err != nil {
			t.Skip("docker socket not present and DOCKER_HOST not set, skip")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := New(ctx, ClientConfig{NegotiateOnce: true, RequestTO: 5 * time.Second})
	if err != nil {
		t.Skipf("docker unreachable, skip: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestClient_PingAndList(t *testing.T) {
	c := requireDocker(t)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	imgs, err := c.ListImages(context.Background())
	if err != nil {
		t.Fatalf("list images: %v", err)
	}
	t.Logf("found %d images, sample: %s", len(imgs), strings.Join(imgs[:min(3, len(imgs))], ","))
}

func TestClient_New_BadHost(t *testing.T) {
	// 用一个肯定连不上的 tcp 地址 + NegotiateOnce=true → 应立即报错
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := New(ctx, ClientConfig{
		Host:          "tcp://127.0.0.1:1", // port 1, almost certainly closed
		NegotiateOnce: true,
		RequestTO:     1 * time.Second,
	})
	if err == nil {
		t.Fatal("expected error for unreachable host, got nil")
	}
	if !strings.Contains(err.Error(), "ping") && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected ping/timeout error, got %v", err)
	}
}

func TestClient_Close_Nil(t *testing.T) {
	var c *Client
	if err := c.Close(); err != nil {
		t.Errorf("nil close should be no-op, got %v", err)
	}
}

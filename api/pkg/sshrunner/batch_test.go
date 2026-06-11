// batch_test.go — 多主机执行框架测试。
package sshrunner

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestBatchExecutor_Serial(t *testing.T) {
	hosts := []string{"a", "b", "c"}
	var order []string
	be := NewBatchExecutor(BatchOpts{Strategy: StrategySerial})
	res := be.Run(context.Background(), hosts, func(ctx context.Context, h string) error {
		order = append(order, h)
		return nil
	})
	if len(res) != 3 {
		t.Fatalf("expected 3 results, got %d", len(res))
	}
	if order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Fatalf("serial order wrong: %v", order)
	}
}

func TestBatchExecutor_Parallel(t *testing.T) {
	hosts := []string{"a", "b", "c"}
	be := NewBatchExecutor(BatchOpts{Strategy: StrategyParallel, MaxConcurrency: 3})
	res := be.Run(context.Background(), hosts, func(ctx context.Context, h string) error {
		return nil
	})
	if len(res) != 3 {
		t.Fatalf("expected 3 results, got %d", len(res))
	}
	for _, r := range res {
		if r.Error != nil {
			t.Fatalf("unexpected error on %s: %v", r.Host, r.Error)
		}
	}
}

func TestBatchExecutor_Rolling(t *testing.T) {
	hosts := []string{"a", "b", "c", "d"}
	be := NewBatchExecutor(BatchOpts{Strategy: StrategyRolling, BatchSize: 2})
	res := be.Run(context.Background(), hosts, func(ctx context.Context, h string) error {
		return nil
	})
	if len(res) != 4 {
		t.Fatalf("expected 4 results, got %d", len(res))
	}
}

func TestBatchExecutor_MaxFailures(t *testing.T) {
	hosts := []string{"a", "b", "c", "d"}
	be := NewBatchExecutor(BatchOpts{Strategy: StrategySerial, MaxFailures: 2})
	calls := 0
	res := be.Run(context.Background(), hosts, func(ctx context.Context, h string) error {
		calls++
		return fmt.Errorf("fail")
	})
	// 第 1 台失败后 failures=1 < max=2 继续; 第 2 台失败后 failures=2 >= max=2 停止
	// 所以 a,b 有真实错误, c,d 被标记为 stopped
	if calls != 2 {
		t.Fatalf("expected 2 calls before stop, got %d", calls)
	}
	if res[2].Error == nil || res[3].Error == nil {
		t.Fatal("expected c,d to be stopped")
	}
}

func TestBatchExecutor_RollingInterval(t *testing.T) {
	hosts := []string{"a", "b", "c", "d"}
	be := NewBatchExecutor(BatchOpts{Strategy: StrategyRolling, BatchSize: 2, Interval: 50 * time.Millisecond})
	start := time.Now()
	be.Run(context.Background(), hosts, func(ctx context.Context, h string) error {
		return nil
	})
	elapsed := time.Since(start)
	// 2 批, 间隔 50ms, 总耗时至少 50ms
	if elapsed < 40*time.Millisecond {
		t.Fatalf("expected interval delay, got %v", elapsed)
	}
}

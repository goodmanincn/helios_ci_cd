// batch.go — 多主机并发/滚动执行框架 (E6.4)。
package sshrunner

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Strategy 部署策略。
type Strategy string

const (
	StrategySerial  Strategy = "serial"  // 一台接一台
	StrategyParallel Strategy = "parallel" // 全部并发
	StrategyRolling  Strategy = "rolling"  // 分批滚动
)

// BatchOpts 分批执行选项。
type BatchOpts struct {
	Strategy     Strategy
	BatchSize    int           // rolling 时每批主机数 (默认 1)
	Interval     time.Duration // rolling 时批间间隔 (默认 0)
	MaxFailures  int           // 允许的最大失败数; 超过则停止 (默认 0 = 任意失败即停)
	MaxConcurrency int         // 全局最大并发数 (默认 5); parallel 时生效
}

// HostTask 单主机任务函数。
type HostTask func(ctx context.Context, host string) error

// HostResult 单主机结果。
type HostResult struct {
	Host    string
	Error   error
	Elapsed time.Duration
}

// BatchExecutor 多主机执行器。
type BatchExecutor struct {
	opts BatchOpts
}

// NewBatchExecutor 构造执行器。
func NewBatchExecutor(opts BatchOpts) *BatchExecutor {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 1
	}
	if opts.MaxConcurrency <= 0 {
		opts.MaxConcurrency = 5
	}
	return &BatchExecutor{opts: opts}
}

// Run 执行主机列表任务,返回所有结果 (按 hosts 原始顺序)。
func (b *BatchExecutor) Run(ctx context.Context, hosts []string, task HostTask) []HostResult {
	switch b.opts.Strategy {
	case StrategyParallel:
		return b.runParallel(ctx, hosts, task)
	case StrategyRolling:
		return b.runRolling(ctx, hosts, task)
	default:
		return b.runSerial(ctx, hosts, task)
	}
}

func (b *BatchExecutor) runSerial(ctx context.Context, hosts []string, task HostTask) []HostResult {
	results := make([]HostResult, len(hosts))
	failures := 0
	for i, h := range hosts {
		if b.shouldStop(failures) {
			results[i] = HostResult{Host: h, Error: fmt.Errorf("stopped: failure threshold reached")}
			continue
		}
		start := time.Now()
		err := task(ctx, h)
		results[i] = HostResult{Host: h, Error: err, Elapsed: time.Since(start)}
		if err != nil {
			failures++
		}
	}
	return results
}

func (b *BatchExecutor) runParallel(ctx context.Context, hosts []string, task HostTask) []HostResult {
	results := make([]HostResult, len(hosts))
	var mu sync.Mutex
	failures := 0

	sem := make(chan struct{}, b.opts.MaxConcurrency)
	var wg sync.WaitGroup

	for i, h := range hosts {
		wg.Add(1)
		go func(idx int, host string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			mu.Lock()
			if b.shouldStop(failures) {
				mu.Unlock()
				results[idx] = HostResult{Host: host, Error: fmt.Errorf("stopped: failure threshold reached")}
				return
			}
			mu.Unlock()

			start := time.Now()
			err := task(ctx, host)
			results[idx] = HostResult{Host: host, Error: err, Elapsed: time.Since(start)}

			mu.Lock()
			if err != nil {
				failures++
			}
			mu.Unlock()
		}(i, h)
	}
	wg.Wait()
	return results
}

func (b *BatchExecutor) runRolling(ctx context.Context, hosts []string, task HostTask) []HostResult {
	results := make([]HostResult, len(hosts))
	failures := 0
	idx := 0

	for idx < len(hosts) {
		batchEnd := idx + b.opts.BatchSize
		if batchEnd > len(hosts) {
			batchEnd = len(hosts)
		}
		batch := hosts[idx:batchEnd]

		var wg sync.WaitGroup
		var mu sync.Mutex
		for j, h := range batch {
			wg.Add(1)
			go func(offset int, host string) {
				defer wg.Done()
				start := time.Now()
				err := task(ctx, host)
				results[idx+offset] = HostResult{Host: host, Error: err, Elapsed: time.Since(start)}
				mu.Lock()
				if err != nil {
					failures++
				}
				mu.Unlock()
			}(j, h)
		}
		wg.Wait()

		idx = batchEnd
		if b.shouldStop(failures) {
			for i := idx; i < len(hosts); i++ {
				results[i] = HostResult{Host: hosts[i], Error: fmt.Errorf("stopped: failure threshold reached")}
			}
			break
		}

		if idx < len(hosts) && b.opts.Interval > 0 {
			time.Sleep(b.opts.Interval)
		}
	}
	return results
}

func (b *BatchExecutor) shouldStop(failures int) bool {
	if b.opts.MaxFailures <= 0 {
		return failures > 0
	}
	return failures >= b.opts.MaxFailures
}

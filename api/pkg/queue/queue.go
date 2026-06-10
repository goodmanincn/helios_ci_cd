// Package queue 封装 Asynq 客户端,提供面向业务的 enqueue API。
//
// 设计:
//   - api 进程持有 Client (只 enqueue,不消费)
//   - worker 进程持有 Server (从 Redis 拉任务执行)
//   - 测试可用 fake enqueuer (见 fake.go)
package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/hibiken/asynq"

	"github.com/helios-cicd/helios/api/pkg/tasks"
)

// Enqueuer 抽象 enqueue 行为,handler 依赖接口便于测试。
type Enqueuer interface {
	EnqueueGitCheckout(ctx context.Context, p *tasks.GitCheckoutPayload) (taskID string, err error)
	EnqueueWebhookRegister(ctx context.Context, p *tasks.WebhookRegisterPayload) (taskID string, err error)
	EnqueueRunBuild(ctx context.Context, p *tasks.RunBuildPayload) (taskID string, err error)
	// EnqueueApprovalTimeout 入延时任务 (asynq.ProcessIn). delay <=0 时立即执行 (兜底, 实际不该出现).
	EnqueueApprovalTimeout(ctx context.Context, p *tasks.ApprovalTimeoutPayload, delay time.Duration) (taskID string, err error)
	Close() error
}

// AsynqEnqueuer 生产实现,基于 Redis。
type AsynqEnqueuer struct {
	client *asynq.Client
}

// New 用 Redis 地址构造 enqueuer。
func New(redisAddr string) *AsynqEnqueuer {
	c := asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
	return &AsynqEnqueuer{client: c}
}

// Close 关闭底层连接。
func (e *AsynqEnqueuer) Close() error {
	return e.client.Close()
}

// EnqueueGitCheckout 把 git checkout 任务入队 (default 队列, 最多重试 3 次, 单任务超时 5 分钟)。
func (e *AsynqEnqueuer) EnqueueGitCheckout(ctx context.Context, p *tasks.GitCheckoutPayload) (string, error) {
	if err := p.Validate(); err != nil {
		return "", fmt.Errorf("payload: %w", err)
	}
	body, err := p.Marshal()
	if err != nil {
		return "", err
	}
	t := asynq.NewTask(tasks.TypeGitCheckout, body,
		asynq.Queue(tasks.QueueDefault),
		asynq.MaxRetry(3),
		asynq.Timeout(5*time.Minute),
		asynq.Retention(24*time.Hour),
	)
	info, err := e.client.EnqueueContext(ctx, t)
	if err != nil {
		return "", err
	}
	return info.ID, nil
}

// EnqueueWebhookRegister 把 webhook 注册任务入队 (default 队列, 最多重试 5 次,
// 单任务超时 30 秒, 完成后保留 7 天用于排查注册失败)。
func (e *AsynqEnqueuer) EnqueueWebhookRegister(ctx context.Context, p *tasks.WebhookRegisterPayload) (string, error) {
	if err := p.Validate(); err != nil {
		return "", fmt.Errorf("payload: %w", err)
	}
	body, err := p.Marshal()
	if err != nil {
		return "", err
	}
	t := asynq.NewTask(tasks.TypeWebhookRegister, body,
		asynq.Queue(tasks.QueueDefault),
		asynq.MaxRetry(5),
		asynq.Timeout(30*time.Second),
		asynq.Retention(7*24*time.Hour),
	)
	info, err := e.client.EnqueueContext(ctx, t)
	if err != nil {
		return "", err
	}
	return info.ID, nil
}

// EnqueueRunBuild 把 build/execute 任务入队。retry=3, 超时=10 分钟。
func (e *AsynqEnqueuer) EnqueueRunBuild(ctx context.Context, p *tasks.RunBuildPayload) (string, error) {
	if err := p.Validate(); err != nil {
		return "", fmt.Errorf("payload: %w", err)
	}
	body, err := p.Marshal()
	if err != nil {
		return "", err
	}
	t := asynq.NewTask(tasks.TypeRunBuild, body,
		asynq.Queue(tasks.QueueDefault),
		asynq.MaxRetry(3),
		asynq.Timeout(10*time.Minute),
		asynq.Retention(24*time.Hour),
	)
	info, err := e.client.EnqueueContext(ctx, t)
	if err != nil {
		return "", err
	}
	return info.ID, nil
}

// EnqueueApprovalTimeout 入审批超时延时任务 (T2.6.3)。
//
//   - delay >0: asynq.ProcessIn(delay) 进 scheduled set, 到时入 critical 队列消费
//   - delay <=0: 立即入队 (兜底; 调用方应保证 delay >0)
//   - MaxRetry=0: 超时本身就是终态信号, 失败重试无意义 (handler 也走幂等)
//   - critical 队列优先级 6, 远高于 default=3, 保证人工等待的反馈不被普通构建积压
func (e *AsynqEnqueuer) EnqueueApprovalTimeout(ctx context.Context, p *tasks.ApprovalTimeoutPayload, delay time.Duration) (string, error) {
	if err := p.Validate(); err != nil {
		return "", fmt.Errorf("payload: %w", err)
	}
	body, err := p.Marshal()
	if err != nil {
		return "", err
	}
	opts := []asynq.Option{
		asynq.Queue(tasks.QueueCritical),
		asynq.MaxRetry(0),
		asynq.Timeout(30 * time.Second),
		asynq.Retention(7 * 24 * time.Hour), // 保留 7 天便于排查超时分支
	}
	if delay > 0 {
		opts = append(opts, asynq.ProcessIn(delay))
	}
	t := asynq.NewTask(tasks.TypeApprovalTimeout, body, opts...)
	info, err := e.client.EnqueueContext(ctx, t)
	if err != nil {
		return "", err
	}
	return info.ID, nil
}

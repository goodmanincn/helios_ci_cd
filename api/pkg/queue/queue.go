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
		asynq.Retention(24*time.Hour), // 完成后保留 1d 便于排障
	)
	info, err := e.client.EnqueueContext(ctx, t)
	if err != nil {
		return "", err
	}
	return info.ID, nil
}

package queue

import (
	"context"
	"sync"

	"github.com/helios-cicd/helios/api/pkg/tasks"
)

// FakeEnqueuer 测试用 enqueuer,记录所有调用便于断言。
type FakeEnqueuer struct {
	mu        sync.Mutex
	Checkouts []*tasks.GitCheckoutPayload
	Webhooks  []*tasks.WebhookRegisterPayload
	RunBuilds []*tasks.RunBuildPayload
	// NextID 注入下一个任务返回的 ID,空则按序号自增。
	NextID string
	// Err 注入下一次调用的错误。
	Err error
}

// NewFake 构造。
func NewFake() *FakeEnqueuer { return &FakeEnqueuer{} }

func (f *FakeEnqueuer) EnqueueGitCheckout(_ context.Context, p *tasks.GitCheckoutPayload) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	if err := p.Validate(); err != nil {
		return "", err
	}
	cp := *p
	f.Checkouts = append(f.Checkouts, &cp)
	if f.NextID != "" {
		return f.NextID, nil
	}
	return "fake-task-id", nil
}

func (f *FakeEnqueuer) EnqueueWebhookRegister(_ context.Context, p *tasks.WebhookRegisterPayload) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	if err := p.Validate(); err != nil {
		return "", err
	}
	cp := *p
	f.Webhooks = append(f.Webhooks, &cp)
	if f.NextID != "" {
		return f.NextID, nil
	}
	return "fake-task-id", nil
}

func (f *FakeEnqueuer) EnqueueRunBuild(_ context.Context, p *tasks.RunBuildPayload) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	if err := p.Validate(); err != nil {
		return "", err
	}
	cp := *p
	f.RunBuilds = append(f.RunBuilds, &cp)
	if f.NextID != "" {
		return f.NextID, nil
	}
	return "fake-task-id", nil
}

func (f *FakeEnqueuer) Close() error { return nil }

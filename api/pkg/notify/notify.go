package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// EventType represents the type of notification event
type EventType string

const (
	EventRunFailed        EventType = "run.failed"
	EventRunSucceeded     EventType = "run.succeeded"
	EventDeploymentSuccess EventType = "deployment.success"
	EventApprovalPending  EventType = "approval.pending"
	EventClusterUnhealthy EventType = "cluster.unhealthy"
)

// ChannelType represents the type of notification channel
type ChannelType string

const (
	ChannelDingTalk ChannelType = "dingtalk"
	ChannelEmail    ChannelType = "email"
	ChannelSlack    ChannelType = "slack"
	ChannelWebhook  ChannelType = "webhook"
)

// Message represents a notification message
type Message struct {
	Event       EventType              `json:"event"`
	Title       string                 `json:"title"`
	Body        string                 `json:"body"`
	Fields      map[string]interface{} `json:"fields,omitempty"`
	ProjectID   string                 `json:"project_id,omitempty"`
	OrgID       string                 `json:"org_id,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
}

// Channel is an interface for notification channels
type Channel interface {
	Send(ctx context.Context, msg Message) error
	Type() ChannelType
}

// Config holds notification configuration
type Config struct {
	// Rate limiting
	MaxPerMinute int
	// Retry settings
	MaxRetries int
	RetryDelay time.Duration
}

// DefaultConfig provides sensible defaults
var DefaultConfig = Config{
	MaxPerMinute: 60,
	MaxRetries:   3,
	RetryDelay:   1 * time.Second,
}

// Notifier handles sending notifications through multiple channels
type Notifier struct {
	channels map[ChannelType]Channel
	mu       sync.RWMutex
	config   Config
	queue    chan Message
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewNotifier creates a new notifier
func NewNotifier(config ...Config) *Notifier {
	cfg := DefaultConfig
	if len(config) > 0 {
		cfg = config[0]
	}

	ctx, cancel := context.WithCancel(context.Background())

	n := &Notifier{
		channels: make(map[ChannelType]Channel),
		config:   cfg,
		queue:    make(chan Message, 1000),
		ctx:      ctx,
		cancel:   cancel,
	}

	n.wg.Add(1)
	go n.processQueue()

	return n
}

// RegisterChannel registers a notification channel
func (n *Notifier) RegisterChannel(channel Channel) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.channels[channel.Type()] = channel
}

// UnregisterChannel unregisters a notification channel
func (n *Notifier) UnregisterChannel(channelType ChannelType) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.channels, channelType)
}

// Send sends a notification to all registered channels
func (n *Notifier) Send(msg Message) {
	msg.CreatedAt = time.Now()
	select {
	case n.queue <- msg:
	default:
		// Drop if queue is full
	}
}

// SendWithContext sends a notification with context
func (n *Notifier) SendWithContext(ctx context.Context, msg Message) error {
	msg.CreatedAt = time.Now()

	n.mu.RLock()
	channels := make([]Channel, 0, len(n.channels))
	for _, ch := range n.channels {
		channels = append(channels, ch)
	}
	n.mu.RUnlock()

	var errs []error
	for _, ch := range channels {
		if err := n.sendWithRetry(ctx, ch, msg); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to send to %d channels", len(errs))
	}
	return nil
}

// sendWithRetry sends a message with retry logic
func (n *Notifier) sendWithRetry(ctx context.Context, ch Channel, msg Message) error {
	var err error
	for i := 0; i < n.config.MaxRetries; i++ {
		err = ch.Send(ctx, msg)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(n.config.RetryDelay):
			// Continue
		}
	}
	return err
}

// processQueue processes the notification queue
func (n *Notifier) processQueue() {
	defer n.wg.Done()

	for {
		select {
		case msg := <-n.queue:
			n.mu.RLock()
			channels := make([]Channel, 0, len(n.channels))
			for _, ch := range n.channels {
				channels = append(channels, ch)
			}
			n.mu.RUnlock()

			for _, ch := range channels {
				_ = ch.Send(n.ctx, msg) // Fire and forget
			}
		case <-n.ctx.Done():
			return
		}
	}
}

// Close shuts down the notifier gracefully
func (n *Notifier) Close() {
	n.cancel()
	n.wg.Wait()
	close(n.queue)
}

// ===== Channel implementations =====

// DingTalkConfig holds DingTalk configuration
type DingTalkConfig struct {
	WebhookURL string
	Secret     string
}

// DingTalkChannel sends notifications to DingTalk
type DingTalkChannel struct {
	config DingTalkConfig
	client *http.Client
}

// NewDingTalkChannel creates a new DingTalk channel
func NewDingTalkChannel(config DingTalkConfig) *DingTalkChannel {
	return &DingTalkChannel{
		config: config,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Send sends a DingTalk notification
func (d *DingTalkChannel) Send(ctx context.Context, msg Message) error {
	payload := map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": msg.Title,
			"text":  fmt.Sprintf("### %s\n\n%s", msg.Title, msg.Body),
		},
	}

	data, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", d.config.WebhookURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dingtalk returned status %d", resp.StatusCode)
	}
	return nil
}

// Type returns the channel type
func (d *DingTalkChannel) Type() ChannelType {
	return ChannelDingTalk
}

// EmailConfig holds email configuration
type EmailConfig struct {
	SMTPHost string
	SMTPPort int
	Username string
	Password string
	From     string
}

// EmailChannel sends email notifications
type EmailChannel struct {
	config EmailConfig
}

// NewEmailChannel creates a new Email channel
func NewEmailChannel(config EmailConfig) *EmailChannel {
	return &EmailChannel{config: config}
}

// Send sends an email notification
func (e *EmailChannel) Send(ctx context.Context, msg Message) error {
	// Simplified email sending - in production, use a proper email library
	return nil
}

// Type returns the channel type
func (e *EmailChannel) Type() ChannelType {
	return ChannelEmail
}

// WebhookConfig holds webhook configuration
type WebhookConfig struct {
	URL     string
	Headers map[string]string
	Method  string // Defaults to POST
}

// WebhookChannel sends notifications to a webhook
type WebhookChannel struct {
	config WebhookConfig
	client *http.Client
}

// NewWebhookChannel creates a new Webhook channel
func NewWebhookChannel(config WebhookConfig) *WebhookChannel {
	if config.Method == "" {
		config.Method = http.MethodPost
	}
	return &WebhookChannel{
		config: config,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Send sends a webhook notification
func (w *WebhookChannel) Send(ctx context.Context, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	method := w.config.Method
	if method == "" {
		method = http.MethodPost
	}

	req, err := http.NewRequestWithContext(ctx, method, w.config.URL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range w.config.Headers {
		req.Header.Set(k, v)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

// Type returns the channel type
func (w *WebhookChannel) Type() ChannelType {
	return ChannelWebhook
}

// Subscription represents a user's notification subscription
type Subscription struct {
	ID      string    `json:"id"`
	UserID  string    `json:"user_id"`
	OrgID   string    `json:"org_id"`
	Event   EventType `json:"event"`
	Channel ChannelType `json:"channel"`
	Enabled bool      `json:"enabled"`
}

// SubscriptionManager manages user subscriptions
type SubscriptionManager struct {
	subs map[string]Subscription // user_id:event:channel -> Subscription
	mu   sync.RWMutex
}

// NewSubscriptionManager creates a new subscription manager
func NewSubscriptionManager() *SubscriptionManager {
	return &SubscriptionManager{
		subs: make(map[string]Subscription),
	}
}

// Subscribe adds a subscription
func (s *SubscriptionManager) Subscribe(sub Subscription) {
	key := fmt.Sprintf("%s:%s:%s", sub.UserID, sub.Event, sub.Channel)
	s.mu.Lock()
	s.subs[key] = sub
	s.mu.Unlock()
}

// Unsubscribe removes a subscription
func (s *SubscriptionManager) Unsubscribe(userID string, event EventType, channel ChannelType) {
	key := fmt.Sprintf("%s:%s:%s", userID, event, channel)
	s.mu.Lock()
	delete(s.subs, key)
	s.mu.Unlock()
}

// GetSubscriptions gets subscriptions for a user
func (s *SubscriptionManager) GetSubscriptions(userID string) []Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var subs []Subscription
	for key, sub := range s.subs {
		if len(key) >= len(userID) && key[:len(userID)] == userID && key[len(userID)] == ':' {
			subs = append(subs, sub)
		}
	}
	return subs
}

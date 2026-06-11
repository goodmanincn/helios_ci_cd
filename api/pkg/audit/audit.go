package audit

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Action represents the type of action performed
type Action string

const (
	ActionCreate Action = "create"
	ActionRead   Action = "read"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
)

// Entry represents an audit log entry
type Entry struct {
	ID          string          `json:"id" gorm:"primaryKey"`
	OrgID       string          `json:"org_id" gorm:"index"`
	ActorID     string          `json:"actor_id" gorm:"index"`
	ActorType   string          `json:"actor_type"`
	Action      Action          `json:"action"`
	Resource    string          `json:"resource"`
	ResourceID  string          `json:"resource_id"`
	Before      json.RawMessage `json:"before,omitempty"`
	After       json.RawMessage `json:"after,omitempty"`
	IPAddress   string          `json:"ip_address"`
	UserAgent   string          `json:"user_agent"`
	CreatedAt   time.Time       `json:"created_at" gorm:"index"`
	ArchivedAt  *time.Time      `json:"archived_at,omitempty"`
}

// TableName specifies the table name for AuditEntry
func (Entry) TableName() string {
	return "audit_logs"
}

// Config holds audit configuration
type Config struct {
	// BufferSize is the size of the channel buffer for async logging
	BufferSize int
	// BatchSize is the number of entries to batch before writing to DB
	BatchSize int
	// BatchTimeout is the timeout for flushing batches
	BatchTimeout time.Duration
	// ArchiveAfter is the duration after which entries are archived
	ArchiveAfter time.Duration
}

// DefaultConfig provides sensible defaults
var DefaultConfig = Config{
	BufferSize:   1000,
	BatchSize:    100,
	BatchTimeout: 5 * time.Second,
	ArchiveAfter: 30 * 24 * time.Hour, // 30 days
}

// Logger handles audit logging
type Logger struct {
	db      *gorm.DB
	config  Config
	entries chan Entry
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewLogger creates a new audit logger
func NewLogger(db *gorm.DB, config ...Config) *Logger {
	cfg := DefaultConfig
	if len(config) > 0 {
		cfg = config[0]
	}

	ctx, cancel := context.WithCancel(context.Background())

	l := &Logger{
		db:      db,
		config:  cfg,
		entries: make(chan Entry, cfg.BufferSize),
		ctx:     ctx,
		cancel:  cancel,
	}

	l.wg.Add(1)
	go l.processLoop()

	return l
}

// Log logs an audit entry
func (l *Logger) Log(entry Entry) {
	select {
	case l.entries <- entry:
	default:
		// Drop entry if buffer is full to avoid blocking main flow
	}
}

// LogRequestContext logs an entry with request context
func (l *Logger) LogRequestContext(c *gin.Context, entry Entry) {
	entry.IPAddress = c.ClientIP()
	entry.UserAgent = c.GetHeader("User-Agent")

	// Get actor info from context
	if actorID, exists := c.Get("user_id"); exists {
		entry.ActorID = actorID.(string)
		entry.ActorType = "user"
	}

	if orgID, exists := c.Get("org_id"); exists {
		entry.OrgID = orgID.(string)
	}

	entry.CreatedAt = time.Now()
	l.Log(entry)
}

// processLoop processes audit entries asynchronously
func (l *Logger) processLoop() {
	defer l.wg.Done()

	var batch []Entry
	ticker := time.NewTicker(l.config.BatchTimeout)
	defer ticker.Stop()

	for {
		select {
		case entry := <-l.entries:
			batch = append(batch, entry)
			if len(batch) >= l.config.BatchSize {
				l.flush(batch)
				batch = nil
			}
		case <-ticker.C:
			if len(batch) > 0 {
				l.flush(batch)
				batch = nil
			}
		case <-l.ctx.Done():
			if len(batch) > 0 {
				l.flush(batch)
			}
			return
		}
	}
}

// flush writes a batch of entries to the database
func (l *Logger) flush(entries []Entry) {
	if len(entries) == 0 {
		return
	}

	// Set IDs if not set
	for i := range entries {
		if entries[i].ID == "" {
			entries[i].ID = generateID()
		}
		if entries[i].CreatedAt.IsZero() {
			entries[i].CreatedAt = time.Now()
		}
	}

	// Write to database in a transaction
	if err := l.db.CreateInBatches(entries, len(entries)).Error; err != nil {
		// Just log the error, don't fail the main flow
	}
}

// Close shuts down the logger gracefully
func (l *Logger) Close() {
	l.cancel()
	l.wg.Wait()
	close(l.entries)
}

// QueryOptions represents options for querying audit logs
type QueryOptions struct {
	OrgID     string
	ActorID   string
	Action    Action
	Resource  string
	FromTime  *time.Time
	ToTime    *time.Time
	Limit     int
	Offset    int
}

// Query queries audit logs
func (l *Logger) Query(ctx context.Context, opts QueryOptions) ([]Entry, int64, error) {
	var entries []Entry
	var total int64

	query := l.db.WithContext(ctx).Model(&Entry{})

	if opts.OrgID != "" {
		query = query.Where("org_id = ?", opts.OrgID)
	}
	if opts.ActorID != "" {
		query = query.Where("actor_id = ?", opts.ActorID)
	}
	if opts.Action != "" {
		query = query.Where("action = ?", opts.Action)
	}
	if opts.Resource != "" {
		query = query.Where("resource = ?", opts.Resource)
	}
	if opts.FromTime != nil {
		query = query.Where("created_at >= ?", *opts.FromTime)
	}
	if opts.ToTime != nil {
		query = query.Where("created_at <= ?", *opts.ToTime)
	}

	// Count total
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Query with pagination
	query = query.Order("created_at DESC")
	if opts.Limit > 0 {
		query = query.Limit(opts.Limit)
	}
	if opts.Offset > 0 {
		query = query.Offset(opts.Offset)
	}

	if err := query.Find(&entries).Error; err != nil {
		return nil, 0, err
	}

	return entries, total, nil
}

// Get retrieves a single audit entry by ID
func (l *Logger) Get(ctx context.Context, id string) (*Entry, error) {
	var entry Entry
	if err := l.db.WithContext(ctx).First(&entry, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &entry, nil
}

// ArchiveOldEntries archives old entries to storage
func (l *Logger) ArchiveOldEntries(ctx context.Context) error {
	cutoff := time.Now().Add(-l.config.ArchiveAfter)

	// In a real implementation, you would:
	// 1. Query old entries
	// 2. Write them to object storage (S3, etc.)
	// 3. Mark them as archived
	// 4. Delete them from the main table (or just keep them)

	// For now, just mark them as archived
	now := time.Now()
	result := l.db.WithContext(ctx).
		Model(&Entry{}).
		Where("created_at < ? AND archived_at IS NULL", cutoff).
		Update("archived_at", now)

	return result.Error
}

// generateID generates a unique ID for audit entries
func generateID() string {
	// Use a simple timestamp-based ID for now
	// In production, use a proper UUID or snowflake ID
	return time.Now().Format("20060102150405.000000")
}

// MiddlewareOptions represents options for the audit middleware
type MiddlewareOptions struct {
	Logger     *Logger
	ShouldLog  func(c *gin.Context) bool
	ResourceFn func(c *gin.Context) (string, string)
}

// Middleware creates a Gin middleware for audit logging
func Middleware(opts MiddlewareOptions) gin.HandlerFunc {
	return func(c *gin.Context) {
		if opts.ShouldLog != nil && !opts.ShouldLog(c) {
			c.Next()
			return
		}

		// Determine action from HTTP method
		var action Action
		switch c.Request.Method {
		case http.MethodPost:
			action = ActionCreate
		case http.MethodGet:
			action = ActionRead
		case http.MethodPut, http.MethodPatch:
			action = ActionUpdate
		case http.MethodDelete:
			action = ActionDelete
		}

		// Get resource information
		resource := ""
		resourceID := ""
		if opts.ResourceFn != nil {
			resource, resourceID = opts.ResourceFn(c)
		}

		// Log before request is processed (read only)
		// We don't have before/after data yet
		entry := Entry{
			Action:     action,
			Resource:   resource,
			ResourceID: resourceID,
		}

		// Process the request
		c.Next()

		// Log after request is processed if we have a logger
		if opts.Logger != nil {
			opts.Logger.LogRequestContext(c, entry)
		}
	}
}

// DefaultShouldLog determines if a request should be logged
func DefaultShouldLog(c *gin.Context) bool {
	// Only log write operations by default
	switch c.Request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

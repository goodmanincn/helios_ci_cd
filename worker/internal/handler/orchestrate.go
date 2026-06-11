package handler

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"github.com/hibiken/asynq"

	"github.com/helios-cicd/helios/api/pkg/logarchive"
	"github.com/helios-cicd/helios/api/pkg/queue"
	"github.com/helios-cicd/helios/api/pkg/runengine"
	"github.com/helios-cicd/helios/api/pkg/runstate"
	"github.com/helios-cicd/helios/api/pkg/tasks"
)

// OrchestrateHandler 处理 helios:run:orchestrate (T2.2.4)。
type OrchestrateHandler struct {
	db           *sql.DB
	machine      *runstate.Machine
	workspaceDir string
	enq          *queue.AsynqEnqueuer
	timeoutEnq   runengine.TimeoutEnqueuer
	archive      *logarchive.Service
}

// NewOrchestrate 构造。
func NewOrchestrate(db *sql.DB, machine *runstate.Machine, workspaceDir string, enq *queue.AsynqEnqueuer, archive *logarchive.Service) *OrchestrateHandler {
	return &OrchestrateHandler{
		db: db, machine: machine, workspaceDir: workspaceDir,
		enq: enq, timeoutEnq: enq, archive: archive,
	}
}

func (h *OrchestrateHandler) ProcessTask(ctx context.Context, t *asynq.Task) error {
	p, err := tasks.UnmarshalRunOrchestrate(t.Payload())
	if err != nil {
		return fmt.Errorf("invalid payload: %w: %w", err, asynq.SkipRetry)
	}

	opts := runengine.AdvanceOpts{
		DB: h.db, Machine: h.machine, WorkspaceDir: h.workspaceDir,
		StageEnq: h.enq, TimeoutEnq: h.timeoutEnq,
	}
	res, err := runengine.Advance(ctx, opts, p.RunID, p.ProjectID)
	if err != nil {
		return fmt.Errorf("advance: %w", err)
	}
	if res.Done {
		log.Printf("orchestrate: run_id=%d finished outcome=%s", p.RunID, res.RunOutcome)
		h.archiveBestEffort(ctx, p.RunID)
	}
	return nil
}

func (h *OrchestrateHandler) archiveBestEffort(ctx context.Context, runID int64) {
	if h.archive == nil {
		return
	}
	stat, err := h.archive.ArchiveAndDrop(ctx, runID)
	if err != nil {
		log.Printf("orchestrate logarchive: run=%d err=%v", runID, err)
		return
	}
	if !stat.Skipped {
		log.Printf("orchestrate logarchive: run=%d archived %d entries", runID, stat.Entries)
	}
}

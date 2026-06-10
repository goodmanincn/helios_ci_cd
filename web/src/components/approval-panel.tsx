"use client";

// ApprovalPanel — 审批面板 (T2.6.4).
//
// 显示时机: run.status === "approval" 且有 pending request.
// 操作:
//   - 当前用户是 approver (用户名在 RequiredApprovers, 或含 '*' 通配) → 显示 Approve/Reject
//   - 非 approver → 显示 disabled + tooltip 列 approvers
//   - reject 强制带 comment, approve 选填
//   - 历史投票按时间排序展示
//
// 操作后调 onChanged 触发父级 refetch (后端是真理, mode=all 可能仍 pending).
import { useState } from "react";

import { ApiException } from "@/lib/api";
import { useAuthStore } from "@/lib/auth-store";
import {
  ApprovalRequest,
  approveStage,
  fmtTime,
  rejectStage,
} from "@/lib/runs-api";

interface Props {
  runId: number;
  requests: ApprovalRequest[];
  onChanged: () => void | Promise<void>;
}

export function ApprovalPanel({ runId, requests, onChanged }: Props) {
  const accessToken = useAuthStore((s) => s.accessToken);
  const me = useAuthStore((s) => s.user);

  if (!requests || requests.length === 0) return null;

  return (
    <div className="ap-panel">
      {requests.map((req) => (
        <ApprovalCard
          key={req.id}
          runId={runId}
          request={req}
          username={me?.username || ""}
          token={accessToken}
          onChanged={onChanged}
        />
      ))}
      <style jsx>{`
        .ap-panel {
          display: flex;
          flex-direction: column;
          gap: 12px;
          padding: 12px 16px;
          background: rgba(250, 204, 21, 0.04);
          border-bottom: 1px solid rgba(250, 204, 21, 0.18);
        }
      `}</style>
    </div>
  );
}

function ApprovalCard({
  runId,
  request,
  username,
  token,
  onChanged,
}: {
  runId: number;
  request: ApprovalRequest;
  username: string;
  token: string | null;
  onChanged: () => void | Promise<void>;
}) {
  const [comment, setComment] = useState("");
  const [acting, setActing] = useState<"approve" | "reject" | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const isPending = request.status === "pending";
  const canApprove = isPending && isApprover(username, request.required_approvers);
  const disabledMsg = !isPending
    ? `请求已 ${request.status}`
    : !canApprove
    ? `仅以下审批者可操作: ${request.required_approvers.join(", ")}`
    : "";

  const run = async (decision: "approve" | "reject") => {
    if (!token) return;
    if (decision === "reject" && comment.trim().length < 3) {
      setErr("拒绝必须填写说明 (>=3 字符)");
      return;
    }
    setActing(decision);
    setErr(null);
    try {
      if (decision === "approve") {
        await approveStage(token, runId, request.stage_id, comment);
      } else {
        await rejectStage(token, runId, request.stage_id, comment);
      }
      setComment("");
      await onChanged();
    } catch (e) {
      setErr(e instanceof ApiException ? e.message : String(e));
    } finally {
      setActing(null);
    }
  };

  return (
    <div className="ap-card">
      <div className="ap-head">
        <span className="ap-stage">👤 待审批 · stage <code>{request.stage_id}</code></span>
        <span className="ap-mode">mode={request.mode}</span>
        {request.timeout_at && (
          <span className="ap-deadline">截止 {fmtTime(request.timeout_at)}</span>
        )}
        <span className="ap-status" data-st={request.status}>{request.status}</span>
      </div>
      <div className="ap-approvers">
        审批名单: {request.required_approvers.join(", ")}
      </div>

      {isPending && (
        <>
          <textarea
            value={comment}
            onChange={(e) => setComment(e.target.value)}
            placeholder={canApprove ? "可选: 留言 (拒绝必填)" : "..."}
            disabled={!canApprove || acting !== null}
            className="ap-comment"
            rows={2}
          />
          <div className="ap-actions">
            <button
              type="button"
              onClick={() => run("approve")}
              disabled={!canApprove || acting !== null}
              className="rd-btn"
              data-variant="primary"
              title={disabledMsg}
            >
              ✓ {acting === "approve" ? "提交中..." : "批准"}
            </button>
            <button
              type="button"
              onClick={() => run("reject")}
              disabled={!canApprove || acting !== null}
              className="rd-btn"
              data-variant="warn"
              title={disabledMsg}
            >
              ✗ {acting === "reject" ? "提交中..." : "拒绝"}
            </button>
            {!canApprove && (
              <span className="ap-hint">{disabledMsg}</span>
            )}
          </div>
          {err && <div className="ap-err">{err}</div>}
        </>
      )}

      {request.approvals.length > 0 && (
        <div className="ap-history">
          <div className="ap-history-head">投票记录</div>
          {request.approvals.map((v) => (
            <div key={v.id} className="ap-vote" data-dec={v.decision}>
              <span className="ap-vote-mark">
                {v.decision === "approve" ? "✓" : "✗"}
              </span>
              <span className="ap-vote-user">{v.username}</span>
              <span className="ap-vote-time">{fmtTime(v.created_at)}</span>
              {v.comment && <span className="ap-vote-comment">&ldquo;{v.comment}&rdquo;</span>}
            </div>
          ))}
        </div>
      )}

      <style jsx>{`
        .ap-card {
          background: var(--bg-elev);
          border: 1px solid var(--border);
          border-left: 3px solid #facc15;
          border-radius: 8px;
          padding: 12px 14px;
          display: flex;
          flex-direction: column;
          gap: 8px;
        }
        .ap-head {
          display: flex;
          align-items: center;
          gap: 12px;
          flex-wrap: wrap;
          font-size: 13px;
        }
        .ap-stage {
          font-weight: 600;
        }
        .ap-stage code,
        .ap-approvers code {
          background: var(--bg-elev-2);
          padding: 1px 6px;
          border-radius: 4px;
          font-family: ui-monospace, Menlo, monospace;
          font-size: 12px;
        }
        .ap-mode,
        .ap-deadline {
          font-size: 12px;
          color: var(--fg-mute);
        }
        .ap-status {
          margin-left: auto;
          font-size: 11px;
          padding: 2px 8px;
          border-radius: 999px;
          text-transform: uppercase;
          background: rgba(250, 204, 21, 0.18);
          color: #facc15;
        }
        .ap-status[data-st="approved"] {
          background: rgba(34, 197, 94, 0.18);
          color: #5eecaa;
        }
        .ap-status[data-st="rejected"],
        .ap-status[data-st="timeout"] {
          background: rgba(248, 113, 113, 0.18);
          color: #fb7185;
        }
        .ap-status[data-st="canceled"] {
          background: rgba(161, 161, 170, 0.18);
          color: #a1a1aa;
        }
        .ap-approvers {
          font-size: 12px;
          color: var(--fg-mute);
        }
        .ap-comment {
          width: 100%;
          background: var(--bg);
          border: 1px solid var(--border);
          color: var(--fg);
          border-radius: 6px;
          padding: 6px 8px;
          font-family: inherit;
          font-size: 13px;
          resize: vertical;
        }
        .ap-comment:disabled {
          opacity: 0.6;
          cursor: not-allowed;
        }
        .ap-actions {
          display: flex;
          gap: 8px;
          align-items: center;
        }
        .ap-hint {
          font-size: 11px;
          color: var(--fg-mute);
        }
        .ap-err {
          color: #fb7185;
          font-size: 12px;
        }
        .ap-history {
          margin-top: 4px;
          border-top: 1px dashed var(--border);
          padding-top: 8px;
          display: flex;
          flex-direction: column;
          gap: 4px;
        }
        .ap-history-head {
          font-size: 11px;
          color: var(--fg-mute);
          text-transform: uppercase;
          letter-spacing: 0.05em;
        }
        .ap-vote {
          display: flex;
          gap: 8px;
          align-items: baseline;
          font-size: 12px;
        }
        .ap-vote-mark {
          font-weight: 700;
        }
        .ap-vote[data-dec="approve"] .ap-vote-mark {
          color: #5eecaa;
        }
        .ap-vote[data-dec="reject"] .ap-vote-mark {
          color: #fb7185;
        }
        .ap-vote-user {
          font-weight: 600;
        }
        .ap-vote-time {
          color: var(--fg-mute);
        }
        .ap-vote-comment {
          color: var(--fg-mute);
          font-style: italic;
        }
      `}</style>
    </div>
  );
}

function isApprover(username: string, list: string[]): boolean {
  if (!username) return false;
  return list.some((a) => a === "*" || a === username);
}

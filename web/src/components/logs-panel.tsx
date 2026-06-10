"use client";

// LogsPanel — Run 日志面板 (T1.6.3)
// 行为:
//   1. 挂载后先 GET /api/v1/runs/:id/logs?source=auto&count=500 拉历史快照
//   2. 若 run 处于 running/pending, 用 EventSource 接 /logs/stream?token=<jwt> 实时追加
//      - 收到 'log' 事件 → 追加
//      - 收到 'end'      → 关流, 状态置 closed
//      - 收到 'ping'     → 忽略 (后端心跳)
//   3. 终态 run 只展示历史不开 stream
//   4. 用户向上滚动后停止 auto-scroll, 提供 "回到底部" 按钮
//   5. stdout/stderr/system 三色

import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { API_BASE, ApiException, apiFetch } from "@/lib/api";

export interface LogEntry {
  id?: string;
  ts: string;
  stream: "stdout" | "stderr" | "system" | string;
  line: string;
}

interface HistoryResp {
  source: "redis" | "archive" | "none" | string;
  entries: LogEntry[];
  next_id?: string;
  has_more?: boolean;
}

interface Props {
  runId: number;
  token: string;
  runStatus: string;
  /** 终态变化时父组件可主动 refresh — 这里不直接用, 但允许重挂载 */
  version?: number;
}

const terminal: Record<string, true> = {
  success: true,
  failed: true,
  canceled: true,
};

export function LogsPanel({ runId, token, runStatus }: Props) {
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [source, setSource] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [streaming, setStreaming] = useState<"off" | "connecting" | "open" | "closed">(
    terminal[runStatus] ? "off" : "connecting",
  );
  const [autoScroll, setAutoScroll] = useState(true);

  const esRef = useRef<EventSource | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const lastIdRef = useRef<string>("");

  // -- 1) load history snapshot
  const loadHistory = useCallback(async () => {
    setLoading(true);
    setErr(null);
    try {
      const r = await apiFetch<HistoryResp>(
        `/api/v1/runs/${runId}/logs?source=auto&count=500`,
        { token },
      );
      setSource(r.source);
      setEntries(r.entries || []);
      if (r.entries && r.entries.length > 0) {
        lastIdRef.current = r.entries[r.entries.length - 1].id || "";
      }
    } catch (e) {
      setErr(e instanceof ApiException ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [runId, token]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    loadHistory();
  }, [loadHistory]);

  // -- 2) open SSE if not terminal
  useEffect(() => {
    if (terminal[runStatus]) {
      // 状态变终态: 关掉流, 但保留已有日志
      if (esRef.current) {
        esRef.current.close();
        esRef.current = null;
      }
      setStreaming("off"); // eslint-disable-line react-hooks/set-state-in-effect
      return;
    }
    if (loading) return; // 等历史加载完再开流, 避免漏接 lastId

    const qs = new URLSearchParams();
    qs.set("token", token);
    if (lastIdRef.current) qs.set("from", lastIdRef.current);

    const url = `${API_BASE}/api/v1/runs/${runId}/logs/stream?${qs.toString()}`;
    setStreaming("connecting");

    const es = new EventSource(url);
    esRef.current = es;

    es.addEventListener("open", () => setStreaming("open"));

    es.addEventListener("log", (ev: MessageEvent) => {
      try {
        const data = JSON.parse(ev.data) as LogEntry;
        const id = ev.lastEventId || data.id;
        setEntries((prev) => [...prev, { ...data, id }]);
        if (id) lastIdRef.current = id;
      } catch {
        /* malformed event, skip */
      }
    });

    es.addEventListener("end", () => {
      setStreaming("closed");
      es.close();
      esRef.current = null;
    });

    es.addEventListener("ping", () => { /* keepalive */ });

    es.addEventListener("error", () => {
      // EventSource 会自动 reconnect, 但若已 close 就是真死
      if (es.readyState === EventSource.CLOSED) {
        setStreaming("closed");
        esRef.current = null;
      }
    });

    return () => {
      es.close();
      esRef.current = null;
    };
  }, [runId, token, runStatus, loading]);

  // -- 3) auto scroll
  useLayoutEffect(() => {
    if (!autoScroll) return;
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [entries, autoScroll]);

  function onScroll() {
    const el = scrollRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 16;
    setAutoScroll(atBottom);
  }

  function goBottom() {
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
    setAutoScroll(true);
  }

  return (
    <div className="card flex flex-col gap-2" style={{ padding: 0, overflow: "hidden" }}>
      <div
        className="flex items-center justify-between px-4 py-2"
        style={{ background: "var(--bg-elev-2)", borderBottom: "1px solid var(--border)" }}
      >
        <div className="flex items-center gap-3">
          <div className="text-sm font-semibold" style={{ color: "var(--fg)" }}>
            实时日志
          </div>
          <SourceBadge source={source} />
          <StreamBadge state={streaming} />
        </div>
        <div className="flex items-center gap-2 text-xs">
          <span style={{ color: "var(--fg-dim)" }}>{entries.length} 行</span>
          {!autoScroll && (
            <button className="btn" onClick={goBottom} style={{ padding: "2px 8px", fontSize: "0.7rem" }}>
              回到底部 ↓
            </button>
          )}
          <button className="btn" onClick={loadHistory} style={{ padding: "2px 8px", fontSize: "0.7rem" }}>
            刷新
          </button>
        </div>
      </div>

      {err && <div className="err-msg" style={{ margin: "8px 16px" }}>{err}</div>}

      <div
        ref={scrollRef}
        onScroll={onScroll}
        style={{
          background: "#0a0a0f",
          color: "#d4d4d8",
          fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
          fontSize: "0.78rem",
          lineHeight: 1.55,
          padding: "10px 14px",
          minHeight: 220,
          maxHeight: 520,
          overflow: "auto",
        }}
      >
        {loading && entries.length === 0 ? (
          <div style={{ color: "var(--fg-dim)" }}>加载历史日志中...</div>
        ) : entries.length === 0 ? (
          <div style={{ color: "var(--fg-dim)" }}>
            {terminal[runStatus] ? "(无日志)" : "(暂无输出, 等待中...)"}
          </div>
        ) : (
          entries.map((e, i) => <LogLine key={(e.id || "") + "-" + i} e={e} />)
        )}
      </div>
    </div>
  );
}

function LogLine({ e }: { e: LogEntry }) {
  const isErr = e.stream === "stderr";
  const isSys = e.stream === "system";
  const color = isErr ? "#fb7185" : isSys ? "#fbd55a" : "#d4d4d8";
  const tag =
    isSys ? "SYS" : isErr ? "ERR" : "OUT";
  const tagColor = isSys ? "#fbd55a" : isErr ? "#fb7185" : "#5eecaa";
  return (
    <div style={{ whiteSpace: "pre-wrap", wordBreak: "break-word", color }}>
      <span
        style={{
          color: "var(--fg-dim)",
          marginRight: 8,
          userSelect: "none",
        }}
      >
        {shortTs(e.ts)}
      </span>
      <span
        style={{
          color: tagColor,
          marginRight: 8,
          fontWeight: 600,
          fontSize: "0.7rem",
          userSelect: "none",
        }}
      >
        {tag}
      </span>
      <span>{e.line}</span>
    </div>
  );
}

function SourceBadge({ source }: { source: string }) {
  if (!source) return null;
  const label =
    source === "redis" ? "Redis" :
    source === "archive" ? "归档" :
    source === "none" ? "无" : source;
  return (
    <span
      style={{
        fontSize: "0.65rem",
        padding: "1px 6px",
        borderRadius: 4,
        background: "var(--bg-elev)",
        color: "var(--fg-mute)",
        border: "1px solid var(--border)",
      }}
    >
      源: {label}
    </span>
  );
}

function StreamBadge({ state }: { state: "off" | "connecting" | "open" | "closed" }) {
  if (state === "off") return null;
  const map = {
    connecting: { color: "#fbd55a", label: "● 连接中" },
    open: { color: "#5eecaa", label: "● LIVE" },
    closed: { color: "var(--fg-dim)", label: "● 已断开" },
  } as const;
  const m = map[state];
  return (
    <span style={{ fontSize: "0.65rem", color: m.color, fontWeight: 600 }}>
      {m.label}
    </span>
  );
}

function shortTs(s: string): string {
  if (!s) return "";
  // ISO 字串裁到 HH:MM:SS.sss; 拿不到就原样返回头 12 字符
  const m = s.match(/T(\d{2}:\d{2}:\d{2}(?:\.\d{1,3})?)/);
  if (m) return m[1];
  return s.slice(0, 12);
}

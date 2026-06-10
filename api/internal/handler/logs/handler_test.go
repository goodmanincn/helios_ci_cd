package logs_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	logsh "github.com/helios-cicd/helios/api/internal/handler/logs"
	"github.com/helios-cicd/helios/api/pkg/logstream"
)

func requireRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("HELIOS_REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6380"
	}
	c := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := c.Ping(ctx).Err(); err != nil {
		t.Skipf("redis %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func freshRunID(t *testing.T) int64 {
	t.Helper()
	return time.Now().UnixNano() % 1_000_000_000
}

func newServer(t *testing.T, rdb *redis.Client) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	logsh.New(logstream.NewReader(rdb), nil).Register(v1)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// ===== History (T1.5.4 提前覆盖, 同 handler) =====

func TestHistory_ReturnsEntriesAndNext(t *testing.T) {
	rdb := requireRedis(t)
	w := logstream.NewWriter(rdb, logstream.Config{})
	rid := freshRunID(t)
	defer func() { _ = rdb.Del(context.Background(), logstream.StreamKey(rid)).Err() }()

	for i := 0; i < 5; i++ {
		w.Append(context.Background(), rid, logstream.StreamStdout, "line-"+itoa(i))
	}

	srv := newServer(t, rdb)
	resp, err := http.Get(srv.URL + "/api/v1/runs/" + itoa64(rid) + "/logs?count=3")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var body struct {
		Entries []struct {
			ID, Ts, Stream, Line string
		} `json:"entries"`
		Next    string `json:"next"`
		HasMore bool   `json:"has_more"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Entries) != 3 {
		t.Fatalf("entries=%d want 3", len(body.Entries))
	}
	if !body.HasMore {
		t.Errorf("has_more=false want true")
	}
	if body.Next == "" {
		t.Errorf("next empty")
	}
	if body.Entries[0].Line != "line-0" || body.Entries[2].Line != "line-2" {
		t.Errorf("lines: %+v", body.Entries)
	}

	// 用 next 翻第二页
	resp2, err := http.Get(srv.URL + "/api/v1/runs/" + itoa64(rid) + "/logs?count=10&from=" + body.Entries[2].ID)
	if err != nil {
		t.Fatalf("GET p2: %v", err)
	}
	defer resp2.Body.Close()
	var body2 struct {
		Entries []struct{ Line string } `json:"entries"`
		HasMore bool                    `json:"has_more"`
	}
	_ = json.NewDecoder(resp2.Body).Decode(&body2)
	if len(body2.Entries) != 3 {
		t.Fatalf("p2 entries=%d want 3 (含起始 id)", len(body2.Entries))
	}
	if body2.HasMore {
		t.Errorf("p2 has_more=true want false")
	}
}

func TestHistory_BadRunID(t *testing.T) {
	rdb := requireRedis(t)
	srv := newServer(t, rdb)
	resp, err := http.Get(srv.URL + "/api/v1/runs/abc/logs")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status=%d want 400", resp.StatusCode)
	}
}

// ===== Stream (T1.5.2 SSE) =====

func TestStream_SSE_DeliversNewEntries(t *testing.T) {
	rdb := requireRedis(t)
	w := logstream.NewWriter(rdb, logstream.Config{})
	rid := freshRunID(t)
	defer func() { _ = rdb.Del(context.Background(), logstream.StreamKey(rid)).Err() }()

	// 先写 1 条已存在
	w.Append(context.Background(), rid, logstream.StreamStdout, "history-1")

	srv := newServer(t, rdb)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET",
		srv.URL+"/api/v1/runs/"+itoa64(rid)+"/logs/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type=%q", got)
	}

	// 启 goroutine 慢写 2 条新
	go func() {
		time.Sleep(200 * time.Millisecond)
		w.Append(context.Background(), rid, logstream.StreamStderr, "new-A")
		time.Sleep(100 * time.Millisecond)
		w.Append(context.Background(), rid, logstream.StreamStdout, "new-B")
	}()

	// 读 SSE: 解析 "event: log\nid: ...\ndata: {...}\n\n"
	br := bufio.NewReader(resp.Body)
	got := make([]map[string]string, 0, 3)
	var curEvent, curID, curData string
	timeout := time.After(4 * time.Second)
	for len(got) < 3 {
		select {
		case <-timeout:
			t.Fatalf("timeout, got=%d entries: %+v", len(got), got)
		default:
		}
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "event: "):
			curEvent = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "id: "):
			curID = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "data: "):
			curData = strings.TrimPrefix(line, "data: ")
		case line == "" && curEvent == "log":
			var d map[string]string
			if err := json.Unmarshal([]byte(curData), &d); err == nil {
				d["_id"] = curID
				got = append(got, d)
			}
			curEvent, curID, curData = "", "", ""
		case line == "":
			curEvent, curID, curData = "", "", ""
		}
	}

	cancel()
	if got[0]["line"] != "history-1" || got[0]["stream"] != "stdout" {
		t.Errorf("got[0]=%+v want history-1/stdout", got[0])
	}
	if got[1]["line"] != "new-A" || got[1]["stream"] != "stderr" {
		t.Errorf("got[1]=%+v want new-A/stderr", got[1])
	}
	if got[2]["line"] != "new-B" || got[2]["stream"] != "stdout" {
		t.Errorf("got[2]=%+v want new-B/stdout", got[2])
	}
	for i, g := range got {
		if g["_id"] == "" {
			t.Errorf("got[%d] missing SSE id", i)
		}
	}
}

func TestStream_SSE_FromDollarSkipsOld(t *testing.T) {
	rdb := requireRedis(t)
	w := logstream.NewWriter(rdb, logstream.Config{})
	rid := freshRunID(t)
	defer func() { _ = rdb.Del(context.Background(), logstream.StreamKey(rid)).Err() }()

	w.Append(context.Background(), rid, logstream.StreamStdout, "skip-me")

	srv := newServer(t, rdb)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET",
		srv.URL+"/api/v1/runs/"+itoa64(rid)+"/logs/stream?from=$", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	go func() {
		time.Sleep(200 * time.Millisecond)
		w.Append(context.Background(), rid, logstream.StreamStdout, "only-fresh")
	}()

	br := bufio.NewReader(resp.Body)
	var curEvent, curData string
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timeout")
		default:
		}
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "event: "):
			curEvent = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			curData = strings.TrimPrefix(line, "data: ")
		case line == "" && curEvent == "log":
			var d map[string]string
			_ = json.Unmarshal([]byte(curData), &d)
			if d["line"] == "skip-me" {
				t.Fatalf("should not have received skip-me")
			}
			if d["line"] == "only-fresh" {
				cancel()
				return
			}
			curEvent, curData = "", ""
		case line == "":
			curEvent, curData = "", ""
		}
	}
}

func itoa(i int) string  { return itoa64(int64(i)) }
func itoa64(i int64) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [24]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

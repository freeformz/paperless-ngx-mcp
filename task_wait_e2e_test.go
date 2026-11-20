//go:build unix

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// readMessages reads JSON-RPC messages from the process until stop returns
// true for one, and returns every message read, including that one.
func (p *mcpProcess) readMessages(t *testing.T, stop func(map[string]any) bool) []map[string]any {
	t.Helper()
	var seen []map[string]any
	timeout := time.After(processTimeout)
	for {
		select {
		case line, ok := <-p.msgs:
			if !ok {
				p.fatalf(t, "mcp stdout closed; read so far: %v", seen)
			}
			var msg map[string]any
			if err := json.Unmarshal(line, &msg); err != nil {
				continue
			}
			seen = append(seen, msg)
			if stop(msg) {
				return seen
			}
		case <-timeout:
			p.fatalf(t, "no matching message within %s; read: %v", processTimeout, seen)
		}
	}
}

func isResponse(id int) func(map[string]any) bool {
	return func(m map[string]any) bool {
		v, ok := m["id"].(float64)
		return ok && strconv.Itoa(int(v)) == strconv.Itoa(id)
	}
}

// TestMCPTaskWaitDoesNotBlockOtherCalls runs a long task_wait against a task
// that stays pending: other tool calls still answer, progress notifications
// reach the client, and closing stdin mid-wait still exits within 2s.
func TestMCPTaskWaitDoesNotBlockOtherCalls(t *testing.T) {
	paperless := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tasks/":
			json.NewEncoder(w).Encode(map[string]any{"count": 1, "results": []any{map[string]any{
				"id": 1, "task_id": testTaskUUID, "status": "pending",
			}}})
		case "/api/statistics/":
			w.Write([]byte(`{"documents_total": 3}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(paperless.Close)
	p := startMCP(t, paperless.URL)

	// More waits than mcp-go's default 5 stdio workers.
	var waitIDs []int
	for i := 0; i < 6; i++ {
		waitIDs = append(waitIDs, p.request(t, "tools/call", map[string]any{
			"name":      "task_wait",
			"arguments": map[string]any{"id": testTaskUUID, "timeout": 300},
			"_meta":     map[string]any{"progressToken": "wait-" + strconv.Itoa(i)},
		}))
	}
	time.Sleep(200 * time.Millisecond) // let the waits start polling
	statsID := p.request(t, "tools/call", map[string]any{"name": "statistics", "arguments": map[string]any{}})
	seen := p.readMessages(t, isResponse(statsID))

	// Each wait reports progress after its first poll; read until every token
	// has been seen, so the check doesn't depend on timing.
	tokens := map[string]bool{}
	check := func(msgs []map[string]any) {
		for _, m := range msgs {
			for _, id := range waitIDs {
				if isResponse(id)(m) {
					t.Fatalf("task_wait %d answered before its timeout: %v", id, m)
				}
			}
			if m["method"] == "notifications/progress" {
				params, _ := m["params"].(map[string]any)
				tok, _ := params["progressToken"].(string)
				if !strings.HasPrefix(tok, "wait-") || params["total"] != float64(300) {
					t.Errorf("progress notification = %v", m)
				}
				tokens[tok] = true
			}
		}
	}
	check(seen)
	if len(tokens) < len(waitIDs) {
		check(p.readMessages(t, func(m map[string]any) bool {
			if m["method"] == "notifications/progress" {
				params, _ := m["params"].(map[string]any)
				tok, _ := params["progressToken"].(string)
				tokens[tok] = true
			}
			return len(tokens) >= len(waitIDs)
		}))
	}
	if len(tokens) != len(waitIDs) {
		t.Errorf("progress seen for %d of %d waits: %v", len(tokens), len(waitIDs), tokens)
	}

	start := time.Now()
	if err := p.stdin.Close(); err != nil {
		p.fatalf(t, "close stdin: %s", err)
	}
	state := p.wait(t)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("mcp exited %s after stdin closed during task waits, want under 2s", elapsed)
	}
	if !state.Success() {
		t.Errorf("mcp ended with %s, want exit status 0\nstderr:\n%s", state, p.stderr.String())
	}
}

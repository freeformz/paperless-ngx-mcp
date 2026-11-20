package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

const testTaskUUID = "12345678-1234-1234-1234-123456789abc"

// fastPolling shrinks the polling schedule for the duration of a test.
func fastPolling(t *testing.T) {
	t.Helper()
	initial, maxi, grace := taskPollInitial, taskPollMax, taskNotFoundGrace
	taskPollInitial, taskPollMax, taskNotFoundGrace = 5*time.Millisecond, 20*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { taskPollInitial, taskPollMax, taskNotFoundGrace = initial, maxi, grace })
}

// taskSequence serves GET /api/tasks/ by stepping through statuses; an empty
// status string means "not found yet". The last entry repeats.
type taskSequence struct {
	mu       sync.Mutex
	statuses []string
	calls    int
	queries  []url.Values
}

func (s *taskSequence) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	i := min(s.calls, len(s.statuses)-1)
	s.calls++
	s.queries = append(s.queries, r.URL.Query())
	status := s.statuses[i]
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if status == "" {
		json.NewEncoder(w).Encode(paginatedResponse([]any{}, 0))
		return
	}
	task := map[string]any{
		"id": float64(77), "task_id": testTaskUUID, "status": status, "task_type": "bulk_update",
		"date_created": "2026-11-19T10:00:00-08:00",
	}
	if status == "failure" {
		task["result_data"] = map[string]any{"error_type": "PermissionError", "error_message": "Insufficient permissions"}
	}
	json.NewEncoder(w).Encode(paginatedResponse([]any{task}, 1))
}

func (s *taskSequence) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func taskSequenceClient(t *testing.T, statuses ...string) (*Client, *taskSequence) {
	t.Helper()
	seq := &taskSequence{statuses: statuses}
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tasks/", seq.handle)
	return testClientAndServer(t, rh), seq
}

func TestWaitForTaskReachesTerminalStatus(t *testing.T) {
	fastPolling(t)
	for _, tc := range []struct {
		final   string
		isError bool
	}{{"success", false}, {"failure", true}, {"revoked", true}} {
		t.Run(tc.final, func(t *testing.T) {
			client, seq := taskSequenceClient(t, "pending", "started", tc.final)
			result := callTool(t, handleTaskWait(client), map[string]any{"id": testTaskUUID})
			if result.IsError != tc.isError {
				t.Fatalf("IsError = %v, want %v: %s", result.IsError, tc.isError, resultText(t, result))
			}
			m := resultJSON(t, result)
			task := m["task"].(map[string]any)
			if task["status"] != tc.final || m["timed_out"] != nil {
				t.Errorf("outcome = %v, want final status %s and no timed_out", m, tc.final)
			}
			if seq.count() != 3 {
				t.Errorf("polls = %d, want 3", seq.count())
			}
			if tc.final == "failure" && !strings.Contains(resultText(t, result), "Insufficient permissions") {
				t.Errorf("failure result should carry result_data: %s", resultText(t, result))
			}
		})
	}
}

func TestWaitForTaskTimesOut(t *testing.T) {
	fastPolling(t)
	client, _ := taskSequenceClient(t, "pending")
	outcome, errRes := waitForTask(t.Context(), client, testTaskUUID, 40*time.Millisecond, 0, nil)
	if errRes != nil {
		t.Fatalf("timeout should not be an error: %s", resultText(t, errRes))
	}
	if outcome["timed_out"] != true || outcome["task"].(map[string]any)["status"] != "pending" {
		t.Errorf("outcome = %v, want timed_out with the pending task", outcome)
	}
	result, _ := taskWaitResult(outcome)
	if result.IsError {
		t.Error("a timed-out wait must not be a tool error")
	}
}

func TestWaitForTaskNotFound(t *testing.T) {
	fastPolling(t)
	t.Run("found within grace", func(t *testing.T) {
		client, seq := taskSequenceClient(t, "", "", "success")
		outcome, errRes := waitForTask(t.Context(), client, testTaskUUID, time.Second, taskNotFoundGrace, nil)
		if errRes != nil || outcome["task"].(map[string]any)["status"] != "success" {
			t.Fatalf("outcome = %v, err = %v", outcome, errRes)
		}
		if seq.count() != 3 {
			t.Errorf("polls = %d, want 3", seq.count())
		}
	})
	t.Run("not found after grace", func(t *testing.T) {
		client, _ := taskSequenceClient(t, "")
		_, errRes := waitForTask(t.Context(), client, testTaskUUID, time.Second, taskNotFoundGrace, nil)
		if errRes == nil || !strings.Contains(resultText(t, errRes), "no task with task_id") {
			t.Fatalf("want a not-found error, got %v", errRes)
		}
	})
	t.Run("task_wait fails at once", func(t *testing.T) {
		client, seq := taskSequenceClient(t, "")
		result := callTool(t, handleTaskWait(client), map[string]any{"id": testTaskUUID})
		assertIsError(t, result)
		if seq.count() != 1 {
			t.Errorf("polls = %d, want 1 (no grace for task_wait)", seq.count())
		}
	})
}

func TestWaitForTaskStopsOnCancel(t *testing.T) {
	initial, maxi := taskPollInitial, taskPollMax
	taskPollInitial, taskPollMax = time.Hour, time.Hour // only ctx can end the wait early
	t.Cleanup(func() { taskPollInitial, taskPollMax = initial, maxi })
	client, _ := taskSequenceClient(t, "pending")
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(30*time.Millisecond, cancel)
	start := time.Now()
	_, errRes := waitForTask(ctx, client, testTaskUUID, 10*time.Minute, 0, nil)
	if errRes == nil || !strings.Contains(resultText(t, errRes), "stopped waiting") {
		t.Fatalf("want a stopped-waiting error, got %v", errRes)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("returned %s after cancel, want promptly", elapsed)
	}
}

func TestNextPollInterval(t *testing.T) {
	want := []time.Duration{1500 * time.Millisecond, 2250 * time.Millisecond, 3375 * time.Millisecond, 5 * time.Second, 5 * time.Second}
	cur := time.Second
	for i, w := range want {
		cur = nextPollInterval(cur)
		if cur != w {
			t.Errorf("step %d: interval = %s, want %s", i+1, cur, w)
		}
	}
}

func TestProgressReporterNeedsTokenAndServer(t *testing.T) {
	req := mcp.CallToolRequest{}
	if progressReporter(t.Context(), req, time.Minute) != nil {
		t.Error("no _meta: want no reporter")
	}
	req.Params.Meta = &mcp.Meta{ProgressToken: "p1"}
	if progressReporter(t.Context(), req, time.Minute) != nil {
		t.Error("no server in the context: want no reporter")
	}
}

func TestGetWaitOptions(t *testing.T) {
	for name, tc := range map[string]struct {
		args    map[string]any
		wait    bool
		timeout time.Duration
		wantErr bool
	}{
		"absent":          {map[string]any{}, false, 0, false},
		"wait default":    {map[string]any{"wait": true}, true, defaultTaskWaitTimeout, false},
		"wait timeout":    {map[string]any{"wait": true, "timeout": float64(120)}, true, 120 * time.Second, false},
		"string timeout":  {map[string]any{"wait": true, "timeout": "30"}, true, 30 * time.Second, false},
		"timeout no wait": {map[string]any{"timeout": float64(10)}, false, 0, true},
		"zero":            {map[string]any{"wait": true, "timeout": float64(0)}, false, 0, true},
		"fraction":        {map[string]any{"wait": true, "timeout": 1.5}, false, 0, true},
		"max":             {map[string]any{"wait": true, "timeout": float64(600)}, true, 600 * time.Second, false},
		"negative":        {map[string]any{"wait": true, "timeout": float64(-1)}, false, 0, true},
		"too long":        {map[string]any{"wait": true, "timeout": float64(601)}, false, 0, true},
	} {
		t.Run(name, func(t *testing.T) {
			wait, timeout, errRes := getWaitOptions(makeRequest(tc.args))
			if (errRes != nil) != tc.wantErr {
				t.Fatalf("errRes = %v, wantErr %v", errRes, tc.wantErr)
			}
			if !tc.wantErr && (wait != tc.wait || timeout != tc.timeout) {
				t.Errorf("got (%v, %s), want (%v, %s)", wait, timeout, tc.wait, tc.timeout)
			}
		})
	}
}

func TestTaskWaitByIntegerID(t *testing.T) {
	fastPolling(t)
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tasks/77/", jsonHandler(t, 200, map[string]any{"id": 77, "status": "success"}))
	result := callTool(t, handleTaskWait(testClientAndServer(t, rh)), map[string]any{"id": float64(77)})
	assertNotError(t, result)
	if resultJSON(t, result)["task"].(map[string]any)["status"] != "success" {
		t.Errorf("result = %s", resultText(t, result))
	}
}

func TestTaskWaitRejectsBadInput(t *testing.T) {
	client := testClientAndServer(t, newRouteHandler(t))
	for name, args := range map[string]map[string]any{
		"missing id":  {},
		"bad id":      {"id": "nope"},
		"bad timeout": {"id": testTaskUUID, "timeout": float64(9999)},
	} {
		t.Run(name, func(t *testing.T) {
			assertIsError(t, callTool(t, handleTaskWait(client), args))
		})
	}
}

// bulkWaitServer answers bulk_edit with a task id, and the task lookups with
// the given status sequence.
func bulkWaitServer(t *testing.T, statuses ...string) (*Client, *taskSequence) {
	t.Helper()
	seq := &taskSequence{statuses: statuses}
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/bulk_edit/", jsonHandler(t, 200, map[string]any{"result": "OK", "task_id": testTaskUUID}))
	rh.Handle("GET", "/api/tasks/", seq.handle)
	return testClientAndServer(t, rh), seq
}

func equalAny(got any, want []any) bool {
	b1, _ := json.Marshal(got)
	b2, _ := json.Marshal(want)
	return string(b1) == string(b2)
}

func TestDocumentBulkEditWithoutWaitIsUnchanged(t *testing.T) {
	client, seq := bulkWaitServer(t, "success")
	result := callTool(t, handleDocumentBulkEdit(client), map[string]any{
		"documents": []any{float64(1)}, "method": "add_tag", "parameters": map[string]any{"tag": float64(1)},
	})
	assertNotError(t, result)
	if m := resultJSON(t, result); m["task_id"] != testTaskUUID || m["response"] != nil {
		t.Errorf("result = %v, want the plain bulk_edit response", m)
	}
	if seq.count() != 0 {
		t.Errorf("task polls = %d, want none without wait", seq.count())
	}
}

func TestDocumentBulkEditRejectsTimeoutWithoutWait(t *testing.T) {
	// No routes: the bulk edit must not be sent.
	client := testClientAndServer(t, newRouteHandler(t))
	assertIsError(t, callTool(t, handleDocumentBulkEdit(client), map[string]any{
		"documents": []any{float64(1)}, "method": "add_tag", "parameters": map[string]any{"tag": float64(1)}, "timeout": float64(10),
	}))
}

func TestWaitErrorKeepsResponse(t *testing.T) {
	fastPolling(t)
	client, _ := bulkWaitServer(t, "") // the task never shows up
	result := callTool(t, handleDocumentBulkEdit(client), map[string]any{
		"documents": []any{float64(1)}, "method": "add_tag", "parameters": map[string]any{"tag": float64(1)}, "wait": true,
	})
	// The bulk edit was queued: a wait problem is not a tool error, so an
	// agent isn't invited to resend it.
	assertNotError(t, result)
	m := resultJSON(t, result)
	if m["response"] == nil || m["task_id"] != testTaskUUID || !strings.Contains(m["wait_error"].(string), "no task") || !strings.Contains(m["note"].(string), "Do not resend") {
		t.Errorf("result = %v, want response, task_id, wait_error and a do-not-resend note", m)
	}
}

func TestTaskRunWait(t *testing.T) {
	fastPolling(t)
	seq := &taskSequence{statuses: []string{"started", "success"}}
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/tasks/run/", jsonHandler(t, 200, map[string]any{"task_id": testTaskUUID}))
	rh.Handle("GET", "/api/tasks/", seq.handle)
	result := callTool(t, handleTaskRun(testClientAndServer(t, rh)), map[string]any{"task_type": "sanity_check", "wait": true})
	assertNotError(t, result)
	if resultJSON(t, result)["task"].(map[string]any)["status"] != "success" {
		t.Errorf("result = %s", resultText(t, result))
	}
}

func TestDocumentUploadWaitReadsBareUUID(t *testing.T) {
	fastPolling(t)
	filePath := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(filePath, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	seq := &taskSequence{statuses: []string{"pending", "success"}}
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/post_document/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`"` + testTaskUUID + `"`))
	})
	rh.Handle("GET", "/api/tasks/", seq.handle)
	result := callTool(t, handleDocumentUpload(testClientAndServer(t, rh)), map[string]any{"file_path": filePath, "wait": true})
	assertNotError(t, result)
	m := resultJSON(t, result)
	if m["response"] != testTaskUUID || m["task"].(map[string]any)["status"] != "success" || m["follow_on"] != nil {
		t.Errorf("result = %v, want the upload's UUID as response and the finished task", m)
	}
	if got := seq.queries[0].Get("task_id"); got != testTaskUUID {
		t.Errorf("polled task_id = %q", got)
	}
}

// flakyTasks serves GET /api/tasks/ with a 502 for the first failures calls,
// then a finished task.
func flakyTasks(t *testing.T, failures int) (*Client, *int) {
	t.Helper()
	calls := 0
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tasks/", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= failures {
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte("bad gateway"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(paginatedResponse([]any{map[string]any{"task_id": testTaskUUID, "status": "success"}}, 1))
	})
	return testClientAndServer(t, rh), &calls
}

func TestWaitForTaskToleratesTransientErrors(t *testing.T) {
	fastPolling(t)
	client, calls := flakyTasks(t, maxTransientPollErrors)
	outcome, errRes := waitForTask(t.Context(), client, testTaskUUID, time.Second, 0, nil)
	if errRes != nil || outcome["task"].(map[string]any)["status"] != "success" {
		t.Fatalf("outcome = %v, err = %v; want success after %d transient errors", outcome, errRes, maxTransientPollErrors)
	}
	if *calls != maxTransientPollErrors+1 {
		t.Errorf("polls = %d", *calls)
	}

	client, _ = flakyTasks(t, maxTransientPollErrors+1)
	if _, errRes := waitForTask(t.Context(), client, testTaskUUID, time.Second, 0, nil); errRes == nil || !strings.Contains(resultText(t, errRes), "502") {
		t.Errorf("want the API error after %d failures in a row, got %v", maxTransientPollErrors+1, errRes)
	}
}

func TestWaitForTaskClientErrorEndsAtOnce(t *testing.T) {
	fastPolling(t)
	calls := 0
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tasks/", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"detail":"forbidden"}`))
	})
	if _, errRes := waitForTask(t.Context(), testClientAndServer(t, rh), testTaskUUID, time.Second, 0, nil); errRes == nil {
		t.Fatal("want an error for a 403")
	}
	if calls != 1 {
		t.Errorf("polls = %d, want 1 (a 4xx is not retried)", calls)
	}
}

func TestWaitForTaskPollBoundedByDeadline(t *testing.T) {
	fastPolling(t)
	first := true
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tasks/", func(w http.ResponseWriter, r *http.Request) {
		if !first {
			select { // hang until the client gives up
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		first = false
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(paginatedResponse([]any{map[string]any{"task_id": testTaskUUID, "status": "started"}}, 1))
	})
	start := time.Now()
	outcome, errRes := waitForTask(t.Context(), testClientAndServer(t, rh), testTaskUUID, 200*time.Millisecond, 0, nil)
	if errRes != nil || outcome["timed_out"] != true || outcome["task"].(map[string]any)["status"] != "started" {
		t.Fatalf("outcome = %v, err = %v; want timed_out with the last task seen", outcome, errRes)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("returned after %s, want close to the 200ms timeout", elapsed)
	}
}

func TestWaitForTaskCancelDuringPoll(t *testing.T) {
	fastPolling(t)
	ctx, cancel := context.WithCancel(t.Context())
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tasks/", func(w http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	})
	_, errRes := waitForTask(ctx, testClientAndServer(t, rh), testTaskUUID, time.Minute, 0, nil)
	if errRes == nil || !strings.Contains(resultText(t, errRes), "stopped waiting") {
		t.Fatalf("want a stopped-waiting error, got %v", errRes)
	}
}

func TestWaitForTaskProgressNotAfterDeadline(t *testing.T) {
	fastPolling(t)
	client, _ := taskSequenceClient(t, "pending")
	var reports []time.Duration
	timeout := 60 * time.Millisecond
	outcome, _ := waitForTask(t.Context(), client, testTaskUUID, timeout, 0, func(_ string, elapsed time.Duration) {
		reports = append(reports, elapsed)
	})
	if outcome["timed_out"] != true || len(reports) == 0 {
		t.Fatalf("outcome = %v, reports = %v", outcome, reports)
	}
	for _, r := range reports {
		if r >= timeout {
			t.Errorf("progress reported at %s, at or after the %s deadline", r, timeout)
		}
	}
	if w, _ := outcome["waited_seconds"].(float64); w < 0.05 || w > 1 {
		t.Errorf("waited_seconds = %v, want about the timeout", outcome["waited_seconds"])
	}
}

func TestWaitForTaskGraceLongerThanTimeout(t *testing.T) {
	fastPolling(t)
	client, _ := taskSequenceClient(t, "")
	_, errRes := waitForTask(t.Context(), client, testTaskUUID, 50*time.Millisecond, time.Minute, nil)
	if errRes == nil || !strings.Contains(resultText(t, errRes), "no task with task_id") {
		t.Fatalf("want not found at the deadline, got %v", errRes)
	}
}

func TestTaskWaitIntegerIDNotFound(t *testing.T) {
	fastPolling(t)
	calls := 0
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tasks/99/", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"detail":"No PaperlessTask matches the given query."}`))
	})
	result := callTool(t, handleTaskWait(testClientAndServer(t, rh)), map[string]any{"id": "99"})
	assertIsError(t, result)
	if calls != 1 || !strings.Contains(resultText(t, result), "No PaperlessTask") {
		t.Errorf("calls = %d, result = %s", calls, resultText(t, result))
	}
}

func TestWaitSkippedWithoutTaskID(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/tasks/run/", jsonHandler(t, 200, map[string]any{"result": "OK"}))
	result := callTool(t, handleTaskRun(testClientAndServer(t, rh)), map[string]any{"task_type": "sanity_check", "wait": true})
	assertNotError(t, result)
	if m := resultJSON(t, result); m["wait_skipped"] == nil || m["response"] == nil {
		t.Errorf("result = %v, want response and wait_skipped", m)
	}
}

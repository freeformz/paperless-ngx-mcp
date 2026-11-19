package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestSystemStatus(t *testing.T) {
	status := map[string]any{"pngx_version": "2.0.0", "database_status": "OK"}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/status/", jsonHandler(t, 200, status))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleSimpleGet(client, "/api/status/"), nil)
	assertNotError(t, result)

	m := resultJSON(t, result)
	if m["pngx_version"] != "2.0.0" {
		t.Errorf("version = %v, want 2.0.0", m["pngx_version"])
	}
}

// taskTestServer records the query of every request to path and answers
// with body.
func taskTestServer(t *testing.T, method, path string, body any) (*Client, *[]url.Values) {
	t.Helper()
	var seen []url.Values
	rh := newRouteHandler(t)
	rh.Handle(method, path, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(body)
	})
	return testClientAndServer(t, rh), &seen
}

func TestTaskListForwardsPaginationAndFilters(t *testing.T) {
	client, seen := taskTestServer(t, "GET", "/api/tasks/", paginatedResponse([]any{}, 0))
	result := callTool(t, handleTaskList(client), map[string]any{
		"page":                float64(2),
		"page_size":           float64(10),
		"status":              []any{"failure", "revoked"},
		"task_type":           "consume_file",
		"trigger_source":      []any{"email_consume"},
		"is_complete":         true,
		"acknowledged":        false,
		"owner":               float64(3),
		"name":                "invoice",
		"result":              "duplicate",
		"date_created_after":  "2026-11-01",
		"date_created_before": "2026-11-19T12:00:00Z",
		"ordering":            "-duration_seconds",
	})
	assertNotError(t, result)

	want := url.Values{
		"page": {"2"}, "page_size": {"10"},
		"status": {"failure", "revoked"}, "task_type": {"consume_file"}, "trigger_source": {"email_consume"},
		"is_complete": {"true"}, "acknowledged": {"false"}, "owner": {"3"},
		"name": {"invoice"}, "result": {"duplicate"},
		"date_created_after": {"2026-11-01"}, "date_created_before": {"2026-11-19T12:00:00Z"},
		"ordering": {"-duration_seconds"},
	}
	if len(*seen) != 1 || !reflect.DeepEqual((*seen)[0], want) {
		t.Errorf("query = %v, want %v", *seen, want)
	}
}

func TestTaskListPassesEnvelopeThrough(t *testing.T) {
	tasks := []any{map[string]any{"id": float64(7), "task_id": "abc", "status": "success"}}
	client, _ := taskTestServer(t, "GET", "/api/tasks/", paginatedResponse(tasks, 60))
	result := callTool(t, handleTaskList(client), nil)
	assertNotError(t, result)

	m := resultJSON(t, result)
	if m["count"] != float64(60) || len(m["results"].([]any)) != 1 {
		t.Errorf("response = %v, want the server's envelope unchanged", m)
	}
}

func TestTaskListRejectsInvalidInput(t *testing.T) {
	tests := map[string]map[string]any{
		"unknown status":         {"status": []any{"FAILURE"}},
		"unknown task type":      {"task_type": "check_sanity"},
		"unknown trigger source": {"trigger_source": []any{"auto_task"}},
		"non-string item":        {"status": []any{float64(1)}},
		"unknown ordering":       {"ordering": "-created"},
		"bad ordering prefix":    {"ordering": "+status"},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			// No routes registered: any request the handler makes fails the test.
			client := testClientAndServer(t, newRouteHandler(t))
			assertIsError(t, callTool(t, handleTaskList(client), args))
		})
	}
}

func TestTaskListTruncatesTracebacks(t *testing.T) {
	long := strings.Repeat("x", 3000) + "ValueError: boom"
	short := "short traceback"
	tasks := []any{
		map[string]any{"id": float64(1), "result_data": map[string]any{"traceback": long, "error_message": "boom"}},
		map[string]any{"id": float64(2), "result_data": map[string]any{"traceback": short}},
		map[string]any{"id": float64(3), "result_data": nil},
	}
	client, _ := taskTestServer(t, "GET", "/api/tasks/", paginatedResponse(tasks, 3))

	results := resultJSON(t, callTool(t, handleTaskList(client), nil))["results"].([]any)
	rd := results[0].(map[string]any)["result_data"].(map[string]any)
	tb := rd["traceback"].(string)
	if !strings.HasPrefix(tb, "…") || !strings.HasSuffix(tb, "ValueError: boom") || len(tb) != len("…")+tracebackSnippetLen {
		t.Errorf("traceback = %q (len %d), want … plus the last %d bytes", tb[:min(len(tb), 40)], len(tb), tracebackSnippetLen)
	}
	if rd["traceback_truncated"] != true {
		t.Error("traceback_truncated should be true")
	}
	if rd["error_message"] != "boom" {
		t.Error("other result_data fields should be kept")
	}
	rd2 := results[1].(map[string]any)["result_data"].(map[string]any)
	if rd2["traceback"] != short || rd2["traceback_truncated"] != nil {
		t.Errorf("short traceback changed: %v", rd2)
	}

	full := resultJSON(t, callTool(t, handleTaskList(client), map[string]any{"include_traceback": true}))["results"].([]any)
	if got := full[0].(map[string]any)["result_data"].(map[string]any)["traceback"]; got != long {
		t.Error("include_traceback should keep the traceback whole")
	}
}

func TestLogList(t *testing.T) {
	logs := []string{"paperless", "mail"}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/logs/", jsonHandler(t, 200, logs))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleSimpleGet(client, "/api/logs/"), nil)
	assertNotError(t, result)
}

func TestTrashList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/trash/", jsonHandler(t, 200, paginatedResponse([]any{}, 0)))

	client := testClientAndServer(t, rh)
	result := callTool(t, handlePaginatedList(client, "/api/trash/"), nil)
	assertNotError(t, result)
}

func TestTrashAction(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/trash/", jsonHandler(t, 200, map[string]any{"result": "ok"}))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleTrashAction(client), map[string]any{
		"action":    "restore",
		"documents": "[1, 2]",
	})
	assertNotError(t, result)
}

func TestTrashActionRequiresAction(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleTrashAction(client), map[string]any{})
	assertIsError(t, result)
}

func TestTaskGetByUUIDUnwrapsEnvelope(t *testing.T) {
	taskID := "12345678-1234-1234-1234-123456789ABC"
	task := map[string]any{"id": float64(9), "task_id": taskID, "status": "success"}
	client, seen := taskTestServer(t, "GET", "/api/tasks/", paginatedResponse([]any{task}, 1))
	result := callTool(t, handleTaskGet(client), map[string]any{"id": taskID})
	assertNotError(t, result)

	if got := (*seen)[0].Get("task_id"); got != taskID {
		t.Errorf("task_id query param = %q, want %q", got, taskID)
	}
	m := resultJSON(t, result)
	if m["id"] != float64(9) || m["task_id"] != taskID {
		t.Errorf("result = %v, want the task object", m)
	}
}

func TestTaskGetByUUIDNotFound(t *testing.T) {
	client, _ := taskTestServer(t, "GET", "/api/tasks/", paginatedResponse([]any{}, 0))
	result := callTool(t, handleTaskGet(client), map[string]any{"id": "12345678-1234-1234-1234-123456789012"})
	assertIsError(t, result)
	if !strings.Contains(resultText(t, result), "no task with task_id") {
		t.Errorf("error = %s, want a not-found message", resultText(t, result))
	}
}

func TestTaskGetByIntegerID(t *testing.T) {
	for name, id := range map[string]any{"string": "7845", "number": float64(7845)} {
		t.Run(name, func(t *testing.T) {
			client, seen := taskTestServer(t, "GET", "/api/tasks/7845/", map[string]any{"id": float64(7845)})
			result := callTool(t, handleTaskGet(client), map[string]any{"id": id})
			assertNotError(t, result)
			if len(*seen) != 1 {
				t.Fatalf("requests = %d, want 1", len(*seen))
			}
			if resultJSON(t, result)["id"] != float64(7845) {
				t.Error("want the task object passed through")
			}
		})
	}
}

func TestTaskGetRejectsInvalidID(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"missing":  {},
		"not uuid": {"id": "abc-123"},
		"path":     {"id": "../users"},
		"negative": {"id": float64(-1)},
		"fraction": {"id": float64(1.5)},
		"signed":   {"id": "-5"},
		"empty":    {"id": ""},
	} {
		t.Run(name, func(t *testing.T) {
			client := testClientAndServer(t, newRouteHandler(t))
			assertIsError(t, callTool(t, handleTaskGet(client), args))
		})
	}
}

func TestTaskAcknowledge(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		want map[string]any
	}{
		{"tasks", map[string]any{"tasks": "[12, 34]"}, map[string]any{"tasks": []any{float64(12), float64(34)}}},
		{"all", map[string]any{"all": true}, map[string]any{"all": true}},
		{"all false with tasks", map[string]any{"tasks": "[5]", "all": false}, map[string]any{"tasks": []any{float64(5)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[string]any
			rh := newRouteHandler(t)
			rh.Handle("POST", "/api/tasks/acknowledge/", func(w http.ResponseWriter, r *http.Request) {
				json.NewDecoder(r.Body).Decode(&got)
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"result": 2}`))
			})
			client := testClientAndServer(t, rh)
			assertNotError(t, callTool(t, handleTaskAcknowledge(client), tt.args))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("body = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTaskAcknowledgeRequiresExactlyOne(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"neither":   {},
		"all false": {"all": false},
		"both":      {"tasks": "[1]", "all": true},
	} {
		t.Run(name, func(t *testing.T) {
			client := testClientAndServer(t, newRouteHandler(t))
			assertIsError(t, callTool(t, handleTaskAcknowledge(client), args))
		})
	}
}

func TestTaskStats(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		path string
		want url.Values
	}{
		{"summary default", map[string]any{"mode": "summary"}, "/api/tasks/summary/", url.Values{}},
		{"summary days", map[string]any{"mode": "summary", "days": float64(7)}, "/api/tasks/summary/", url.Values{"days": {"7"}}},
		{"status_counts filtered", map[string]any{"mode": "status_counts", "task_type": []any{"mail_fetch"}, "acknowledged": false},
			"/api/tasks/status_counts/", url.Values{"task_type": {"mail_fetch"}, "acknowledged": {"false"}}},
		{"active", map[string]any{"mode": "active"}, "/api/tasks/active/", url.Values{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, seen := taskTestServer(t, "GET", tt.path, map[string]any{})
			assertNotError(t, callTool(t, handleTaskStats(client), tt.args))
			if len(*seen) != 1 || !reflect.DeepEqual((*seen)[0], tt.want) {
				t.Errorf("query = %v, want %v", *seen, tt.want)
			}
		})
	}
}

func TestTaskStatsRejectsInvalidInput(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"missing mode":          {},
		"unknown mode":          {"mode": "counts"},
		"days with active":      {"mode": "active", "days": float64(7)},
		"filter with summary":   {"mode": "summary", "task_type": []any{"mail_fetch"}},
		"status in counts":      {"mode": "status_counts", "status": []any{"failure"}},
		"is_complete in counts": {"mode": "status_counts", "is_complete": true},
		"days zero":             {"mode": "summary", "days": float64(0)},
		"days too large":        {"mode": "summary", "days": float64(366)},
		"days fraction":         {"mode": "summary", "days": float64(1.5)},
		"bad enum in counts":    {"mode": "status_counts", "trigger_source": []any{"cron"}},
	} {
		t.Run(name, func(t *testing.T) {
			client := testClientAndServer(t, newRouteHandler(t))
			assertIsError(t, callTool(t, handleTaskStats(client), args))
		})
	}
}

func TestTaskRun(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/tasks/run/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		if len(got) != 1 || got["task_type"] != "train_classifier" {
			t.Errorf("request body = %s, want {\"task_type\":\"train_classifier\"}", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"task_id":"new-123"}`))
	})
	client := testClientAndServer(t, rh)
	result := callTool(t, handleTaskRun(client), map[string]any{"task_type": "train_classifier"})
	assertNotError(t, result)
}

func TestTaskRunRequiresType(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleTaskRun(client), map[string]any{})
	assertIsError(t, result)
}

func TestTaskRunRejectsUnknownType(t *testing.T) {
	// No routes registered: any request the handler makes fails the test.
	client := testClientAndServer(t, newRouteHandler(t))
	result := callTool(t, handleTaskRun(client), map[string]any{"task_type": "index_optimize"})
	assertIsError(t, result)
}

func TestLogGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/logs/paperless/", jsonHandler(t, 200, []string{"line 1", "line 2"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleLogGet(client), map[string]any{"id": "paperless"})
	assertNotError(t, result)

	m := resultJSON(t, result)
	if m["total_lines"] != float64(2) {
		t.Errorf("total_lines = %v, want 2", m["total_lines"])
	}
	if len(m["lines"].([]any)) != 2 {
		t.Errorf("lines length = %d, want 2", len(m["lines"].([]any)))
	}
}

func TestLogGetTail(t *testing.T) {
	lines := make([]string, 250)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/logs/celery/", jsonHandler(t, 200, lines))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleLogGet(client), map[string]any{"id": "celery", "tail": float64(10)})
	assertNotError(t, result)

	m := resultJSON(t, result)
	if m["total_lines"] != float64(250) {
		t.Errorf("total_lines = %v, want 250", m["total_lines"])
	}
	got := m["lines"].([]any)
	if len(got) != 10 {
		t.Errorf("lines length = %d, want 10", len(got))
	}
	if got[0] != "line 240" {
		t.Errorf("first line = %v, want line 240", got[0])
	}
	if got[9] != "line 249" {
		t.Errorf("last line = %v, want line 249", got[9])
	}
}

func TestLogGetDefaultTail(t *testing.T) {
	lines := make([]string, 250)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/logs/celery/", jsonHandler(t, 200, lines))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleLogGet(client), map[string]any{"id": "celery"})
	assertNotError(t, result)

	m := resultJSON(t, result)
	if len(m["lines"].([]any)) != 100 {
		t.Errorf("lines length = %d, want default tail of 100", len(m["lines"].([]any)))
	}
}

func TestLogGetRequiresId(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleLogGet(client), map[string]any{})
	assertIsError(t, result)
}

func TestLogGetRejectsInvalidId(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleLogGet(client), map[string]any{"id": "../etc/passwd"})
	assertIsError(t, result)
}

func TestLogGetRejectsDotDot(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleLogGet(client), map[string]any{"id": ".."})
	assertIsError(t, result)
}

func TestRemoteVersion(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/remote_version/", jsonHandler(t, 200, map[string]any{"version": "2.1.0"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleSimpleGet(client, "/api/remote_version/"), nil)
	assertNotError(t, result)
}

func TestUISettingsGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/ui_settings/", jsonHandler(t, 200, map[string]any{"theme": "dark"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleSimpleGet(client, "/api/ui_settings/"), nil)
	assertNotError(t, result)
}

func TestConfigList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/config/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleSimpleGet(client, "/api/config/"), nil)
	assertNotError(t, result)
}

func TestConfigGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/config/1/", jsonHandler(t, 200, map[string]any{"id": 1, "key": "value"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/config/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestConfigUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/config/1/", jsonHandler(t, 200, map[string]any{"id": 1}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleConfigUpdate(client), map[string]any{
		"id":   float64(1),
		"body": `{"key": "new_value"}`,
	})
	assertNotError(t, result)
}

func TestConfigUpdateNoFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleConfigUpdate(client), map[string]any{"id": float64(1)})
	assertIsError(t, result)
}

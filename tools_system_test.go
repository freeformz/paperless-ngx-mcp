package main

import (
	"encoding/json"
	"fmt"
	"net/http"
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

func TestTaskList(t *testing.T) {
	tasks := []map[string]any{{"task_id": "abc-123", "status": "SUCCESS"}}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tasks/", jsonHandler(t, 200, tasks))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleTaskList(client), nil)
	assertNotError(t, result)

	m := resultJSON(t, result)
	if m["count"] != float64(1) {
		t.Errorf("count = %v, want 1", m["count"])
	}
	if len(m["results"].([]any)) != 1 {
		t.Errorf("results length = %d, want 1", len(m["results"].([]any)))
	}
}

func TestTaskListPaginatesClientSide(t *testing.T) {
	// The tasks endpoint ignores pagination and returns a bare array of all
	// tasks; the handler must slice it client-side.
	tasks := make([]map[string]any, 60)
	for i := range tasks {
		tasks[i] = map[string]any{"id": i, "status": "SUCCESS"}
	}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tasks/", jsonHandler(t, 200, tasks))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleTaskList(client), map[string]any{"page": float64(2), "page_size": float64(25)})
	assertNotError(t, result)

	m := resultJSON(t, result)
	if m["count"] != float64(60) {
		t.Errorf("count = %v, want 60", m["count"])
	}
	results := m["results"].([]any)
	if len(results) != 25 {
		t.Errorf("results length = %d, want 25", len(results))
	}
	if first := results[0].(map[string]any)["id"]; first != float64(25) {
		t.Errorf("first result id = %v, want 25", first)
	}
	if m["next_page"] != float64(3) {
		t.Errorf("next_page = %v, want 3", m["next_page"])
	}
}

func TestTaskListHugePaginationDoesNotPanic(t *testing.T) {
	tasks := []map[string]any{{"id": 0}, {"id": 1}}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tasks/", jsonHandler(t, 200, tasks))

	client := testClientAndServer(t, rh)
	// Values large enough that (page-1)*pageSize overflows int.
	result := callTool(t, handleTaskList(client), map[string]any{
		"page":      float64(1e15),
		"page_size": float64(1e15),
	})
	assertNotError(t, result)

	m := resultJSON(t, result)
	if len(m["results"].([]any)) != 0 {
		t.Errorf("results length = %d, want 0 for a page past the end", len(m["results"].([]any)))
	}
	if m["count"] != float64(2) {
		t.Errorf("count = %v, want 2", m["count"])
	}
}

func TestTaskListLastPageHasNoNextPage(t *testing.T) {
	tasks := []map[string]any{{"id": 0}, {"id": 1}, {"id": 2}}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tasks/", jsonHandler(t, 200, tasks))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleTaskList(client), map[string]any{"page": float64(1), "page_size": float64(5)})
	assertNotError(t, result)

	m := resultJSON(t, result)
	if _, ok := m["next_page"]; ok {
		t.Error("next_page should be absent on the last page")
	}
	if len(m["results"].([]any)) != 3 {
		t.Errorf("results length = %d, want 3", len(m["results"].([]any)))
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

func TestTaskGet(t *testing.T) {
	taskID := "12345678-1234-1234-1234-123456789012"
	var capturedTaskID string
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tasks/", func(w http.ResponseWriter, r *http.Request) {
		capturedTaskID = r.URL.Query().Get("task_id")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{{"task_id": taskID, "status": "SUCCESS"}})
	})
	client := testClientAndServer(t, rh)
	result := callTool(t, handleTaskGet(client), map[string]any{"id": taskID})
	assertNotError(t, result)

	if capturedTaskID != taskID {
		t.Errorf("task_id query param = %q, want %q", capturedTaskID, taskID)
	}
}

func TestTaskGetRequiresId(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleTaskGet(client), map[string]any{})
	assertIsError(t, result)
}

func TestTaskGetAcceptsUppercaseUUID(t *testing.T) {
	taskID := "12345678-1234-1234-1234-123456789ABC"
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tasks/", jsonHandler(t, 200, []map[string]any{{"task_id": taskID, "status": "SUCCESS"}}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleTaskGet(client), map[string]any{"id": taskID})
	assertNotError(t, result)
}

func TestTaskGetRejectsInvalidId(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleTaskGet(client), map[string]any{"id": "abc-123"})
	assertIsError(t, result)
}

func TestTaskAcknowledge(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/tasks/acknowledge/", jsonHandler(t, 200, map[string]any{"result": "ok"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleTaskAcknowledge(client), map[string]any{"tasks": `[12, 34]`})
	assertNotError(t, result)
}

func TestTaskAcknowledgeRequiresTasks(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleTaskAcknowledge(client), map[string]any{})
	assertIsError(t, result)
}

func TestTaskRun(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/tasks/run/", jsonHandler(t, 200, map[string]any{"task_id": "new-123"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleTaskRun(client), map[string]any{"task_name": "index_optimize"})
	assertNotError(t, result)
}

func TestTaskRunRequiresName(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleTaskRun(client), map[string]any{})
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

package main

import (
	"testing"
)

func TestSystemStatus(t *testing.T) {
	status := map[string]any{"pngx_version": "2.0.0", "database_status": "OK"}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/status/", jsonHandler(t, 200, status))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleSystemStatus(client), nil)
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
}

func TestLogList(t *testing.T) {
	logs := []string{"paperless", "mail"}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/logs/", jsonHandler(t, 200, logs))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleLogList(client), nil)
	assertNotError(t, result)
}

func TestTrashList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/trash/", jsonHandler(t, 200, paginatedResponse([]any{}, 0)))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleTrashList(client), nil)
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
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tasks/abc-123/", jsonHandler(t, 200, map[string]any{"task_id": "abc-123", "status": "SUCCESS"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleTaskGet(client), map[string]any{"id": "abc-123"})
	assertNotError(t, result)
}

func TestTaskGetRequiresId(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleTaskGet(client), map[string]any{})
	assertIsError(t, result)
}

func TestTaskAcknowledge(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/tasks/acknowledge/", jsonHandler(t, 200, map[string]any{"result": "ok"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleTaskAcknowledge(client), map[string]any{"tasks": `["abc-123"]`})
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
	result := callTool(t, handleTaskRun(client), map[string]any{"task_name": "documents.tasks.index_reindex"})
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
}

func TestLogGetRequiresId(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleLogGet(client), map[string]any{})
	assertIsError(t, result)
}

func TestRemoteVersion(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/remote_version/", jsonHandler(t, 200, map[string]any{"version": "2.1.0"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleRemoteVersion(client), nil)
	assertNotError(t, result)
}

func TestUISettingsGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/ui_settings/", jsonHandler(t, 200, map[string]any{"theme": "dark"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleUISettingsGet(client), nil)
	assertNotError(t, result)
}

func TestConfigList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/config/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleConfigList(client), nil)
	assertNotError(t, result)
}

func TestConfigGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/config/1/", jsonHandler(t, 200, map[string]any{"id": 1, "key": "value"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleConfigGet(client), map[string]any{"id": float64(1)})
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


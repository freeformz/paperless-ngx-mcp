package main

import (
	"net/http"
	"testing"
)

func TestWorkflowList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/workflows/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1, "name": "Auto-tag"}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handlePaginatedList(client, "/api/workflows/"), nil)
	assertNotError(t, result)
}

func TestWorkflowGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/workflows/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Auto-tag"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/workflows/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestWorkflowCreate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/workflows/", jsonHandler(t, 201, map[string]any{"id": 1, "name": "Auto-tag"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleWorkflowCreate(client), map[string]any{
		"name": "Auto-tag",
		"body": `{"enabled": true}`,
	})
	assertNotError(t, result)
}

func TestWorkflowCreateRequiresName(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleWorkflowCreate(client), map[string]any{"body": `{"enabled": true}`})
	assertIsError(t, result)
}

func TestWorkflowUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/workflows/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Updated"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleWorkflowUpdate(client), map[string]any{
		"id":   float64(1),
		"body": `{"name": "Updated"}`,
	})
	assertNotError(t, result)
}

func TestWorkflowUpdateNoFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleWorkflowUpdate(client), map[string]any{"id": float64(1)})
	assertIsError(t, result)
}

func TestWorkflowDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/workflows/1/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDeleteByID(client, "/api/workflows/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestWorkflowTriggerList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/workflow_triggers/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handlePaginatedList(client, "/api/workflow_triggers/"), nil)
	assertNotError(t, result)
}

func TestWorkflowTriggerGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/workflow_triggers/1/", jsonHandler(t, 200, map[string]any{"id": 1, "type": 1}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/workflow_triggers/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestWorkflowTriggerCreate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/workflow_triggers/", jsonHandler(t, 201, map[string]any{"id": 1}))
	client := testClientAndServer(t, rh)
	result := callTool(t, genericJSONCreate(client, "/api/workflow_triggers/", nil), map[string]any{
		"body": `{"type": 1, "sources": [1]}`,
	})
	assertNotError(t, result)
}

func TestWorkflowTriggerUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/workflow_triggers/1/", jsonHandler(t, 200, map[string]any{"id": 1}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGenericJSONUpdateByID(client, "/api/workflow_triggers/%d/"), map[string]any{
		"id":   float64(1),
		"body": `{"type": 2}`,
	})
	assertNotError(t, result)
}

func TestWorkflowTriggerUpdateNoFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleGenericJSONUpdateByID(client, "/api/workflow_triggers/%d/"), map[string]any{"id": float64(1)})
	assertIsError(t, result)
}

func TestWorkflowTriggerDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/workflow_triggers/1/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDeleteByID(client, "/api/workflow_triggers/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestWorkflowActionList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/workflow_actions/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handlePaginatedList(client, "/api/workflow_actions/"), nil)
	assertNotError(t, result)
}

func TestWorkflowActionGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/workflow_actions/1/", jsonHandler(t, 200, map[string]any{"id": 1, "type": 1}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/workflow_actions/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestWorkflowActionCreate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/workflow_actions/", jsonHandler(t, 201, map[string]any{"id": 1}))
	client := testClientAndServer(t, rh)
	result := callTool(t, genericJSONCreate(client, "/api/workflow_actions/", nil), map[string]any{
		"body": `{"type": 1, "assign_tags": [1, 2]}`,
	})
	assertNotError(t, result)
}

func TestWorkflowActionUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/workflow_actions/1/", jsonHandler(t, 200, map[string]any{"id": 1}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGenericJSONUpdateByID(client, "/api/workflow_actions/%d/"), map[string]any{
		"id":   float64(1),
		"body": `{"type": 2}`,
	})
	assertNotError(t, result)
}

func TestWorkflowActionUpdateNoFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleGenericJSONUpdateByID(client, "/api/workflow_actions/%d/"), map[string]any{"id": float64(1)})
	assertIsError(t, result)
}

func TestWorkflowActionDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/workflow_actions/1/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDeleteByID(client, "/api/workflow_actions/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

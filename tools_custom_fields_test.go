package main

import (
	"net/http"
	"testing"
)

func TestCustomFieldList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/custom_fields/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1, "name": "Due Date"}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleCustomFieldList(client), nil)
	assertNotError(t, result)
}

func TestCustomFieldGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/custom_fields/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Due Date", "data_type": "date"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/custom_fields/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestCustomFieldCreate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/custom_fields/", jsonHandler(t, 201, map[string]any{"id": 1, "name": "Amount", "data_type": "monetary"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleCustomFieldCreate(client), map[string]any{"name": "Amount", "data_type": "monetary"})
	assertNotError(t, result)
}

func TestCustomFieldCreateRequiresFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleCustomFieldCreate(client), map[string]any{})
	assertIsError(t, result)
	result = callTool(t, handleCustomFieldCreate(client), map[string]any{"name": "test"})
	assertIsError(t, result)
}

func TestCustomFieldUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/custom_fields/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Updated"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleCustomFieldUpdate(client), map[string]any{"id": float64(1), "name": "Updated"})
	assertNotError(t, result)
}

func TestCustomFieldDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/custom_fields/1/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDeleteByID(client, "/api/custom_fields/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

package main

import (
	"net/http"
	"testing"
)

func TestSavedViewList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/saved_views/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1, "name": "Inbox"}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handlePaginatedList(client, "/api/saved_views/"), nil)
	assertNotError(t, result)
}

func TestSavedViewGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/saved_views/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Inbox"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/saved_views/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestSavedViewCreate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/saved_views/", jsonHandler(t, 201, map[string]any{"id": 1, "name": "Inbox"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleSavedViewCreate(client), map[string]any{"name": "Inbox"})
	assertNotError(t, result)
}

func TestSavedViewCreateRequiresName(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleSavedViewCreate(client), map[string]any{})
	assertIsError(t, result)
}

func TestSavedViewUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/saved_views/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Updated"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleSavedViewUpdate(client), map[string]any{"id": float64(1), "name": "Updated"})
	assertNotError(t, result)
}

func TestSavedViewUpdateNoFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleSavedViewUpdate(client), map[string]any{"id": float64(1)})
	assertIsError(t, result)
}

func TestSavedViewDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/saved_views/1/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDeleteByID(client, "/api/saved_views/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

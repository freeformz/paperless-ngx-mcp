package main

import (
	"net/http"
	"testing"
)

func TestStoragePathList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/storage_paths/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1, "name": "Default"}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleStoragePathList(client), nil)
	assertNotError(t, result)
}

func TestStoragePathGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/storage_paths/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Default"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleStoragePathGet(client), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestStoragePathCreate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/storage_paths/", jsonHandler(t, 201, map[string]any{"id": 1, "name": "Archive", "path": "{created_year}/"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleStoragePathCreate(client), map[string]any{"name": "Archive", "path": "{created_year}/"})
	assertNotError(t, result)
}

func TestStoragePathCreateRequiresFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleStoragePathCreate(client), map[string]any{})
	assertIsError(t, result)
	result = callTool(t, handleStoragePathCreate(client), map[string]any{"name": "test"})
	assertIsError(t, result)
}

func TestStoragePathUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/storage_paths/1/", jsonHandler(t, 200, map[string]any{"id": 1}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleStoragePathUpdate(client), map[string]any{"id": float64(1), "name": "Updated"})
	assertNotError(t, result)
}

func TestStoragePathDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/storage_paths/1/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	client := testClientAndServer(t, rh)
	result := callTool(t, handleStoragePathDelete(client), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestStoragePathTest(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/storage_paths/test/", jsonHandler(t, 200, map[string]any{"path": "2024/invoice.pdf"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleStoragePathTest(client), map[string]any{"path": "{created_year}/{title}.pdf"})
	assertNotError(t, result)
}

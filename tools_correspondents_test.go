package main

import (
	"net/http"
	"testing"
)

func TestCorrespondentList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/correspondents/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1, "name": "Acme"}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleCorrespondentList(client), nil)
	assertNotError(t, result)
}

func TestCorrespondentGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/correspondents/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Acme"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/correspondents/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestCorrespondentCreate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/correspondents/", jsonHandler(t, 201, map[string]any{"id": 1, "name": "New"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleCorrespondentCreate(client), map[string]any{"name": "New"})
	assertNotError(t, result)
}

func TestCorrespondentCreateRequiresName(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleCorrespondentCreate(client), map[string]any{})
	assertIsError(t, result)
}

func TestCorrespondentUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/correspondents/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Updated"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleCorrespondentUpdate(client), map[string]any{"id": float64(1), "name": "Updated"})
	assertNotError(t, result)
}

func TestCorrespondentDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/correspondents/1/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDeleteByID(client, "/api/correspondents/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

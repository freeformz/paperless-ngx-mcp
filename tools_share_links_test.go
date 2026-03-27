package main

import (
	"net/http"
	"testing"
)

func TestShareLinkList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/share_links/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handlePaginatedList(client, "/api/share_links/"), nil)
	assertNotError(t, result)
}

func TestShareLinkGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/share_links/1/", jsonHandler(t, 200, map[string]any{"id": 1, "slug": "abc"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/share_links/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestShareLinkCreate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/share_links/", jsonHandler(t, 201, map[string]any{"id": 1, "document": 5}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleShareLinkCreate(client), map[string]any{"document": float64(5)})
	assertNotError(t, result)
}

func TestShareLinkCreateRequiresDocument(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleShareLinkCreate(client), map[string]any{})
	assertIsError(t, result)
}

func TestShareLinkUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/share_links/1/", jsonHandler(t, 200, map[string]any{"id": 1}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleShareLinkUpdate(client), map[string]any{"id": float64(1), "expiration": "2025-12-31"})
	assertNotError(t, result)
}

func TestShareLinkUpdateNoFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleShareLinkUpdate(client), map[string]any{"id": float64(1)})
	assertIsError(t, result)
}

func TestShareLinkDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/share_links/1/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDeleteByID(client, "/api/share_links/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

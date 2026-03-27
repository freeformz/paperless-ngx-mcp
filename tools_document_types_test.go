package main

import (
	"net/http"
	"testing"
)

func TestDocumentTypeList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/document_types/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1, "name": "Invoice"}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentTypeList(client), nil)
	assertNotError(t, result)
}

func TestDocumentTypeGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/document_types/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Invoice"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentTypeGet(client), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestDocumentTypeCreate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/document_types/", jsonHandler(t, 201, map[string]any{"id": 1, "name": "Receipt"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentTypeCreate(client), map[string]any{"name": "Receipt"})
	assertNotError(t, result)
}

func TestDocumentTypeCreateRequiresName(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentTypeCreate(client), map[string]any{})
	assertIsError(t, result)
}

func TestDocumentTypeUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/document_types/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Updated"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentTypeUpdate(client), map[string]any{"id": float64(1), "name": "Updated"})
	assertNotError(t, result)
}

func TestDocumentTypeDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/document_types/1/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentTypeDelete(client), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

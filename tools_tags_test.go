package main

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestTagList(t *testing.T) {
	tags := paginatedResponse([]map[string]any{
		{"id": 1, "name": "Invoice", "color": "#ff0000"},
		{"id": 2, "name": "Receipt", "color": "#00ff00"},
	}, 2)

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tags/", jsonHandler(t, 200, tags))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleTagList(client), nil)
	assertNotError(t, result)

	m := resultJSON(t, result)
	if m["count"] != float64(2) {
		t.Errorf("count = %v, want 2", m["count"])
	}
}

func TestTagGet(t *testing.T) {
	tag := map[string]any{"id": float64(1), "name": "Invoice"}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tags/1/", jsonHandler(t, 200, tag))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleTagGet(client), map[string]any{"id": float64(1)})
	assertNotError(t, result)

	m := resultJSON(t, result)
	if m["name"] != "Invoice" {
		t.Errorf("name = %v, want Invoice", m["name"])
	}
}

func TestTagCreate(t *testing.T) {
	var capturedBody map[string]any

	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		w.Write([]byte(`{"id":3,"name":"New Tag","color":"#0000ff"}`))
	})

	client := testClientAndServer(t, rh)
	result := callTool(t, handleTagCreate(client), map[string]any{
		"name":  "New Tag",
		"color": "#0000ff",
	})
	assertNotError(t, result)

	if capturedBody["name"] != "New Tag" {
		t.Errorf("name = %v, want New Tag", capturedBody["name"])
	}
	if capturedBody["color"] != "#0000ff" {
		t.Errorf("color = %v, want #0000ff", capturedBody["color"])
	}
}

func TestTagCreateRequiresName(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleTagCreate(client), map[string]any{})
	assertIsError(t, result)
}

func TestTagUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/tags/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Updated"}))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleTagUpdate(client), map[string]any{
		"id":   float64(1),
		"name": "Updated",
	})
	assertNotError(t, result)
}

func TestTagUpdateNoFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleTagUpdate(client), map[string]any{"id": float64(1)})
	assertIsError(t, result)
}

func TestTagDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/tags/1/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	client := testClientAndServer(t, rh)
	result := callTool(t, handleTagDelete(client), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestTagListWithNameFilter(t *testing.T) {
	var capturedName string
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		capturedName = r.URL.Query().Get("name__icontains")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(paginatedResponse([]any{}, 0))
	})

	client := testClientAndServer(t, rh)
	callTool(t, handleTagList(client), map[string]any{"name": "invoice"})

	if capturedName != "invoice" {
		t.Errorf("name filter = %q, want invoice", capturedName)
	}
}

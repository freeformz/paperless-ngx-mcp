package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/mark3labs/mcp-go/server"
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
	result := callTool(t, handleGetByID(client, "/api/tags/%d/"), map[string]any{"id": float64(1)})
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
	result := callTool(t, handleDeleteByID(client, "/api/tags/%d/"), map[string]any{"id": float64(1)})
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

// tag_create used to send parent: 0 for a null parent, which Paperless
// rejects with "Invalid pk 0"; it now shares tag_update's nullable handling.
func TestTagCreateParent(t *testing.T) {
	tests := []struct {
		name    string
		parent  any
		want    any
		wantErr bool
	}{
		{"json null", nil, nil, false},
		{"string null", "null", nil, false},
		{"number", float64(4), float64(4), false},
		{"integer string", "4", float64(4), false},
		{"fraction", 4.5, nil, true},
		{"empty string", "", nil, true},
		{"bool", true, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body map[string]any
			rh := newRouteHandler(t)
			if !tt.wantErr {
				rh.Handle("POST", "/api/tags/", captureJSONBody(t, &body))
			}
			client := testClientAndServer(t, rh)
			result := callTool(t, handleTagCreate(client), map[string]any{"name": "x", "parent": tt.parent})
			if tt.wantErr {
				assertIsError(t, result)
				return
			}
			assertNotError(t, result)
			if v, ok := body["parent"]; !ok || v != tt.want {
				t.Errorf("parent = %#v (present %v), want %#v", v, ok, tt.want)
			}
		})
	}
}

func TestTagCreateParentSchemaNullable(t *testing.T) {
	srv := server.NewMCPServer("test", "1")
	registerTagTools(srv, NewClient("http://unused", "unused"))
	tool := srv.GetTool("tag_create")
	if tool == nil {
		t.Fatal("tag_create not registered")
	}
	prop := tool.Tool.InputSchema.Properties["parent"].(map[string]any)
	if prop["pattern"] != `^(-?\d+|null)$` {
		t.Errorf("parent pattern = %v, want nullable pattern", prop["pattern"])
	}
}

func TestTagCreateOmitsOwnerByDefault(t *testing.T) {
	var body map[string]any
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/tags/", captureJSONBody(t, &body))

	client := testClientAndServer(t, rh)
	assertNotError(t, callTool(t, handleTagCreate(client), map[string]any{"name": "x"}))

	if _, ok := body["owner"]; ok {
		t.Error("owner must be omitted when not given")
	}
}

func TestTagCreateRejectsNonObjectPermissions(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleTagCreate(client), map[string]any{
		"name":            "x",
		"set_permissions": `[2]`,
	})
	assertIsError(t, result)
}

// full_perms: false must not be sent: Paperless-ngx 2.16 reads the string
// "false" as true, and a query parameter would bypass the list cache.
func TestTagFullPermsFalseOmitted(t *testing.T) {
	var listRequests int
	var listQuery, getQuery url.Values
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		listRequests++
		listQuery = r.URL.Query()
		jsonHandler(t, 200, paginatedResponse([]any{}, 0))(w, r)
	})
	rh.Handle("GET", "/api/tags/1/", func(w http.ResponseWriter, r *http.Request) {
		getQuery = r.URL.Query()
		jsonHandler(t, 200, map[string]any{"id": 1})(w, r)
	})

	client := testClientAndServer(t, rh)
	for range 2 {
		assertNotError(t, callTool(t, handleTagList(client), map[string]any{"full_perms": false}))
	}
	assertNotError(t, callTool(t, handleGetByID(client, "/api/tags/%d/", addFullPerms), map[string]any{"id": float64(1), "full_perms": false}))

	if listQuery.Has("full_perms") || getQuery.Has("full_perms") {
		t.Errorf("full_perms sent: list=%v get=%v", listQuery, getQuery)
	}
	if listRequests != 1 {
		t.Errorf("list requests = %d, want 1 (second call served from cache)", listRequests)
	}
}

func TestTagFullPerms(t *testing.T) {
	var listQuery, getQuery string
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		listQuery = r.URL.Query().Get("full_perms")
		jsonHandler(t, 200, paginatedResponse([]any{}, 0))(w, r)
	})
	rh.Handle("GET", "/api/tags/1/", func(w http.ResponseWriter, r *http.Request) {
		getQuery = r.URL.Query().Get("full_perms")
		jsonHandler(t, 200, map[string]any{"id": 1})(w, r)
	})

	client := testClientAndServer(t, rh)
	assertNotError(t, callTool(t, handleTagList(client), map[string]any{"full_perms": true}))
	assertNotError(t, callTool(t, handleGetByID(client, "/api/tags/%d/", addFullPerms), map[string]any{"id": float64(1), "full_perms": true}))

	if listQuery != "true" || getQuery != "true" {
		t.Errorf("full_perms list=%q get=%q, want true/true", listQuery, getQuery)
	}
}

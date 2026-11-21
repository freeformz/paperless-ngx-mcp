package main

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

func TestMatchingAlgorithmNamesOnMetadataTools(t *testing.T) {
	type tool struct {
		name, method, path string
		handler            func(*Client) server.ToolHandlerFunc
		args               map[string]any
	}
	tools := []tool{
		{"tag_create", "POST", "/api/tags/", handleTagCreate, map[string]any{"name": "t"}},
		{"tag_update", "PATCH", "/api/tags/1/", handleTagUpdate, map[string]any{"id": float64(1)}},
		{"correspondent_create", "POST", "/api/correspondents/", handleCorrespondentCreate, map[string]any{"name": "c"}},
		{"correspondent_update", "PATCH", "/api/correspondents/1/", handleCorrespondentUpdate, map[string]any{"id": float64(1)}},
		{"document_type_create", "POST", "/api/document_types/", handleDocumentTypeCreate, map[string]any{"name": "d"}},
		{"document_type_update", "PATCH", "/api/document_types/1/", handleDocumentTypeUpdate, map[string]any{"id": float64(1)}},
		{"storage_path_create", "POST", "/api/storage_paths/", handleStoragePathCreate, map[string]any{"name": "s", "path": "{title}"}},
		{"storage_path_update", "PATCH", "/api/storage_paths/1/", handleStoragePathUpdate, map[string]any{"id": float64(1)}},
	}
	cases := []struct {
		in   any
		want float64
	}{
		{"none", 0}, {"any", 1}, {"all", 2}, {"literal", 3}, {"regex", 4}, {"fuzzy", 5}, {"auto", 6},
		{"Regular Expression", 4}, {"exact-match", 3}, {" AUTO ", 6},
		{"4", 4}, {float64(5), 5},
	}
	for _, tl := range tools {
		for _, c := range cases {
			var body map[string]any
			rh := newRouteHandler(t)
			rh.Handle(tl.method, tl.path, func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				json.Unmarshal(raw, &body)
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"id":1}`))
			})
			args := map[string]any{"matching_algorithm": c.in}
			for k, v := range tl.args {
				args[k] = v
			}
			result := callTool(t, tl.handler(testClientAndServer(t, rh)), args)
			assertNotError(t, result)
			if body["matching_algorithm"] != c.want {
				t.Errorf("%s(matching_algorithm=%v): sent %v, want %v", tl.name, c.in, body["matching_algorithm"], c.want)
			}
		}

		args := map[string]any{"matching_algorithm": "sometimes"}
		for k, v := range tl.args {
			args[k] = v
		}
		result := callTool(t, tl.handler(NewClient("http://unused", "unused")), args)
		assertIsError(t, result)
	}
}

func TestMatchingAlgorithmsMatchWorkflowNames(t *testing.T) {
	ws, err := loadWorkflowSchemas()
	if err != nil {
		t.Fatal(err)
	}
	for name, n := range ws.names["matching_algorithm"] {
		if got, ok := matchingAlgorithms[name]; !ok || got != n {
			t.Errorf("workflow name %q = %d, metadata tools have %d (present %v)", name, n, got, ok)
		}
	}
	if matchingAlgorithms["auto"] != 6 {
		t.Errorf("auto = %d, want 6", matchingAlgorithms["auto"])
	}
}

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	filePath := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	return filePath
}

// newVersionedRouteHandler returns a routeHandler that answers the server
// version probe with the given X-Version, as Paperless-ngx does.
func newVersionedRouteHandler(t *testing.T, version string) *routeHandler {
	rh := newRouteHandler(t)
	rh.Handle("GET", versionProbePath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Version", version)
		jsonHandler(t, 200, map[string]any{})(w, r)
	})
	return rh
}

func TestDocumentVersionToolsRejectOldServer(t *testing.T) {
	filePath := writeTempFile(t, "v2.pdf", "pdf")
	// Only the probe is routed: any call to a version endpoint fails the test.
	rh := newVersionedRouteHandler(t, "2.20.15")
	client := testClientAndServer(t, rh)

	for name, tc := range map[string]struct {
		handler func(*Client) server.ToolHandlerFunc
		args    map[string]any
	}{
		"document_update_version":       {handleDocumentUpdateVersion, map[string]any{"id": float64(42), "file_path": filePath}},
		"document_version_list":         {handleDocumentVersionList, map[string]any{"id": float64(42)}},
		"document_version_label_update": {handleDocumentVersionLabelUpdate, map[string]any{"id": float64(42), "version_id": float64(57), "version_label": "x"}},
	} {
		t.Run(name, func(t *testing.T) {
			result := callTool(t, tc.handler(client), tc.args)
			assertIsError(t, result)
			text := resultText(t, result)
			if !strings.Contains(text, "requires Paperless-ngx 3.0.0 or later") || !strings.Contains(text, "2.20.15") {
				t.Errorf("error = %q, want minimum and server version", text)
			}
		})
	}
}

func TestDocumentVersionToolsRejectInvalidIDs(t *testing.T) {
	filePath := writeTempFile(t, "v2.pdf", "pdf")
	// No routes: an invalid ID must never reach the server, least of all a
	// truncated one that names another document.
	client := testClientAndServer(t, newRouteHandler(t))

	for _, bad := range []any{float64(7.9), float64(0), float64(-3), "abc", "7.9", "", true, map[string]any{}, []any{float64(3)}, nil} {
		for name, tc := range map[string]struct {
			handler func(*Client) server.ToolHandlerFunc
			args    map[string]any
		}{
			"update_version id":       {handleDocumentUpdateVersion, map[string]any{"id": bad, "file_path": filePath}},
			"version_list id":         {handleDocumentVersionList, map[string]any{"id": bad}},
			"label_update id":         {handleDocumentVersionLabelUpdate, map[string]any{"id": bad, "version_id": float64(57), "version_label": "x"}},
			"label_update version_id": {handleDocumentVersionLabelUpdate, map[string]any{"id": float64(42), "version_id": bad, "version_label": "x"}},
		} {
			result := callTool(t, tc.handler(client), tc.args)
			if !result.IsError {
				t.Errorf("%s = %#v: expected error, got %s", name, bad, resultText(t, result))
			}
		}
	}
}

func TestDocumentVersionToolsAcceptIntegerStringIDs(t *testing.T) {
	rh := newVersionedRouteHandler(t, "3.2.1")
	rh.Handle("GET", "/api/documents/42/", jsonHandler(t, 200, map[string]any{"id": 42, "versions": []any{}}))

	client := testClientAndServer(t, rh)
	assertNotError(t, callTool(t, handleDocumentVersionList(client), map[string]any{"id": " 42 "}))
}

func TestDocumentUpdateVersionRejectsNonStringLabel(t *testing.T) {
	filePath := writeTempFile(t, "v2.pdf", "pdf")
	client := testClientAndServer(t, newRouteHandler(t))

	for _, bad := range []any{float64(5), true, map[string]any{}, []any{"x"}} {
		result := callTool(t, handleDocumentUpdateVersion(client), map[string]any{
			"id":            float64(42),
			"file_path":     filePath,
			"version_label": bad,
		})
		assertIsError(t, result)
		if text := resultText(t, result); !strings.Contains(text, "version_label must be a string or null") {
			t.Errorf("version_label = %#v: error = %q", bad, text)
		}
	}
}

func TestDocumentUpdateVersionNullLabelMeansNoLabel(t *testing.T) {
	filePath := writeTempFile(t, "v2.pdf", "pdf")
	for _, label := range []any{nil, "null", " NULL ", ""} {
		rh := newVersionedRouteHandler(t, "3.2.1")
		rh.Handle("POST", "/api/documents/42/update_version/", func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("parse multipart form: %v", err)
			}
			if got, ok := r.MultipartForm.Value["version_label"]; ok {
				t.Errorf("version_label = %#v: sent %v, want no label", label, got)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`"task-uuid"`))
		})

		client := testClientAndServer(t, rh)
		assertNotError(t, callTool(t, handleDocumentUpdateVersion(client), map[string]any{
			"id":            float64(42),
			"file_path":     filePath,
			"version_label": label,
		}))
	}
}

func TestDocumentVersionLabelSchemaAllowsNull(t *testing.T) {
	tool := mcp.NewTool("x", withVersionLabel())
	prop, ok := tool.InputSchema.Properties["version_label"].(map[string]any)
	if !ok {
		t.Fatalf("version_label property missing: %#v", tool.InputSchema.Properties)
	}
	types, _ := prop["type"].([]string)
	if len(types) != 2 || types[0] != "string" || types[1] != "null" {
		t.Errorf("version_label type = %#v, want [string null]", prop["type"])
	}
}

func TestDocumentVersionListRejectsMissingVersionsField(t *testing.T) {
	// Version unknown (no X-Version header) and a pre-3.0 style response that
	// silently drops the unknown versions field.
	rh := newRouteHandler(t)
	rh.Handle("GET", versionProbePath, jsonHandler(t, 200, map[string]any{}))
	rh.Handle("GET", "/api/documents/42/", jsonHandler(t, 200, map[string]any{"id": 42}))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentVersionList(client), map[string]any{"id": float64(42)})
	assertIsError(t, result)
	if text := resultText(t, result); !strings.Contains(text, "no versions field") {
		t.Errorf("error = %q", text)
	}
}

func TestDocumentVersionListProceedsWhenVersionUnknown(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", versionProbePath, jsonHandler(t, 200, map[string]any{}))
	rh.Handle("GET", "/api/documents/42/", jsonHandler(t, 200, map[string]any{"id": 42, "versions": []any{}}))

	client := testClientAndServer(t, rh)
	assertNotError(t, callTool(t, handleDocumentVersionList(client), map[string]any{"id": float64(42)}))
}

func TestDocumentUpdateVersion(t *testing.T) {
	filePath := writeTempFile(t, "v2.pdf", "pdf-v2")

	var capturedLabel []string
	var capturedFileName, capturedFileContent string
	rh := newVersionedRouteHandler(t, "3.2.1")
	rh.Handle("POST", "/api/documents/42/update_version/", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		capturedLabel = r.MultipartForm.Value["version_label"]
		files := r.MultipartForm.File["document"]
		if len(files) != 1 {
			t.Fatalf("expected 1 file in field 'document', got %d", len(files))
		}
		capturedFileName = files[0].Filename
		f, err := files[0].Open()
		if err != nil {
			t.Fatalf("open uploaded file: %v", err)
		}
		defer f.Close()
		data, _ := io.ReadAll(f)
		capturedFileContent = string(data)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`"task-uuid"`))
	})

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentUpdateVersion(client), map[string]any{
		"id":            float64(42),
		"file_path":     filePath,
		"version_label": "  Signed copy  ",
	})
	assertNotError(t, result)

	if got := resultText(t, result); !strings.Contains(got, "task-uuid") {
		t.Errorf("result = %s, want task UUID", got)
	}
	if len(capturedLabel) != 1 || capturedLabel[0] != "Signed copy" {
		t.Errorf("version_label = %v, want [Signed copy]", capturedLabel)
	}
	if capturedFileName != "v2.pdf" {
		t.Errorf("file name = %q, want v2.pdf", capturedFileName)
	}
	if capturedFileContent != "pdf-v2" {
		t.Errorf("file content = %q, want pdf-v2", capturedFileContent)
	}
}

func TestDocumentUpdateVersionOmitsEmptyLabel(t *testing.T) {
	filePath := writeTempFile(t, "v2.pdf", "pdf")

	rh := newVersionedRouteHandler(t, "3.2.1")
	rh.Handle("POST", "/api/documents/42/update_version/", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		if _, ok := r.MultipartForm.Value["version_label"]; ok {
			t.Error("version_label should be omitted when blank")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`"task-uuid"`))
	})

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentUpdateVersion(client), map[string]any{
		"id":            float64(42),
		"file_path":     filePath,
		"version_label": "   ",
	})
	assertNotError(t, result)
}

func TestDocumentUpdateVersionRejectsLongLabel(t *testing.T) {
	filePath := writeTempFile(t, "v2.pdf", "pdf")
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentUpdateVersion(client), map[string]any{
		"id":            float64(42),
		"file_path":     filePath,
		"version_label": strings.Repeat("x", 65),
	})
	assertIsError(t, result)
}

func TestDocumentUpdateVersionRequiresFields(t *testing.T) {
	filePath := writeTempFile(t, "v2.pdf", "pdf")
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentUpdateVersion(client), map[string]any{"file_path": filePath})
	assertIsError(t, result)
	result = callTool(t, handleDocumentUpdateVersion(client), map[string]any{"id": float64(42)})
	assertIsError(t, result)
}

func TestDocumentUpdateVersionRejectsNonExistentPath(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentUpdateVersion(client), map[string]any{
		"id":        float64(42),
		"file_path": "/nonexistent/file.pdf",
	})
	assertIsError(t, result)
}

func TestDocumentUpdateVersionRejectsDirectory(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentUpdateVersion(client), map[string]any{
		"id":        float64(42),
		"file_path": t.TempDir(),
	})
	assertIsError(t, result)
}

func TestDocumentUpdateVersionRejectsSymlink(t *testing.T) {
	target := writeTempFile(t, "target.pdf", "pdf")
	link := filepath.Join(t.TempDir(), "link.pdf")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("skipping: symlinks not supported: %v", err)
	}

	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentUpdateVersion(client), map[string]any{
		"id":        float64(42),
		"file_path": link,
	})
	assertIsError(t, result)
}

func TestDocumentUpdateVersionForbidden(t *testing.T) {
	filePath := writeTempFile(t, "v2.pdf", "pdf")

	rh := newVersionedRouteHandler(t, "3.2.1")
	rh.Handle("POST", "/api/documents/42/update_version/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("Insufficient permissions"))
	})

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentUpdateVersion(client), map[string]any{
		"id":        float64(42),
		"file_path": filePath,
	})
	assertIsError(t, result)

	m := resultJSON(t, result)
	if m["status_code"] != float64(403) {
		t.Errorf("status_code = %v, want 403", m["status_code"])
	}
}

func TestDocumentVersionList(t *testing.T) {
	var capturedFields string
	rh := newVersionedRouteHandler(t, "3.2.1")
	rh.Handle("GET", "/api/documents/42/", func(w http.ResponseWriter, r *http.Request) {
		capturedFields = r.URL.Query().Get("fields")
		jsonHandler(t, 200, map[string]any{
			"id": 42,
			"versions": []any{
				map[string]any{"id": 57, "added": "2026-10-15T10:00:00Z", "version_label": "v2", "checksum": "b", "is_root": false},
				map[string]any{"id": 42, "added": "2026-10-01T10:00:00Z", "version_label": nil, "checksum": "a", "is_root": true},
			},
		})(w, r)
	})

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentVersionList(client), map[string]any{"id": float64(42)})
	assertNotError(t, result)

	if capturedFields != "id,versions" {
		t.Errorf("fields = %q, want id,versions", capturedFields)
	}
	m := resultJSON(t, result)
	versions, ok := m["versions"].([]any)
	if !ok || len(versions) != 2 {
		t.Fatalf("versions = %v, want 2 entries", m["versions"])
	}
}

func TestDocumentVersionListRequiresID(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentVersionList(client), map[string]any{})
	assertIsError(t, result)
}

func TestDocumentVersionLabelUpdate(t *testing.T) {
	tests := []struct {
		name  string
		label any
		want  any
	}{
		{"set", " Final ", "Final"},
		{"clear with empty string", "", nil},
		{"clear with null", nil, nil},
		{"clear with null string", "null", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedBody map[string]any
			rh := newVersionedRouteHandler(t, "3.2.1")
			rh.Handle("PATCH", "/api/documents/42/versions/57/", func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				jsonHandler(t, 200, map[string]any{"id": 57, "version_label": capturedBody["version_label"], "is_root": false})(w, r)
			})

			client := testClientAndServer(t, rh)
			result := callTool(t, handleDocumentVersionLabelUpdate(client), map[string]any{
				"id":            float64(42),
				"version_id":    float64(57),
				"version_label": tt.label,
			})
			assertNotError(t, result)

			got, ok := capturedBody["version_label"]
			if !ok {
				t.Fatal("body should contain version_label")
			}
			if got != tt.want {
				t.Errorf("version_label = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDocumentVersionLabelUpdateValidation(t *testing.T) {
	client := NewClient("http://unused", "unused")
	for name, args := range map[string]map[string]any{
		"missing id":         {"version_id": float64(57), "version_label": "x"},
		"missing version_id": {"id": float64(42), "version_label": "x"},
		"missing label":      {"id": float64(42), "version_id": float64(57)},
		"label too long":     {"id": float64(42), "version_id": float64(57), "version_label": strings.Repeat("x", 65)},
		"label not a string": {"id": float64(42), "version_id": float64(57), "version_label": float64(3)},
	} {
		t.Run(name, func(t *testing.T) {
			assertIsError(t, callTool(t, handleDocumentVersionLabelUpdate(client), args))
		})
	}
}

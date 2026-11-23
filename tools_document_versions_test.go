package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// rootRoute serves GET /api/documents/{id}/root/ with root_id.
func rootRoute(rh *routeHandler, id string, rootID int) {
	rh.Handle("GET", "/api/documents/"+id+"/root/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"root_id": rootID})
	})
}

func statusRoute(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}
}

func textBlocks(result *mcp.CallToolResult) []string {
	var out []string
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			out = append(out, tc.Text)
		}
	}
	return out
}

func tempFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "v2.pdf")
	if err := os.WriteFile(p, []byte("%PDF-1.4 test"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDocumentVersionUpload(t *testing.T) {
	var fields map[string][]string
	var fileName string
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/7/update_version/", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		fields = r.MultipartForm.Value
		if fh := r.MultipartForm.File["document"]; len(fh) == 1 {
			fileName = fh[0].Filename
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`"` + testTaskUUID + `"`))
	})
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentVersionUpload(client), map[string]any{"id": float64(7), "file_path": tempFile(t), "version_label": "  signed  "})
	assertNotError(t, result)
	if !reflect.DeepEqual(fields, map[string][]string{"version_label": {"signed"}}) || fileName != "v2.pdf" {
		t.Errorf("fields = %v, file = %q", fields, fileName)
	}
	if !strings.Contains(resultText(t, result), testTaskUUID) {
		t.Errorf("result = %s, want the task id", resultText(t, result))
	}
}

func TestDocumentVersionUploadWaits(t *testing.T) {
	fastPolling(t)
	seq := &taskSequence{statuses: []string{"pending", "success"}}
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/7/update_version/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`"` + testTaskUUID + `"`))
	})
	rh.Handle("GET", "/api/tasks/", seq.handle)
	result := callTool(t, handleDocumentVersionUpload(testClientAndServer(t, rh)), map[string]any{"id": float64(7), "file_path": tempFile(t), "wait": true})
	assertNotError(t, result)
	if m := resultJSON(t, result); m["task"].(map[string]any)["status"] != "success" {
		t.Errorf("result = %v", m)
	}
}

func TestDocumentVersionUploadRefuses(t *testing.T) {
	none := testClientAndServer(t, newRouteHandler(t))
	for name, args := range map[string]map[string]any{
		"long label": {"id": float64(7), "file_path": tempFile(t), "version_label": strings.Repeat("é", 65)},
		"no file":    {"id": float64(7), "file_path": filepath.Join(t.TempDir(), "missing.pdf")},
		"directory":  {"id": float64(7), "file_path": t.TempDir()},
		"no id":      {"file_path": tempFile(t)},
	} {
		assertIsError(t, callTool(t, handleDocumentVersionUpload(none), args))
		_ = name
	}
	// 64 characters (not bytes) is fine.
	if _, errRes := versionLabel(makeRequest(map[string]any{"version_label": strings.Repeat("é", 64)})); errRes != nil {
		t.Errorf("64 characters refused: %s", resultText(t, errRes))
	}
}

const versionsJSON = `{"id": 1, "versions": [{"id": 9, "added": "2026-11-20T10:00:00Z", "version_label": "signed", "checksum": "b", "is_root": false}, {"id": 1, "added": "2026-11-01T10:00:00Z", "version_label": null, "checksum": "a", "is_root": true}]}`

func TestDocumentVersionList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fields") != "id,versions" {
			t.Errorf("fields = %q", r.URL.Query().Get("fields"))
		}
		statusRoute(200, versionsJSON)(w, r)
	})
	rh.Handle("GET", "/api/documents/9/", statusRoute(404, `{"detail": "No Document matches the given query."}`))
	rootRoute(rh, "9", 1)
	client := testClientAndServer(t, rh)
	for _, id := range []float64{1, 9} {
		m := resultJSON(t, callTool(t, handleDocumentVersionList(client), map[string]any{"id": id}))
		if m["root_id"] != float64(1) || len(m["versions"].([]any)) != 2 {
			t.Errorf("id %v: result = %v", id, m)
		}
	}
}

func TestDocumentVersionListKeepsThe404(t *testing.T) {
	for name, root := range map[string]http.HandlerFunc{
		"hidden root":  statusRoute(403, `"Insufficient permissions"`),
		"trashed root": statusRoute(200, `{"root_id": 5}`),
		"missing":      statusRoute(404, `{"detail": "Not found."}`),
	} {
		rh := newRouteHandler(t)
		rh.Handle("GET", "/api/documents/5/", statusRoute(404, `{"detail": "No Document matches the given query."}`))
		rh.Handle("GET", "/api/documents/5/root/", root)
		result := callTool(t, handleDocumentVersionList(testClientAndServer(t, rh)), map[string]any{"id": float64(5)})
		assertIsError(t, result)
		if !strings.Contains(resultText(t, result), "404") {
			t.Errorf("%s: error = %s, want the document's 404", name, resultText(t, result))
		}
	}
}

func TestDocumentVersionListNeedsVersions(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/", statusRoute(200, `{"id": 1}`))
	result := callTool(t, handleDocumentVersionList(testClientAndServer(t, rh)), map[string]any{"id": float64(1)})
	assertIsError(t, result)
}

func TestDocumentVersionLabel(t *testing.T) {
	for name, tc := range map[string]struct {
		args map[string]any
		want map[string]any
	}{
		"set":   {map[string]any{"id": float64(1), "version_id": float64(9), "version_label": " signed "}, map[string]any{"version_label": "signed"}},
		"clear": {map[string]any{"id": float64(1), "version_id": float64(9), "clear": []any{"version_label"}}, map[string]any{"version_label": nil}},
		"root":  {map[string]any{"id": float64(1), "version_id": float64(1), "version_label": "original"}, map[string]any{"version_label": "original"}},
	} {
		path := "/api/documents/1/versions/9/"
		if tc.args["version_id"] == float64(1) {
			path = "/api/documents/1/versions/1/"
		}
		client, got := captureJSONBody(t, "PATCH", path)
		assertNotError(t, callTool(t, handleDocumentVersionLabel(client), tc.args))
		if !reflect.DeepEqual(*got, tc.want) {
			t.Errorf("%s: body = %v, want %v", name, *got, tc.want)
		}
	}
	none := testClientAndServer(t, newRouteHandler(t))
	for _, args := range []map[string]any{
		{"id": float64(1), "version_id": float64(9)},
		{"id": float64(1), "version_id": float64(9), "version_label": "  "},
		{"id": float64(1), "version_id": float64(9), "version_label": strings.Repeat("x", 65)},
		{"id": float64(1), "version_label": "x"},
	} {
		assertIsError(t, callTool(t, handleDocumentVersionLabel(none), args))
	}
}

func TestDocumentVersionDeleteAndRoot(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/documents/1/versions/9/", statusRoute(200, `{"result": "OK", "current_version_id": 1}`))
	rootRoute(rh, "9", 1)
	client := testClientAndServer(t, rh)
	if m := resultJSON(t, callTool(t, handleDocumentVersionDelete(client), map[string]any{"id": float64(1), "version_id": float64(9)})); m["current_version_id"] != float64(1) {
		t.Errorf("delete = %v", m)
	}
	tool := NewServer(client, nil).GetTool("document_root")
	if m := resultJSON(t, callTool(t, tool.Handler, map[string]any{"id": float64(9)})); m["root_id"] != float64(1) {
		t.Errorf("root = %v", m)
	}
}

func TestDocumentMergeAsVersions(t *testing.T) {
	client, got, _ := actionServer(t, documentActionPaths["merge_as_versions"])
	assertNotError(t, callTool(t, documentActionHandlerFor(client, "merge_as_versions"), map[string]any{
		"documents": []any{float64(1), float64(2)}, "root_document_id": float64(1), "version_label": " scan ",
	}))
	want := map[string]any{"documents": []any{float64(1), float64(2)}, "root_document_id": float64(1), "version_label": "scan"}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}
	none := testClientAndServer(t, newRouteHandler(t))
	for want, args := range map[string]map[string]any{
		"at least 2":       {"documents": []any{float64(1)}, "root_document_id": float64(1)},
		"one of documents": {"documents": []any{float64(1), float64(2)}, "root_document_id": float64(3)},
		"root_document_id": {"documents": []any{float64(1), float64(2)}},
		"exactly one":      {"documents": []any{float64(1), float64(2), float64(3)}, "root_document_id": float64(1), "version_label": "x"},
		"doesn't support":  {"all": true, "root_document_id": float64(1)},
		"at most 64":       {"documents": []any{float64(1), float64(2)}, "root_document_id": float64(1), "version_label": strings.Repeat("x", 65)},
	} {
		result := callTool(t, documentActionHandlerFor(none, "merge_as_versions"), args)
		assertIsError(t, result)
		if !strings.Contains(resultText(t, result), want) {
			t.Errorf("%v: error = %s, want %q", args, resultText(t, result), want)
		}
	}
}

// versionQuery records the version query parameter of one route.
func versionQuery(t *testing.T, rh *routeHandler, method, path, body string) *string {
	got := new(string)
	*got = "unset"
	rh.Handle(method, path, func(w http.ResponseWriter, r *http.Request) {
		*got = r.URL.Query().Get("version")
		io.Copy(io.Discard, r.Body)
		statusRoute(200, body)(w, r)
	})
	return got
}

func TestVersionParameter(t *testing.T) {
	rh := newRouteHandler(t)
	get := versionQuery(t, rh, "GET", "/api/documents/1/", `{"id": 1}`)
	meta := versionQuery(t, rh, "GET", "/api/documents/1/metadata/", `{}`)
	patch := versionQuery(t, rh, "PATCH", "/api/documents/1/", `{"id": 1}`)
	client := testClientAndServer(t, rh)
	srv := NewServer(client, nil)
	call := func(tool string, args map[string]any) *mcp.CallToolResult {
		return callTool(t, srv.GetTool(tool).Handler, args)
	}
	assertNotError(t, call("document_get", map[string]any{"id": float64(1), "version": float64(9)}))
	assertNotError(t, call("document_metadata", map[string]any{"id": float64(1), "version": "9"}))
	assertNotError(t, call("document_update", map[string]any{"id": float64(1), "version": float64(9), "content": "fixed text"}))
	for name, got := range map[string]*string{"get": get, "metadata": meta, "update": patch} {
		if *got != "9" {
			t.Errorf("%s: version = %q, want 9", name, *got)
		}
	}
	*patch = "unset"
	assertNotError(t, call("document_update", map[string]any{"id": float64(1), "version": float64(9), "clear": []any{"content"}}))
	if *patch != "9" {
		t.Errorf("clear content with version: version = %q", *patch)
	}
	*get = "unset"
	assertNotError(t, call("document_get", map[string]any{"id": float64(1)}))
	if *get != "" {
		t.Errorf("without version: version = %q, want none", *get)
	}

	none := testClientAndServer(t, newRouteHandler(t))
	nsrv := NewServer(none, nil)
	for want, tc := range map[string]struct {
		tool string
		args map[string]any
	}{
		"applies only to content": {"document_update", map[string]any{"id": float64(1), "version": float64(9), "title": "T"}},
		"single id":               {"document_download", map[string]any{"ids": []any{float64(1), float64(2)}, "version": float64(9), "content": true}},
		"positive whole number":   {"document_get", map[string]any{"id": float64(1), "version": float64(0)}},
		"whole number":            {"document_metadata", map[string]any{"id": float64(1), "version": 1.5}},
		"must be a whole number":  {"document_page_image", map[string]any{"id": float64(1), "version": "x"}},
	} {
		result := callTool(t, nsrv.GetTool(tc.tool).Handler, tc.args)
		assertIsError(t, result)
		if !strings.Contains(resultText(t, result), want) {
			t.Errorf("%s %v: error = %s, want %q", tc.tool, tc.args, resultText(t, result), want)
		}
	}
}

func TestDownloadAndPageImageSendVersion(t *testing.T) {
	var got []string
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/download/", func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Query().Get("version"))
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("hello"))
	})
	client := testClientAndServer(t, rh)
	callTool(t, handleDocumentDownload(client, testDownloader(t, 1)), map[string]any{"ids": []any{float64(1)}, "version": float64(9), "content": true})
	callTool(t, handleDocumentPageImage(client), map[string]any{"id": float64(1), "version": float64(9)})
	if !reflect.DeepEqual(got, []string{"9", "9"}) {
		t.Errorf("version queries = %v, want [9 9]", got)
	}
}

func TestNotesUseTheRoot(t *testing.T) {
	rh := newRouteHandler(t)
	rootRoute(rh, "9", 1)
	rootRoute(rh, "1", 1)
	rh.Handle("GET", "/api/documents/1/notes/", statusRoute(200, `[{"id": 3, "note": "root note"}]`))
	rh.Handle("GET", "/api/documents/9/notes/", statusRoute(200, `[{"id": 4, "note": "version note"}]`))
	rh.Handle("POST", "/api/documents/1/notes/", statusRoute(200, `[{"id": 3}, {"id": 5}]`))
	rh.Handle("DELETE", "/api/documents/1/notes/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") == "4" {
			statusRoute(404, `{"detail": "No Note matches the given query."}`)(w, r)
			return
		}
		statusRoute(200, `[]`)(w, r)
	})
	rh.Handle("DELETE", "/api/documents/9/notes/", statusRoute(200, `[]`))
	client := testClientAndServer(t, rh)

	// A root id: only the API's JSON.
	if b := textBlocks(callTool(t, handleDocumentNoteList(client), map[string]any{"id": float64(1)})); len(b) != 1 {
		t.Errorf("root list blocks = %v, want 1", b)
	}
	// A version id: the root's notes, plus a block naming the mapping and the version's own notes.
	b := textBlocks(callTool(t, handleDocumentNoteList(client), map[string]any{"id": float64(9)}))
	if len(b) != 2 || !strings.Contains(b[0], "root note") || !strings.Contains(b[1], "root document 1") || !strings.Contains(b[1], "version note") {
		t.Errorf("version list blocks = %v", b)
	}
	b = textBlocks(callTool(t, handleDocumentNoteAdd(client), map[string]any{"id": float64(9), "note": "n"}))
	if len(b) != 2 || !strings.Contains(b[1], "root document 1") {
		t.Errorf("add blocks = %v", b)
	}
	b = textBlocks(callTool(t, handleDocumentNoteDelete(client), map[string]any{"id": float64(9), "note_id": float64(3)}))
	if len(b) != 2 || !strings.Contains(b[1], "root document 1") {
		t.Errorf("delete blocks = %v", b)
	}
	// A note stored on the version itself is deleted there.
	result := callTool(t, handleDocumentNoteDelete(client), map[string]any{"id": float64(9), "note_id": float64(4)})
	assertNotError(t, result)
	if b := textBlocks(result); len(b) != 2 || !strings.Contains(b[1], "stored on version 9") {
		t.Errorf("fallback delete blocks = %v", b)
	}
}

func TestNotesRootLookupErrors(t *testing.T) {
	for name, root := range map[string]http.HandlerFunc{
		"hidden":  statusRoute(403, `"Insufficient permissions"`),
		"missing": statusRoute(404, `{"detail": "Not found."}`),
	} {
		rh := newRouteHandler(t) // no notes routes: a notes call fails the test
		rh.Handle("GET", "/api/documents/5/root/", root)
		client := testClientAndServer(t, rh)
		assertIsError(t, callTool(t, handleDocumentNoteList(client), map[string]any{"id": float64(5)}))
		assertIsError(t, callTool(t, handleDocumentNoteAdd(client), map[string]any{"id": float64(5), "note": "n"}))
		assertIsError(t, callTool(t, handleDocumentNoteDelete(client), map[string]any{"id": float64(5), "note_id": float64(1)}))
		_ = name
	}
	// A trashed root is its own root; the notes 404 comes back.
	rh := newRouteHandler(t)
	rootRoute(rh, "5", 5)
	rh.Handle("GET", "/api/documents/5/notes/", statusRoute(404, `{"detail": "No Document matches the given query."}`))
	assertIsError(t, callTool(t, handleDocumentNoteList(testClientAndServer(t, rh)), map[string]any{"id": float64(5)}))
}

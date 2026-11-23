package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// uploadForm returns a client whose post_document route records the form.
func uploadForm(t *testing.T) (*Client, string, *url.Values) {
	t.Helper()
	filePath := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(filePath, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	form := &url.Values{}
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/post_document/", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart form: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		*form = r.MultipartForm.Value
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`"task-uuid"`))
	})
	return testClientAndServer(t, rh), filePath, form
}

func TestDocumentUploadCustomFields(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want any // decoded JSON of the one custom_fields form field
	}{
		{"array", []any{float64(3), float64(7)}, []any{float64(3), float64(7)}},
		{"array deduped", []any{float64(3), "3", float64(7)}, []any{float64(3), float64(7)}},
		{"json string array", "[4]", []any{float64(4)}},
		{"object", map[string]any{"3": "INV-1", "7": 12.5}, map[string]any{"3": "INV-1", "7": 12.5}},
		{"object canonical keys", map[string]any{" 03": nil}, map[string]any{"3": nil}},
		{"json string object", `{"5": [1, 2]}`, map[string]any{"5": []any{float64(1), float64(2)}}},
		{"bare id", float64(5), []any{float64(5)}},
		{"bare id string", "5", []any{float64(5)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, filePath, form := uploadForm(t)
			assertNotError(t, callTool(t, handleDocumentUpload(client), map[string]any{"file_path": filePath, "custom_fields": tt.in}))
			vals := (*form)["custom_fields"]
			if len(vals) != 1 {
				t.Fatalf("custom_fields form fields = %v, want exactly one", vals)
			}
			var got any
			if err := json.Unmarshal([]byte(vals[0]), &got); err != nil {
				t.Fatalf("custom_fields %q is not JSON: %v", vals[0], err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("custom_fields = %s, want %v", vals[0], tt.want)
			}
		})
	}
}

func TestDocumentUploadCustomFieldsPadding(t *testing.T) {
	for _, in := range []any{nil, "", "null", "[]", "{}", []any{}, map[string]any{}} {
		client, filePath, form := uploadForm(t)
		assertNotError(t, callTool(t, handleDocumentUpload(client), map[string]any{"file_path": filePath, "custom_fields": in}))
		if form.Has("custom_fields") {
			t.Errorf("custom_fields %#v: sent %v, want no field", in, (*form)["custom_fields"])
		}
	}
}

func TestDocumentUploadCustomFieldsRefused(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{[]any{1.5}, "1.5 is not a custom field id"},
		{[]any{"x"}, "x is not a custom field id"},
		{[]any{float64(0)}, "0 is not a custom field id"},
		{[]any{true}, customFieldsShape},
		{map[string]any{"a": 1}, `key "a" is not a custom field id`},
		{map[string]any{"3": "a", "03": "b"}, "names field 3 twice"},
		{true, customFieldsShape},
		{float64(-2), customFieldsShape},
		{"x", "invalid custom_fields JSON"},
	}
	for _, tt := range tests {
		// No route: a request would fail the test.
		client := testClientAndServer(t, newRouteHandler(t))
		filePath := filepath.Join(t.TempDir(), "doc.pdf")
		if err := os.WriteFile(filePath, []byte("pdf"), 0o644); err != nil {
			t.Fatal(err)
		}
		assertErrorContains(t, callTool(t, handleDocumentUpload(client), map[string]any{"file_path": filePath, "custom_fields": tt.in}), tt.want)
	}
}

func TestDocumentEmailUseArchiveVersion(t *testing.T) {
	base := map[string]any{"documents": []any{float64(1)}, "to": "a@example.com", "subject": "s", "body": "b"}
	with := func(extra map[string]any) map[string]any {
		m := map[string]any{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	for in, want := range map[any]bool{false: false, true: true, "false": false} {
		client, got := captureJSONBody(t, "POST", "/api/documents/email/")
		assertNotError(t, callTool(t, handleDocumentEmail(client), with(map[string]any{"use_archive_version": in})))
		if (*got)["use_archive_version"] != want {
			t.Errorf("use_archive_version %v: body = %v, want %v", in, *got, want)
		}
	}
	for _, in := range []any{nil, ""} {
		client, got := captureJSONBody(t, "POST", "/api/documents/email/")
		assertNotError(t, callTool(t, handleDocumentEmail(client), with(map[string]any{"use_archive_version": in})))
		if _, ok := (*got)["use_archive_version"]; ok {
			t.Errorf("use_archive_version %#v: sent %v, want it left to the server", in, (*got)["use_archive_version"])
		}
	}
	client := testClientAndServer(t, newRouteHandler(t))
	assertErrorContains(t, callTool(t, handleDocumentEmail(client), with(map[string]any{"use_archive_version": "maybe"})), "use_archive_version must be true or false")
}

func TestShareLinkCreateRefusesOldArchiveName(t *testing.T) {
	client := testClientAndServer(t, newRouteHandler(t))
	for _, in := range []any{false, true, "false"} {
		assertErrorContains(t, callTool(t, handleShareLinkCreate(client), map[string]any{"document": float64(1), "file_version_archive": in}), "file_version_archive was renamed to use_archive_version")
	}
}

func TestSearchGlobalDBOnly(t *testing.T) {
	tests := []struct {
		in   any
		want url.Values
	}{
		{true, url.Values{"query": {"invoice"}, "db_only": {"true"}}},
		{"true", url.Values{"query": {"invoice"}, "db_only": {"true"}}},
		// Upstream reads any non-empty db_only as true, so false is never sent.
		{false, url.Values{"query": {"invoice"}}},
		{"false", url.Values{"query": {"invoice"}}},
		{"", url.Values{"query": {"invoice"}}},
	}
	for _, tt := range tests {
		var captured url.Values
		rh := newRouteHandler(t)
		rh.Handle("GET", "/api/search/", func(w http.ResponseWriter, r *http.Request) {
			captured = r.URL.Query()
			jsonHandler(t, 200, map[string]any{"total": 0})(w, r)
		})
		client := testClientAndServer(t, rh)
		assertNotError(t, callTool(t, handleSearchGlobal(client), map[string]any{"query": "invoice", "db_only": tt.in}))
		if !reflect.DeepEqual(captured, tt.want) {
			t.Errorf("db_only %#v: query = %v, want %v", tt.in, captured, tt.want)
		}
	}
	client := testClientAndServer(t, newRouteHandler(t))
	assertErrorContains(t, callTool(t, handleSearchGlobal(client), map[string]any{"query": "invoice", "db_only": "yes"}), "db_only must be true or false")
}

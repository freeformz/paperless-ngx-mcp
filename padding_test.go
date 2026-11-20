package main

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// Padded "" values are not given; values that don't parse are errors, not 0 or false.
func TestUpdatePaddingAndBadValues(t *testing.T) {
	client, got := captureJSONBody(t, "PATCH", "/api/tags/1/")
	assertNotError(t, callTool(t, handleTagUpdate(client), map[string]any{"id": float64(1), "name": "n", "matching_algorithm": "", "is_inbox_tag": "", "is_insensitive": ""}))
	if want := map[string]any{"name": "n"}; !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}

	none := testClientAndServer(t, newRouteHandler(t))
	for _, tc := range []struct {
		name    string
		handler func(*Client) func() *mcp.CallToolResult
		want    string
	}{
		{"tag_update matching_algorithm", func(c *Client) func() *mcp.CallToolResult {
			return func() *mcp.CallToolResult {
				return callTool(t, handleTagUpdate(c), map[string]any{"id": float64(1), "matching_algorithm": "abc"})
			}
		}, "matching_algorithm must be a whole number"},
		{"tag_update is_inbox_tag", func(c *Client) func() *mcp.CallToolResult {
			return func() *mcp.CallToolResult {
				return callTool(t, handleTagUpdate(c), map[string]any{"id": float64(1), "is_inbox_tag": "maybe"})
			}
		}, "is_inbox_tag must be true or false"},
		{"tag_list is_root", func(c *Client) func() *mcp.CallToolResult {
			return func() *mcp.CallToolResult { return callTool(t, handleTagList(c), map[string]any{"is_root": "maybe"}) }
		}, "is_root must be true or false"},
		{"user_update is_superuser", func(c *Client) func() *mcp.CallToolResult {
			return func() *mcp.CallToolResult {
				return callTool(t, handleUserUpdate(c), map[string]any{"id": float64(1), "is_superuser": "maybe"})
			}
		}, "is_superuser must be true or false"},
		{"tag_get id", func(c *Client) func() *mcp.CallToolResult {
			return func() *mcp.CallToolResult {
				return callTool(t, handleGetByID(c, "/api/tags/%d/"), map[string]any{"id": "abc"})
			}
		}, "id must be a whole number"},
	} {
		result := tc.handler(none)()
		assertIsError(t, result)
		if !strings.Contains(resultText(t, result), tc.want) {
			t.Errorf("%s: error = %s, want %q", tc.name, resultText(t, result), tc.want)
		}
	}
}

// "null" is text only for free-text params; for dates, colours and the like it is not given.
func TestNullStringOnlyKeptForFreeText(t *testing.T) {
	client, got := captureJSONBody(t, "POST", "/api/share_links/")
	assertNotError(t, callThroughMiddleware(t, client, "share_link_create", map[string]any{"document": float64(1), "expiration": "null"}))
	if want := map[string]any{"document": float64(1)}; !reflect.DeepEqual(*got, want) {
		t.Errorf("share_link_create body = %v, want %v", *got, want)
	}

	client, got = captureJSONBody(t, "PATCH", "/api/tags/1/")
	assertNotError(t, callThroughMiddleware(t, client, "tag_update", map[string]any{"id": float64(1), "name": "null", "color": "null"}))
	if want := map[string]any{"name": "null"}; !reflect.DeepEqual(*got, want) {
		t.Errorf("tag_update body = %v, want %v", *got, want)
	}
}

func TestSelectionTagListAsJSONString(t *testing.T) {
	client, got := captureJSONBody(t, "POST", "/api/documents/selection_data/")
	assertNotError(t, callTool(t, handleDocumentSelectionData(client), map[string]any{"all": true, "filters": map[string]any{"tags_id_all": "[3, 4]"}}))
	if want := map[string]any{"all": true, "filters": map[string]any{"tags__id__all": "3,4"}}; !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}
	result := callTool(t, handleDocumentSelectionData(client), map[string]any{"all": true, "filters": map[string]any{"tags_id_all": "[3, \"x\"]"}})
	assertIsError(t, result)
}

func TestExplicitOnlyToolsAcceptEmptyFilters(t *testing.T) {
	for _, f := range []any{map[string]any{}, "{}", " { } "} {
		req := mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"filters": f, "all": false}}}
		if errRes := rejectSelectionArgs(req, "document_merge"); errRes != nil {
			t.Errorf("filters %q: %s", f, resultTextOf(errRes))
		}
	}
	req := mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"filters": map[string]any{"title": "x"}}}}
	if rejectSelectionArgs(req, "document_merge") == nil {
		t.Error("non-empty filters should be rejected")
	}
}

// captureQuery returns a client whose GET path records its query string.
func captureQuery(t *testing.T, path, response string) (*Client, *url.Values) {
	t.Helper()
	var got url.Values
	rh := newRouteHandler(t)
	rh.Handle("GET", path, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(response))
	})
	return testClientAndServer(t, rh), &got
}

func TestNumericBooleans(t *testing.T) {
	client, got := captureJSONBody(t, "PATCH", "/api/tags/1/")
	assertNotError(t, callTool(t, handleTagUpdate(client), map[string]any{"id": float64(1), "is_inbox_tag": float64(1), "is_insensitive": float64(0)}))
	if want := map[string]any{"is_inbox_tag": true, "is_insensitive": false}; !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}
	for _, v := range []any{float64(2), 0.5, float64(-1)} {
		assertIsError(t, callTool(t, handleTagUpdate(client), map[string]any{"id": float64(1), "is_inbox_tag": v}))
	}
	// all stays strict: a number is not true.
	result := callTool(t, handleDocumentSelectionData(client), map[string]any{"all": float64(1)})
	assertIsError(t, result)
}

func TestListFilterParsing(t *testing.T) {
	client, got := captureQuery(t, "/api/tasks/", `{"count":0,"results":[]}`)
	assertNotError(t, callTool(t, handleTaskList(client), map[string]any{"acknowledged": float64(0), "owner": "3", "is_complete": "true"}))
	for k, want := range map[string]string{"acknowledged": "false", "owner": "3", "is_complete": "true"} {
		if g := got.Get(k); g != want {
			t.Errorf("%s = %q, want %q", k, g, want)
		}
	}
	assertNotError(t, callTool(t, handleTaskList(client), map[string]any{"acknowledged": "", "owner": ""}))
	if got.Has("acknowledged") || got.Has("owner") {
		t.Errorf("padded \"\" was sent: %v", *got)
	}
	for _, args := range []map[string]any{{"owner": "me"}, {"acknowledged": "maybe"}, {"is_complete": "x"}} {
		assertIsError(t, callTool(t, handleTaskList(client), args))
	}

	sc, sq := captureQuery(t, "/api/search/autocomplete/", `[]`)
	assertNotError(t, callTool(t, handleSearchAutocomplete(sc), map[string]any{"term": "inv", "limit": "5"}))
	if sq.Get("limit") != "5" {
		t.Errorf("limit = %q", sq.Get("limit"))
	}
	assertIsError(t, callTool(t, handleSearchAutocomplete(sc), map[string]any{"term": "inv", "limit": "lots"}))

	dc, dq := captureQuery(t, "/api/documents/", `{"count":0,"results":[]}`)
	assertNotError(t, callTool(t, handleDocumentList(dc), map[string]any{"tags_id_all": "[3, 4]"}))
	if dq.Get("tags__id__all") != "3,4" {
		t.Errorf("tags__id__all = %q", dq.Get("tags__id__all"))
	}
	for _, v := range []string{"[]", "[3,"} {
		assertIsError(t, callTool(t, handleDocumentList(dc), map[string]any{"tags_id_all": v}))
	}
}

func TestOptionalIntsInBodies(t *testing.T) {
	client, got := captureJSONBody(t, "PATCH", "/api/mail_accounts/1/")
	assertNotError(t, callTool(t, handleMailAccountUpdate(client), map[string]any{"id": float64(1), "imap_port": "143"}))
	if (*got)["imap_port"] != float64(143) {
		t.Errorf("imap_port = %v", (*got)["imap_port"])
	}
	assertIsError(t, callTool(t, handleMailAccountUpdate(client), map[string]any{"id": float64(1), "imap_port": "imap"}))

	for in, want := range map[any]string{true: "archive", false: "original", "false": "original", float64(0): "original"} {
		client, got := captureJSONBody(t, "POST", "/api/share_links/")
		assertNotError(t, callTool(t, handleShareLinkCreate(client), map[string]any{"document": float64(1), "file_version_archive": in}))
		if (*got)["file_version"] != want {
			t.Errorf("file_version_archive %v: file_version = %v, want %s", in, (*got)["file_version"], want)
		}
	}
	client, got = captureJSONBody(t, "POST", "/api/share_links/")
	assertNotError(t, callTool(t, handleShareLinkCreate(client), map[string]any{"document": float64(1), "file_version_archive": ""}))
	if _, ok := (*got)["file_version"]; ok {
		t.Errorf("padded \"\" sent file_version: %v", *got)
	}

	// Archive serial numbers go up to 0xFFFFFFFF.
	client, got = captureJSONBody(t, "PATCH", "/api/documents/1/")
	assertNotError(t, callTool(t, handleDocumentUpdate(client), map[string]any{"id": float64(1), "archive_serial_number": float64(3000000000)}))
	if (*got)["archive_serial_number"] != float64(3000000000) {
		t.Errorf("archive_serial_number = %v", (*got)["archive_serial_number"])
	}
	assertIsError(t, callTool(t, handleDocumentUpdate(client), map[string]any{"id": float64(1), "archive_serial_number": float64(0x100000000)}))
}

func TestUploadNumericFields(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(filePath, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	var form url.Values
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/post_document/", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		form = r.MultipartForm.Value
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`"task-uuid"`))
	})
	client := testClientAndServer(t, rh)
	assertNotError(t, callTool(t, handleDocumentUpload(client), map[string]any{
		"file_path": filePath, "correspondent": "4", "document_type": "", "archive_serial_number": float64(3000000000),
	}))
	if form.Get("correspondent") != "4" || form.Get("archive_serial_number") != "3000000000" || form.Has("document_type") {
		t.Errorf("form = %v", form)
	}
	assertIsError(t, callTool(t, handleDocumentUpload(client), map[string]any{"file_path": filePath, "storage_path": "abc"}))
}

func TestRequiredIntValues(t *testing.T) {
	client := testClientAndServer(t, newRouteHandler(t))
	for _, v := range []any{"", "null", 1.5, map[string]any{}} {
		result := callTool(t, handleGetByID(client, "/api/tags/%d/"), map[string]any{"id": v})
		assertIsError(t, result)
	}
}

// An empty object is "not given"; any other brace text is not.
func TestArgIsSetEmptyObject(t *testing.T) {
	for _, v := range []any{map[string]any{}, "{}", " { } "} {
		if argIsSet(v) {
			t.Errorf("argIsSet(%q) = true", v)
		}
	}
	for _, v := range []any{"}{", "{{", map[string]any{"a": 1}} {
		if !argIsSet(v) {
			t.Errorf("argIsSet(%q) = false", v)
		}
	}
	// A padded {} alongside clear is fine; parameters: {} is not refused.
	client, got := captureJSONBody(t, "PATCH", "/api/tags/1/")
	assertNotError(t, callTool(t, handleTagUpdate(client), map[string]any{"id": float64(1), "parent": map[string]any{}, "clear": []any{"parent"}}))
	if want := map[string]any{"parent": nil}; !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}
	client, got = captureJSONBody(t, "POST", "/api/bulk_edit_objects/")
	assertNotError(t, callTool(t, handleBulkEditObjects(client), map[string]any{"object_type": "tags", "operation": "delete", "objects": []any{float64(1)}, "parameters": map[string]any{}}))
	if _, ok := (*got)["parameters"]; ok {
		t.Errorf("body = %v", *got)
	}
}

// "null" for non-free-text strings (a required log name, an ordering) means not given.
func TestNullStringForOtherStrings(t *testing.T) {
	result := callThroughMiddleware(t, testClientAndServer(t, newRouteHandler(t)), "log_get", map[string]any{"id": "null"})
	assertIsError(t, result)
	if !strings.Contains(resultText(t, result), "id is required") {
		t.Errorf("error = %s", resultText(t, result))
	}
	client, got := captureQuery(t, "/api/tags/", `{"count":0,"results":[]}`)
	assertNotError(t, callThroughMiddleware(t, client, "tag_list", map[string]any{"ordering": "null", "name": "null"}))
	if got.Has("ordering") || got.Get("name__icontains") != "null" {
		t.Errorf("query = %v", *got)
	}
}

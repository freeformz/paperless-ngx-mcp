package main

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestErrResult(t *testing.T) {
	result := errResult("something went wrong")
	if !result.IsError {
		t.Fatal("expected IsError to be true")
	}
	text := resultText(t, result)
	if text != "something went wrong" {
		t.Errorf("got %q, want %q", text, "something went wrong")
	}
}

func TestJsonResult(t *testing.T) {
	result, err := jsonResult(map[string]string{"key": "value"})
	if err != nil {
		t.Fatal(err)
	}
	m := resultJSON(t, result)
	if m["key"] != "value" {
		t.Errorf("got %v, want value", m["key"])
	}
}

func TestTruncateUTF8(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"shorter than limit", "hello", 10, "hello"},
		{"exact limit", "hello", 5, "hello"},
		{"ascii cut", "hello world", 5, "hello"},
		{"multibyte boundary", "aé", 2, "a"}, // é is 2 bytes; cutting at 2 would split it
		{"multibyte kept", "aé", 3, "aé"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateUTF8(tt.in, tt.n); got != tt.want {
				t.Errorf("truncateUTF8(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
			}
		})
	}
}

func TestGetContentSnippetLen(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		want    int
		wantErr bool
	}{
		{"absent uses default", map[string]any{}, defaultContentSnippetLen, false},
		{"explicit value", map[string]any{"content_snippet_bytes": float64(1000)}, 1000, false},
		{"zero disables truncation", map[string]any{"content_snippet_bytes": float64(0)}, 0, false},
		{"negative rejected", map[string]any{"content_snippet_bytes": float64(-1)}, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, errRes := getContentSnippetLen(makeRequest(tt.args))
			if (errRes != nil) != tt.wantErr {
				t.Fatalf("getContentSnippetLen(%v) errRes = %v, wantErr %v", tt.args, errRes, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("getContentSnippetLen(%v) = %d, want %d", tt.args, got, tt.want)
			}
		})
	}
}

func TestTruncateContentFieldsDisabled(t *testing.T) {
	long := strings.Repeat("z", 5000)
	items := []any{map[string]any{"content": long}}
	truncateContentFields(items, 0)

	doc := items[0].(map[string]any)
	if len(doc["content"].(string)) != 5000 {
		t.Errorf("content length = %d, want 5000 (truncation disabled)", len(doc["content"].(string)))
	}
	if _, ok := doc["content_truncated"]; ok {
		t.Error("content_truncated should be absent when truncation is disabled")
	}
}

func TestTruncateContentFieldsCustomLength(t *testing.T) {
	items := []any{map[string]any{"content": "abcdefghijklmnop"}}
	truncateContentFields(items, 10)

	doc := items[0].(map[string]any)
	if doc["content"] != "abcdefghij" {
		t.Errorf("content = %q, want %q", doc["content"], "abcdefghij")
	}
	if doc["content_truncated"] != true {
		t.Error("content_truncated should be true")
	}
}

func TestStripNotesFields(t *testing.T) {
	items := []any{
		map[string]any{"id": 1, "notes": []any{
			map[string]any{"id": 10, "note": "first"},
			map[string]any{"id": 11, "note": "second"},
		}},
		map[string]any{"id": 2, "notes": []any{}},
		map[string]any{"id": 3},
	}
	stripNotesFields(items)

	withNotes := items[0].(map[string]any)
	if _, ok := withNotes["notes"]; ok {
		t.Error("notes should be stripped")
	}
	if withNotes["notes_count"] != 2 {
		t.Errorf("notes_count = %v, want 2", withNotes["notes_count"])
	}

	empty := items[1].(map[string]any)
	if _, ok := empty["notes"]; ok {
		t.Error("empty notes array should be stripped")
	}
	if _, ok := empty["notes_count"]; ok {
		t.Error("notes_count should be absent for empty notes")
	}

	if _, ok := items[2].(map[string]any)["notes_count"]; ok {
		t.Error("notes_count should be absent when notes field is missing")
	}
}

func TestHandlePaginatedListStripsAllIDs(t *testing.T) {
	body := map[string]any{
		"count":   2,
		"all":     []int{1, 2, 3, 4, 5, 6, 7, 8},
		"results": []map[string]any{{"id": 1}, {"id": 2}},
	}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/correspondents/", jsonHandler(t, 200, body))
	client := testClientAndServer(t, rh)

	result := callTool(t, handlePaginatedList(client, "/api/correspondents/"), nil)
	assertNotError(t, result)
	m := resultJSON(t, result)
	if _, ok := m["all"]; ok {
		t.Error("all ID array should be stripped by default")
	}
	if m["count"] != float64(2) {
		t.Errorf("count = %v, want 2", m["count"])
	}
}

func TestRawJSONResult(t *testing.T) {
	result, err := rawJSONResult([]byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	m := resultJSON(t, result)
	if m["a"] != float64(1) {
		t.Errorf("got %v, want 1", m["a"])
	}
}

func TestRawJSONResultInvalid(t *testing.T) {
	result, err := rawJSONResult([]byte(`not json`))
	if err != nil {
		t.Fatal(err)
	}
	text := resultText(t, result)
	if text != "not json" {
		t.Errorf("got %q, want %q", text, "not json")
	}
}

func TestApiErrorResult(t *testing.T) {
	result := apiErrorResult(404, []byte(`{"detail":"Not found."}`), "GET", "/api/docs/1/")
	if !result.IsError {
		t.Fatal("expected IsError to be true")
	}
	m := resultJSON(t, result)
	if m["status_code"] != float64(404) {
		t.Errorf("status_code = %v, want 404", m["status_code"])
	}
	if m["detail"] != "Not found." {
		t.Errorf("detail = %v, want Not found.", m["detail"])
	}
}

func TestApiErrorResultPlainText(t *testing.T) {
	result := apiErrorResult(500, []byte(`server error`), "GET", "/api/status/")
	if !result.IsError {
		t.Fatal("expected IsError to be true")
	}
	m := resultJSON(t, result)
	if m["detail"] != "server error" {
		t.Errorf("detail = %v, want server error", m["detail"])
	}
}

func TestDoRequestError(t *testing.T) {
	result, err := doRequest(nil, io.ErrUnexpectedEOF, "GET", "/api/test/")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError to be true")
	}
}

func TestDoRequestHTTPError(t *testing.T) {
	resp := &http.Response{
		StatusCode: 403,
		Body:       io.NopCloser(bytes.NewBufferString(`{"detail":"Forbidden"}`)),
	}
	result, err := doRequest(resp, nil, "GET", "/api/test/")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError to be true")
	}
}

func TestDoRequestEmptyBody(t *testing.T) {
	resp := &http.Response{
		StatusCode: 204,
		Body:       io.NopCloser(bytes.NewReader(nil)),
	}
	result, err := doRequest(resp, nil, "DELETE", "/api/test/")
	if err != nil {
		t.Fatal(err)
	}
	text := resultText(t, result)
	if text != "success" {
		t.Errorf("got %q, want success", text)
	}
}

func TestDoRequestSuccess(t *testing.T) {
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewBufferString(`{"id":1}`)),
	}
	result, err := doRequest(resp, nil, "GET", "/api/test/")
	if err != nil {
		t.Fatal(err)
	}
	m := resultJSON(t, result)
	if m["id"] != float64(1) {
		t.Errorf("id = %v, want 1", m["id"])
	}
}

func TestSetJSONFieldValid(t *testing.T) {
	body := map[string]any{}
	req := makeRequest(map[string]any{"tags": "[1, 2, 3]"})
	err := setJSONField(body, req, "tags")
	if err != nil {
		t.Fatal(err)
	}
	arr, ok := body["tags"].([]any)
	if !ok {
		t.Fatal("expected array")
	}
	if len(arr) != 3 {
		t.Errorf("got %d elements, want 3", len(arr))
	}
}

func TestSetJSONFieldEmpty(t *testing.T) {
	body := map[string]any{}
	req := makeRequest(map[string]any{})
	err := setJSONField(body, req, "tags")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["tags"]; ok {
		t.Error("expected no tags key")
	}
}

func TestSetJSONFieldInvalid(t *testing.T) {
	body := map[string]any{}
	req := makeRequest(map[string]any{"tags": "not json"})
	err := setJSONField(body, req, "tags")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestWithNumberSchema(t *testing.T) {
	tool := mcp.NewTool("test_tool",
		withNumber("page_size", mcp.Description("Results per page")),
	)
	prop, ok := tool.InputSchema.Properties["page_size"].(map[string]any)
	if !ok {
		t.Fatal("expected page_size property")
	}
	types, ok := prop["type"].([]string)
	if !ok {
		t.Fatalf("expected type to be []string, got %T", prop["type"])
	}
	if len(types) != 2 || types[0] != "integer" || types[1] != "string" {
		t.Errorf("type = %v, want [integer string]", types)
	}
	if prop["pattern"] != `^-?\d+$` {
		t.Errorf("pattern = %v, want integer pattern", prop["pattern"])
	}
}

func TestWithNullableNumberSchema(t *testing.T) {
	tool := mcp.NewTool("test_tool",
		withNullableNumber("correspondent", mcp.Description("Correspondent ID")),
	)
	prop, ok := tool.InputSchema.Properties["correspondent"].(map[string]any)
	if !ok {
		t.Fatal("expected correspondent property")
	}
	types, ok := prop["type"].([]string)
	if !ok {
		t.Fatalf("expected type to be []string, got %T", prop["type"])
	}
	if len(types) != 3 || types[0] != "integer" || types[1] != "string" || types[2] != "null" {
		t.Errorf("type = %v, want [integer string null]", types)
	}
}

// Clients validate string arguments against the schema pattern before the
// handler runs, so the pattern must accept exactly the strings
// parseNullableInt accepts.
func TestNullableNumberPatternMatchesHandler(t *testing.T) {
	tool := mcp.NewTool("test_tool", withNullableNumber("owner"))
	pattern := tool.InputSchema.Properties["owner"].(map[string]any)["pattern"].(string)
	re := regexp.MustCompile(pattern)
	for _, s := range []string{"7", "-3", "0", "null", "", " ", "NULL", "Null", " 7 ", "7.9", "abc", "null ", "1e3", "+7", "007", "-0"} {
		_, err := parseNullableInt("owner", s)
		if re.MatchString(s) != (err == nil) {
			t.Errorf("%q: pattern match = %v, handler accepts = %v", s, re.MatchString(s), err == nil)
		}
	}
}

func TestWithNumberHandlerCoercion(t *testing.T) {
	// Verify GetFloat coerces string "50" to 50.0
	req := makeRequest(map[string]any{"page_size": "50"})
	v := req.GetFloat("page_size", 0)
	if v != 50.0 {
		t.Errorf("GetFloat(\"50\") = %v, want 50.0", v)
	}

	// Verify GetFloat handles actual number
	req2 := makeRequest(map[string]any{"page_size": float64(25)})
	v2 := req2.GetFloat("page_size", 0)
	if v2 != 25.0 {
		t.Errorf("GetFloat(25) = %v, want 25.0", v2)
	}
}

func TestOwnershipForwarded(t *testing.T) {
	// MCP clients send set_permissions as a JSON-encoded string or as an object.
	permsForms := map[string]any{
		"string": `{"view":{"users":[],"groups":[2]},"change":{"users":[],"groups":[2]}}`,
		"object": map[string]any{
			"view":   map[string]any{"users": []any{}, "groups": []any{float64(2)}},
			"change": map[string]any{"users": []any{}, "groups": []any{float64(2)}},
		},
	}
	cases := []struct {
		name    string
		method  string
		path    string
		handler func(*Client) server.ToolHandlerFunc
		args    map[string]any
	}{
		{"tag_create", "POST", "/api/tags/", handleTagCreate, map[string]any{"name": "x"}},
		{"tag_update", "PATCH", "/api/tags/1/", handleTagUpdate, map[string]any{"id": float64(1)}},
		{"correspondent_create", "POST", "/api/correspondents/", handleCorrespondentCreate, map[string]any{"name": "x"}},
		{"correspondent_update", "PATCH", "/api/correspondents/1/", handleCorrespondentUpdate, map[string]any{"id": float64(1)}},
		{"document_type_create", "POST", "/api/document_types/", handleDocumentTypeCreate, map[string]any{"name": "x"}},
		{"document_type_update", "PATCH", "/api/document_types/1/", handleDocumentTypeUpdate, map[string]any{"id": float64(1)}},
		{"storage_path_create", "POST", "/api/storage_paths/", handleStoragePathCreate, map[string]any{"name": "x", "path": "{{ title }}"}},
		{"storage_path_update", "PATCH", "/api/storage_paths/1/", handleStoragePathUpdate, map[string]any{"id": float64(1)}},
	}
	for _, tc := range cases {
		for form, perms := range permsForms {
			t.Run(tc.name+"/"+form, func(t *testing.T) {
				var body map[string]any
				rh := newRouteHandler(t)
				rh.Handle(tc.method, tc.path, captureJSONBody(t, &body))
				client := testClientAndServer(t, rh)

				// Ownership alone must count as a change for the update tools.
				args := map[string]any{"owner": nil, "set_permissions": perms}
				for k, v := range tc.args {
					args[k] = v
				}
				assertNotError(t, callTool(t, tc.handler(client), args))

				if owner, ok := body["owner"]; !ok || owner != nil {
					t.Errorf("owner = %v (present %v), want explicit null", owner, ok)
				}
				sp, ok := body["set_permissions"].(map[string]any)
				if !ok {
					t.Fatalf("set_permissions = %v, want object", body["set_permissions"])
				}
				if _, ok := sp["change"].(map[string]any); !ok {
					t.Errorf("set_permissions.change = %v, want object", sp["change"])
				}
			})
		}
	}
}

// Only set_permissions given: without it the update tools would report
// "no fields to update", and create would silently skip the permissions.
func TestOwnershipObjectPermissionsAlone(t *testing.T) {
	var body map[string]any
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/tags/1/", captureJSONBody(t, &body))
	client := testClientAndServer(t, rh)

	assertNotError(t, callTool(t, handleTagUpdate(client), map[string]any{
		"id":              float64(1),
		"set_permissions": map[string]any{"view": map[string]any{"groups": []any{float64(2)}}},
	}))
	if _, ok := body["set_permissions"].(map[string]any); !ok {
		t.Errorf("set_permissions = %v, want object", body["set_permissions"])
	}
}

func TestSetJSONObjectField(t *testing.T) {
	tests := []struct {
		name    string
		val     any
		wantErr bool
	}{
		{"object", map[string]any{"view": map[string]any{}}, false},
		{"json string", `{"view":{}}`, false},
		{"empty string", "", true},
		{"empty object", map[string]any{}, true},
		{"empty object string", "{}", true},
		{"null string", "null", true},
		{"array string", "[2]", true},
		{"array", []any{float64(2)}, true},
		{"number", float64(2), true},
		{"bool", true, true},
		{"invalid json", "{view", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]any{}
			err := setJSONObjectField(body, makeRequest(map[string]any{"set_permissions": tt.val}), "set_permissions")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, body = %v", body)
				}
				if _, ok := body["set_permissions"]; ok {
					t.Error("field must not be set on error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if _, ok := body["set_permissions"].(map[string]any); !ok {
				t.Errorf("set_permissions = %v, want object", body["set_permissions"])
			}
		})
	}
}

func TestSetJSONObjectFieldAbsentOrNull(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"absent": {},
		"null":   {"set_permissions": nil},
	} {
		t.Run(name, func(t *testing.T) {
			body := map[string]any{}
			if err := setJSONObjectField(body, makeRequest(args), "set_permissions"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if _, ok := body["set_permissions"]; ok {
				t.Error("set_permissions must be omitted")
			}
		})
	}
}

func TestWithJSONObjectSchema(t *testing.T) {
	tool := mcp.NewTool("test_tool", withOwnership())
	prop, ok := tool.InputSchema.Properties["set_permissions"].(map[string]any)
	if !ok {
		t.Fatal("expected set_permissions property")
	}
	types, ok := prop["type"].([]string)
	if !ok {
		t.Fatalf("expected type to be []string, got %T", prop["type"])
	}
	if len(types) != 2 || types[0] != "object" || types[1] != "string" {
		t.Errorf("type = %v, want [object string]", types)
	}
}

func TestSetNullableInt(t *testing.T) {
	tests := []struct {
		name    string
		val     any
		want    any
		wantErr bool
	}{
		{"json null", nil, nil, false},
		{"string null", "null", nil, false},
		{"empty string", "", nil, true},
		{"blank string", "  ", nil, true},
		{"string NULL", "NULL", nil, true},
		{"padded integer string", " 7 ", nil, true},
		{"plus sign", "+7", nil, true},
		{"number", float64(5), 5, false},
		{"integer string", "7", 7, false},
		{"non-numeric string", "abc", nil, true},
		{"whole float", float64(7.0), 7, false},
		{"int", 4, 4, false},
		{"fraction", 7.9, nil, true},
		{"fraction string", "7.9", nil, true},
		{"bool", true, nil, true},
		{"object", map[string]any{}, nil, true},
		{"array", []any{float64(3)}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{"owner": tt.val}
			req := mcp.CallToolRequest{}
			req.Params.Arguments = args
			body := map[string]any{}
			err := setNullableInt(body, args, "owner")
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if _, ok := body["owner"]; ok {
					t.Errorf("owner set to %v on error", body["owner"])
				}
				return
			}
			if v, ok := body["owner"]; !ok || v != tt.want {
				t.Errorf("owner = %v (present %v), want %v", v, ok, tt.want)
			}
		})
	}
}

func TestSetNullableIntAbsent(t *testing.T) {
	body := map[string]any{}
	if err := setNullableInt(body, map[string]any{}, "owner"); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["owner"]; ok {
		t.Error("owner must stay absent")
	}
}

// An owner that isn't a whole number must be refused before any request:
// 0 is an invalid pk, and a truncated 7.9 would hand the object to user 7.
func TestOwnershipRejectsNonIntegerOwner(t *testing.T) {
	for name, owner := range map[string]any{
		"fraction": 7.9,
		"bool":     true,
		"object":   map[string]any{},
		"array":    []any{float64(3)},
	} {
		t.Run(name, func(t *testing.T) {
			client := testClientAndServer(t, newRouteHandler(t))
			assertIsError(t, callTool(t, handleTagUpdate(client), map[string]any{"id": float64(1), "owner": owner}))
			assertIsError(t, callTool(t, handleBulkEditObjects(client), map[string]any{
				"object_type": "tags", "objects": "[1]", "operation": "set_permissions", "owner": owner,
			}))
		})
	}
}

// Hooks let a tool add its own query parameters without a copy of the
// generic handler; without hooks the request carries no query at all.
func TestGenericReadHandlerHooks(t *testing.T) {
	var listQuery, getQuery string
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		listQuery = r.URL.RawQuery
		jsonHandler(t, 200, paginatedResponse([]any{}, 0))(w, r)
	})
	rh.Handle("GET", "/api/tags/1/", func(w http.ResponseWriter, r *http.Request) {
		getQuery = r.URL.RawQuery
		jsonHandler(t, 200, map[string]any{"id": 1})(w, r)
	})
	client := testClientAndServer(t, rh)
	client.cache = nil

	assertNotError(t, callTool(t, handleGetByID(client, "/api/tags/%d/"), map[string]any{"id": float64(1), "full_perms": true}))
	if getQuery != "" {
		t.Errorf("get without hooks sent query %q", getQuery)
	}

	hook := func(params url.Values, _ mcp.CallToolRequest) { params.Set("x", "1") }
	assertNotError(t, callTool(t, handleGetByID(client, "/api/tags/%d/", addFullPerms, hook), map[string]any{"id": float64(1), "full_perms": true}))
	if getQuery != "full_perms=true&x=1" {
		t.Errorf("get query = %q, want full_perms=true&x=1", getQuery)
	}

	assertNotError(t, callTool(t, handlePaginatedList(client, "/api/tags/", addFullPerms), map[string]any{"page": float64(2), "full_perms": true}))
	if listQuery != "full_perms=true&page=2" {
		t.Errorf("list query = %q, want full_perms=true&page=2", listQuery)
	}
}

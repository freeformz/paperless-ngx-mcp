package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// callThroughMiddleware calls a registered tool the way the server does,
// with dropNullArgs in front of its handler.
func callThroughMiddleware(t *testing.T, client *Client, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	srv := NewServer(client, nil)
	tool := srv.GetTool(name)
	if tool == nil {
		t.Fatalf("no tool %s", name)
	}
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	result, err := dropNullArgs(srv)(tool.Handler)(context.Background(), req)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return result
}

func TestSelectionFiltersSerialiseAsStrings(t *testing.T) {
	client, got := captureJSONBody(t, "POST", "/api/documents/selection_data/")
	assertNotError(t, callTool(t, handleDocumentSelectionData(client), map[string]any{
		"all": true,
		"filters": map[string]any{
			"is_in_inbox": true, "tags_id_all": []any{float64(1), float64(2)}, "correspondent_id": float64(7),
			"created_before": "2020-01-05", "title": "invoice",
			"custom_field_query": []any{"AND", []any{[]any{float64(1), "exists", true}}},
		},
		"excluded_documents": "[9]",
	}))
	want := map[string]any{
		"all": true,
		"filters": map[string]any{
			"is_in_inbox": "true", "tags__id__all": "1,2", "correspondent__id": "7",
			"created__date__lte": "2020-01-05", "title__icontains": "invoice",
			"custom_field_query": `["AND",[[1,"exists",true]]]`,
		},
		"excluded_documents": []any{float64(9)},
	}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}
}

func TestSelectionAllWithoutFiltersSendsEmptyFilters(t *testing.T) {
	client, got := captureJSONBody(t, "POST", "/api/documents/delete/")
	assertNotError(t, callTool(t, handleDocumentAction(client, "delete", buildDeleteBody), map[string]any{"all": "true", "documents": []any{}}))
	if want := map[string]any{"all": true, "filters": map[string]any{}}; !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}
}

func TestSelectionRejectsWideningInput(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"unknown filter":         {"all": true, "filters": map[string]any{"tag_id": float64(5)}},
		"raw filter name":        {"all": true, "filters": map[string]any{"tags__id__all": "5"}},
		"bad int":                {"all": true, "filters": map[string]any{"correspondent_id": "abc"}},
		"fraction":               {"all": true, "filters": map[string]any{"correspondent_id": 1.5}},
		"bad tag list":           {"all": true, "filters": map[string]any{"tags_id_all": "a,b"}},
		"empty tag item":         {"all": true, "filters": map[string]any{"tags_id_all": "1,,2"}},
		"trailing comma":         {"all": true, "filters": map[string]any{"tags_id_in": "1,"}},
		"empty tag array":        {"all": true, "filters": map[string]any{"tags_id_none": []any{}}},
		"tag array of strings":   {"all": true, "filters": map[string]any{"tags_id_all": []any{"1"}}},
		"bool as yes":            {"all": true, "filters": map[string]any{"is_in_inbox": "yes"}},
		"bool as 1":              {"all": true, "filters": map[string]any{"is_tagged": float64(1)}},
		"impossible date":        {"all": true, "filters": map[string]any{"created_after": "2024-02-30"}},
		"year zero":              {"all": true, "filters": map[string]any{"added_before": "0000-01-01"}},
		"date shape":             {"all": true, "filters": map[string]any{"added_after": "01/02/2024"}},
		"blank title":            {"all": true, "filters": map[string]any{"title": "   "}},
		"separator-only title":   {"all": true, "filters": map[string]any{"title": "\x1f"}},
		"NUL in content":         {"all": true, "filters": map[string]any{"content": "a\x00b"}},
		"star query":             {"all": true, "filters": map[string]any{"query": " * "}},
		"wildcards query":        {"all": true, "filters": map[string]any{"query": "** ?"}},
		"long query":             {"all": true, "filters": map[string]any{"query": strings.Repeat("a", maxQueryLen+1)}},
		"query and more_like_id": {"all": true, "filters": map[string]any{"query": "x", "more_like_id": float64(3)}},
		"null filter value":      {"all": true, "filters": map[string]any{"query": nil}},
		"filters as array":       {"all": true, "filters": "[]"},
		"filters as number":      {"all": true, "filters": "5"},
		"bad custom field query": {"all": true, "filters": map[string]any{"custom_field_query": "{"}},
		"custom field object":    {"all": true, "filters": map[string]any{"custom_field_query": map[string]any{}}},
		"all with documents":     {"all": true, "documents": []any{float64(1)}},
		"all as 1":               {"all": float64(1)},
		"all as t":               {"all": "t"},
		"filters without all":    {"documents": []any{float64(1)}, "filters": map[string]any{"title": "x"}},
		"excluded without all":   {"documents": []any{float64(1)}, "excluded_documents": []any{float64(2)}},
		"duplicate excluded":     {"all": true, "excluded_documents": []any{float64(2), float64(2)}},
		"no selection":           {},
		"empty documents":        {"documents": []any{}},
		"duplicate documents":    {"documents": []any{float64(1), float64(1)}},
	} {
		t.Run(name, func(t *testing.T) {
			client := testClientAndServer(t, newRouteHandler(t))
			assertIsError(t, callTool(t, handleDocumentSelectionData(client), args))
		})
	}
}

func TestSelectionToolsAcceptAll(t *testing.T) {
	cases := map[string]struct {
		path string
		call func(*Client) *mcp.CallToolResult
	}{
		"document_bulk_edit": {"/api/documents/bulk_edit/", func(c *Client) *mcp.CallToolResult {
			return callTool(t, handleDocumentBulkEdit(c), map[string]any{"all": true, "filters": map[string]any{"title": "x"}, "method": "add_tag", "parameters": map[string]any{"tag": float64(1)}})
		}},
		"document_rotate": {"/api/documents/rotate/", func(c *Client) *mcp.CallToolResult {
			return callTool(t, handleDocumentAction(c, "rotate", buildRotateBody), map[string]any{"all": true, "filters": map[string]any{"title": "x"}, "degrees": float64(90), "source_mode": "latest_version"})
		}},
		"document_reprocess": {"/api/documents/reprocess/", func(c *Client) *mcp.CallToolResult {
			return callTool(t, handleDocumentAction(c, "reprocess", buildReprocessBody), map[string]any{"all": true, "filters": map[string]any{"title": "x"}})
		}},
		"document_bulk_delete": {"/api/documents/delete/", func(c *Client) *mcp.CallToolResult {
			return callTool(t, handleDocumentAction(c, "delete", buildDeleteBody), map[string]any{"all": true, "filters": map[string]any{"title": "x"}})
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client, got := captureJSONBody(t, "POST", tc.path)
			assertNotError(t, tc.call(client))
			if (*got)["all"] != true || !reflect.DeepEqual((*got)["filters"], map[string]any{"title__icontains": "x"}) {
				t.Errorf("body = %v, want all with the mapped filters", *got)
			}
			if _, ok := (*got)["documents"]; ok {
				t.Errorf("body = %v, want no documents with all", *got)
			}
		})
	}
}

func TestExplicitOnlyActionsRefuseSelection(t *testing.T) {
	for _, op := range []string{"merge", "edit_pdf", "remove_password"} {
		for _, arg := range []map[string]any{{"all": true}, {"filters": map[string]any{"title": "x"}}, {"excluded_documents": []any{float64(3)}}} {
			args := map[string]any{"documents": []any{float64(1), float64(2)}, "password": "p", "operations": []any{map[string]any{"page": float64(1)}}}
			for k, v := range arg {
				args[k] = v
			}
			client := testClientAndServer(t, newRouteHandler(t))
			result := callTool(t, documentActionHandlerFor(client, op), args)
			assertIsError(t, result)
			if !strings.Contains(resultText(t, result), "doesn't support") {
				t.Errorf("%s with %v: error = %s", op, arg, resultText(t, result))
			}
		}
	}
}

func TestSelectionWaitKeepsFollowOn(t *testing.T) {
	fastPolling(t)
	for _, op := range []string{"rotate", "reprocess"} {
		t.Run(op, func(t *testing.T) {
			client, _, _ := actionServer(t, documentActionPaths[op], "pending", "success")
			args := map[string]any{"all": true, "filters": map[string]any{"title": "x"}, "wait": true}
			if op == "rotate" {
				args["degrees"] = float64(90)
			}
			result := callTool(t, documentActionHandlerFor(client, op), args)
			assertNotError(t, result)
			if resultJSON(t, result)["follow_on"] == nil {
				t.Errorf("%s with all: want a follow_on", op)
			}
		})
	}
}

func TestDocumentListParamsUnchanged(t *testing.T) {
	var got url.Values
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/", func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(paginatedResponse([]any{}, 0))
	})
	client := testClientAndServer(t, rh)
	assertNotError(t, callTool(t, handleDocumentList(client), map[string]any{
		"query": "invoice", "correspondent_id": "5", "document_type_id": float64(6), "storage_path_id": float64(7),
		"tags_id_all": "1, 2", "tags_id_none": "3", "tags_id_in": "4", "is_tagged": "true", "is_in_inbox": false,
		"title": "a", "content": "b", "custom_field_query": `["AND",[[1,"exists",true]]]`,
		"created_after": "2020-01-01", "created_before": "2021-01-01", "added_after": "2022-01-01", "added_before": "2023-01-01",
		"owner_id": float64(2), "ordering": "-created", "page": float64(2), "page_size": float64(10),
		// Not given: null, "null" for non-text kinds, and "".
		"more_like_id": nil, "is_tagged_x": "null",
	}))
	want := url.Values{
		"query": {"invoice"}, "correspondent__id": {"5"}, "document_type__id": {"6"}, "storage_path__id": {"7"},
		"tags__id__all": {"1,2"}, "tags__id__none": {"3"}, "tags__id__in": {"4"}, "is_tagged": {"true"}, "is_in_inbox": {"false"},
		"title__icontains": {"a"}, "content__icontains": {"b"}, "custom_field_query": {`["AND",[[1,"exists",true]]]`},
		"created__date__gte": {"2020-01-01"}, "created__date__lte": {"2021-01-01"}, "added__date__gte": {"2022-01-01"}, "added__date__lte": {"2023-01-01"},
		"owner__id": {"2"}, "ordering": {"-created"}, "page": {"2"}, "page_size": {"10"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("query = %v\nwant    %v", got, want)
	}
}

func TestDocumentListLenientNulls(t *testing.T) {
	var got url.Values
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/", func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(paginatedResponse([]any{}, 0))
	})
	client := testClientAndServer(t, rh)
	assertNotError(t, callTool(t, handleDocumentList(client), map[string]any{
		"correspondent_id": nil, "is_in_inbox": nil, "owner_id": "null", "tags_id_all": "", "created_after": "null", "title": "",
	}))
	if len(got) != 0 {
		t.Errorf("query = %v, want no filters", got)
	}
	for name, args := range map[string]map[string]any{
		"bool yes": {"is_in_inbox": "yes"}, "int abc": {"correspondent_id": "abc"}, "bad tags": {"tags_id_all": "1,x"},
		"bad date": {"created_after": "2024-13-01"}, "query and more_like": {"query": "x", "more_like_id": float64(1)},
	} {
		t.Run(name, func(t *testing.T) {
			assertIsError(t, callTool(t, handleDocumentList(testClientAndServer(t, newRouteHandler(t))), args))
		})
	}
}

func TestDocumentListFilterSchemaIsTheTable(t *testing.T) {
	props := NewServer(NewClient("http://unused", "unused"), nil).GetTool("document_list").Tool.InputSchema.Properties
	for _, p := range documentFilterParams {
		if _, ok := props[p.name]; !ok {
			t.Errorf("document_list lacks filter %s", p.name)
		}
	}
	sel := NewServer(NewClient("http://unused", "unused"), nil).GetTool("document_bulk_delete").Tool.InputSchema.Properties["filters"].(map[string]any)
	if sel["additionalProperties"] != false || len(sel["properties"].(map[string]any)) != len(documentFilterParams) {
		t.Errorf("filters schema = %v", sel)
	}
}

func TestBulkEditObjectsPermissionsTopLevel(t *testing.T) {
	client, got := captureJSONBody(t, "POST", "/api/bulk_edit_objects/")
	assertNotError(t, callTool(t, handleBulkEditObjects(client), map[string]any{
		"object_type": "tags", "operation": "set_permissions", "objects": []any{float64(1)},
		"owner": float64(3), "permissions": `{"view": {"users": [2], "groups": []}}`, "merge": true,
	}))
	want := map[string]any{
		"object_type": "tags", "operation": "set_permissions", "objects": []any{float64(1)},
		"owner": float64(3), "permissions": map[string]any{"view": map[string]any{"users": []any{float64(2)}, "groups": []any{}}}, "merge": true,
	}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}
}

func TestBulkEditObjectsClearOwnerAndAll(t *testing.T) {
	client, got := captureJSONBody(t, "POST", "/api/bulk_edit_objects/")
	assertNotError(t, callTool(t, handleBulkEditObjects(client), map[string]any{
		"object_type": "storage_paths", "operation": "set_permissions", "all": true,
		"filters": map[string]any{"path": "{{ correspondent }}"}, "clear": "owner",
	}))
	want := map[string]any{
		"object_type": "storage_paths", "operation": "set_permissions", "all": true,
		"filters": map[string]any{"path__icontains": "{{ correspondent }}"}, "owner": nil,
	}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}
}

func TestBulkEditObjectsRejects(t *testing.T) {
	base := func(extra map[string]any) map[string]any {
		m := map[string]any{"object_type": "tags", "operation": "set_permissions", "objects": []any{float64(1)}, "owner": float64(2)}
		for k, v := range extra {
			if v == nil {
				delete(m, k)
			} else {
				m[k] = v
			}
		}
		return m
	}
	for name, args := range map[string]map[string]any{
		"old parameters":          base(map[string]any{"parameters": map[string]any{"owner": float64(1)}}),
		"bad object_type":         base(map[string]any{"object_type": "saved_views"}),
		"bad operation":           base(map[string]any{"operation": "rename"}),
		"no objects":              base(map[string]any{"objects": nil}),
		"duplicate objects":       base(map[string]any{"objects": []any{float64(1), float64(1)}}),
		"all with objects":        base(map[string]any{"all": true}),
		"filters without all":     base(map[string]any{"filters": map[string]any{"name": "x"}}),
		"path filter on tags":     base(map[string]any{"objects": nil, "all": true, "filters": map[string]any{"path": "x"}}),
		"blank name filter":       base(map[string]any{"objects": nil, "all": true, "filters": map[string]any{"name": " "}}),
		"nothing to set":          base(map[string]any{"owner": nil}),
		"owner and clear owner":   base(map[string]any{"clear": []any{"owner"}}),
		"clear owner with merge":  base(map[string]any{"owner": nil, "clear": []any{"owner"}, "merge": true}),
		"empty permissions":       base(map[string]any{"permissions": map[string]any{}}),
		"empty action":            base(map[string]any{"permissions": map[string]any{"view": map[string]any{}}}),
		"unknown action":          base(map[string]any{"permissions": map[string]any{"edit": map[string]any{"users": []any{}}}}),
		"unknown permissions key": base(map[string]any{"permissions": map[string]any{"view": map[string]any{"user": []any{}}}}),
		"string ids":              base(map[string]any{"permissions": map[string]any{"view": map[string]any{"users": []any{"1"}}}}),
		"delete with owner":       base(map[string]any{"operation": "delete"}),
	} {
		t.Run(name, func(t *testing.T) {
			assertIsError(t, callTool(t, handleBulkEditObjects(testClientAndServer(t, newRouteHandler(t))), args))
		})
	}
}

func TestTrashActionSelection(t *testing.T) {
	client, got := captureJSONBody(t, "POST", "/api/trash/")
	assertNotError(t, callTool(t, handleTrashAction(client), map[string]any{"action": "empty", "all": true, "documents": "[]"}))
	if want := map[string]any{"action": "empty"}; !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}
	client, got = captureJSONBody(t, "POST", "/api/trash/")
	assertNotError(t, callTool(t, handleTrashAction(client), map[string]any{"action": "restore", "documents": "[4, 5]"}))
	if want := map[string]any{"action": "restore", "documents": []any{float64(4), float64(5)}}; !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}
	for name, args := range map[string]map[string]any{
		"empty without documents": {"action": "empty"},
		"empty with null":         {"action": "empty", "documents": nil},
		"empty with \"null\"":     {"action": "empty", "documents": "null"},
		"empty with \"\"":         {"action": "empty", "documents": ""},
		"empty with []":           {"action": "empty", "documents": []any{}},
		"restore without":         {"action": "restore"},
		"restore with all":        {"action": "restore", "all": true},
		"all and documents":       {"action": "empty", "all": true, "documents": []any{float64(1)}},
		"bad action":              {"action": "purge", "documents": []any{float64(1)}},
		"all as 1":                {"action": "empty", "all": float64(1)},
	} {
		t.Run(name, func(t *testing.T) {
			assertIsError(t, callTool(t, handleTrashAction(testClientAndServer(t, newRouteHandler(t))), args))
		})
	}
}

func TestStoragePathListPathFilter(t *testing.T) {
	var got string
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/storage_paths/", func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("path__icontains")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(paginatedResponse([]any{}, 0))
	})
	assertNotError(t, callTool(t, handleStoragePathList(testClientAndServer(t, rh)), map[string]any{"path": "{{ title }}"}))
	if got != "{{ title }}" {
		t.Errorf("path__icontains = %q", got)
	}
}

func TestClear(t *testing.T) {
	client, got := captureJSONBody(t, "PATCH", "/api/tags/3/")
	assertNotError(t, callTool(t, handleTagUpdate(client), map[string]any{"id": float64(3), "clear": "parent"}))
	if want := map[string]any{"parent": nil}; !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}
	for name, args := range map[string]map[string]any{
		"unknown field":   {"id": float64(1), "clear": []any{"title"}},
		"set and cleared": {"id": float64(1), "correspondent": float64(2), "clear": []any{"correspondent"}},
		"not names":       {"id": float64(1), "clear": []any{float64(1)}},
		"clear only []":   {"id": float64(1), "clear": []any{}},
	} {
		t.Run(name, func(t *testing.T) {
			assertIsError(t, callTool(t, handleDocumentUpdate(testClientAndServer(t, newRouteHandler(t))), args))
		})
	}
	// display_fields [] and clear both mean "use the defaults".
	client, got = captureJSONBody(t, "PATCH", "/api/saved_views/1/")
	assertNotError(t, callTool(t, handleSavedViewUpdate(client), map[string]any{"id": float64(1), "display_fields": []any{}, "clear": []any{"display_fields"}}))
	if want := map[string]any{"display_fields": nil}; !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}
}

// TestNullPaddingIsIgnored sends the nulls a padding client adds to the
// handlers where a null used to become a real value.
func TestNullPaddingIsIgnored(t *testing.T) {
	cases := []struct {
		tool, method, path string
		args               map[string]any
		want               map[string]any
	}{
		{"document_update", "PATCH", "/api/documents/1/", map[string]any{"id": float64(1), "title": "T", "content": nil, "correspondent": nil, "owner": "null", "tags": "null", "custom_fields": nil, "clear": nil}, map[string]any{"title": "T"}},
		{"user_update", "PATCH", "/api/users/1/", map[string]any{"id": float64(1), "first_name": "A", "is_staff": nil, "is_superuser": nil, "is_active": "null", "groups": "null"}, map[string]any{"first_name": "A"}},
		{"tag_create", "POST", "/api/tags/", map[string]any{"name": "n", "matching_algorithm": nil, "is_insensitive": nil, "is_inbox_tag": "null", "parent": nil}, map[string]any{"name": "n"}},
		{"tag_update", "PATCH", "/api/tags/1/", map[string]any{"id": float64(1), "name": "n", "parent": nil, "clear": "null"}, map[string]any{"name": "n"}},
		{"correspondent_update", "PATCH", "/api/correspondents/1/", map[string]any{"id": float64(1), "name": "n", "matching_algorithm": nil, "is_insensitive": nil}, map[string]any{"name": "n"}},
		{"saved_view_update", "PATCH", "/api/saved_views/1/", map[string]any{"id": float64(1), "name": "n", "page_size": nil, "display_mode": nil, "display_fields": "null", "sort_reverse": nil}, map[string]any{"name": "n"}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			client, got := captureJSONBody(t, tc.method, tc.path)
			assertNotError(t, callThroughMiddleware(t, client, tc.tool, tc.args))
			if !reflect.DeepEqual(*got, tc.want) {
				t.Errorf("body = %v, want %v", *got, tc.want)
			}
		})
	}
}

func TestNullPaddingInListFilters(t *testing.T) {
	var got url.Values
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tasks/", func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(paginatedResponse([]any{}, 0))
	})
	assertNotError(t, callThroughMiddleware(t, testClientAndServer(t, rh), "task_list", map[string]any{"acknowledged": nil, "is_complete": "null", "owner": nil}))
	for _, k := range []string{"acknowledged", "is_complete", "owner"} {
		if got.Has(k) {
			t.Errorf("query = %v, want no %s", got, k)
		}
	}
}

func TestPlainStringKeepsNullText(t *testing.T) {
	client, got := captureJSONBody(t, "PATCH", "/api/documents/1/")
	assertNotError(t, callThroughMiddleware(t, client, "document_update", map[string]any{"id": float64(1), "title": "null"}))
	if want := map[string]any{"title": "null"}; !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}
}

func TestMiddlewareIsWiredIn(t *testing.T) {
	client, got := captureJSONBody(t, "PATCH", "/api/documents/1/")
	srv := NewServer(client, nil)
	msg := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"document_update","arguments":{"id":1,"title":"T","content":null}}}`
	if resp := srv.HandleMessage(context.Background(), json.RawMessage(msg)); resp == nil {
		t.Fatal("no response")
	}
	if want := map[string]any{"title": "T"}; !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v (content: null must not wipe the content)", *got, want)
	}
}

func TestSelectionEdgeCases(t *testing.T) {
	// A long non-ASCII query is measured in characters, like the server does.
	client, got := captureJSONBody(t, "POST", "/api/documents/selection_data/")
	q := strings.Repeat("é", 2100)
	assertNotError(t, callTool(t, handleDocumentSelectionData(client), map[string]any{"all": true, "filters": map[string]any{"query": q}}))
	if (*got)["filters"].(map[string]any)["query"] != q {
		t.Errorf("query not sent")
	}
	// An empty filters object padding an explicit selection is not an error.
	client, got = captureJSONBody(t, "POST", "/api/documents/selection_data/")
	assertNotError(t, callTool(t, handleDocumentSelectionData(client), map[string]any{"documents": []any{float64(1)}, "filters": "{}", "excluded_documents": []any{}}))
	if want := map[string]any{"documents": []any{float64(1)}}; !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}
	client, got = captureJSONBody(t, "POST", "/api/bulk_edit_objects/")
	assertNotError(t, callTool(t, handleBulkEditObjects(client), map[string]any{"object_type": "tags", "operation": "delete", "objects": []any{float64(1)}, "filters": map[string]any{}}))
	if _, ok := (*got)["filters"]; ok {
		t.Errorf("body = %v, want no filters", *got)
	}
}

func TestMiddlewareSelectionAndEnums(t *testing.T) {
	// all: true with every other selection param padded with null.
	client, got := captureJSONBody(t, "POST", "/api/documents/delete/")
	srv := NewServer(client, nil)
	msg := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"document_bulk_delete","arguments":{"all":true,"documents":null,"filters":null,"excluded_documents":"null","wait":null,"timeout":null}}}`
	srv.HandleMessage(context.Background(), json.RawMessage(msg))
	if want := map[string]any{"all": true, "filters": map[string]any{}}; !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}
	// "null" for an enum string param means not given.
	result := callThroughMiddleware(t, testClientAndServer(t, newRouteHandler(t)), "trash_action", map[string]any{"action": "null", "documents": []any{float64(1)}})
	assertIsError(t, result)
	if !strings.Contains(resultText(t, result), "action is required") {
		t.Errorf("error = %s", resultText(t, result))
	}
	// clear given as null or "null" is a no-op.
	for _, c := range []any{nil, "null"} {
		client, got := captureJSONBody(t, "PATCH", "/api/tags/1/")
		assertNotError(t, callThroughMiddleware(t, client, "tag_update", map[string]any{"id": float64(1), "name": "n", "clear": c}))
		if want := map[string]any{"name": "n"}; !reflect.DeepEqual(*got, want) {
			t.Errorf("clear %v: body = %v, want %v", c, *got, want)
		}
	}
	// document_list lenient mode still rejects a bad date and a fractional id.
	for _, args := range []map[string]any{{"created_after": "2024-02-30"}, {"correspondent_id": 1.5}} {
		assertIsError(t, callThroughMiddleware(t, testClientAndServer(t, newRouteHandler(t)), "document_list", args))
	}
}

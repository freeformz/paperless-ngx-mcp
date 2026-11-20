package main

import (
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

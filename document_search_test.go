package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestDocumentListSearchParams(t *testing.T) {
	for _, tc := range []struct {
		args map[string]any
		want map[string]string
	}{
		{map[string]any{"text": "invoice acme"}, map[string]string{"text": "invoice acme"}},
		{map[string]any{"title_search": "invoice"}, map[string]string{"title_search": "invoice"}},
		{map[string]any{"text": "invoice", "query": "tag:unpaid"}, map[string]string{"text": "invoice", "query": "tag:unpaid"}},
		{map[string]any{"title_search": "invoice", "query": "added:[-1 week to now]"}, map[string]string{"title_search": "invoice", "query": "added:[-1 week to now]"}},
		{map[string]any{"has_duplicates": true}, map[string]string{"has_duplicates": "true"}},
		{map[string]any{"has_duplicates": float64(0)}, map[string]string{"has_duplicates": "false"}},
		// Padding beside a search doesn't count as a second one.
		{map[string]any{"text": "", "title_search": "  ", "query": "x"}, map[string]string{"query": "x"}},
		{map[string]any{"text": "invoice", "ordering": " -score "}, map[string]string{"text": "invoice", "ordering": "-score"}},
		{map[string]any{"query": "x", "ordering": "created"}, map[string]string{"query": "x", "ordering": "created"}},
		{map[string]any{"ordering": "-created"}, map[string]string{"ordering": "-created"}},
	} {
		client, got := captureQuery(t, "/api/documents/", `{"count":0,"results":[]}`)
		assertNotError(t, callThroughMiddleware(t, client, "document_list", tc.args))
		for k, v := range tc.want {
			if got.Get(k) != v {
				t.Errorf("%v: %s = %q, want %q (query %v)", tc.args, k, got.Get(k), v, *got)
			}
		}
		for _, k := range []string{"text", "title_search", "query", "ordering"} {
			if _, want := tc.want[k]; !want && got.Has(k) {
				t.Errorf("%v: %s sent, want it left out", tc.args, k)
			}
		}
	}
}

func TestDocumentListRefusesSearchMisuse(t *testing.T) {
	long := strings.Repeat("a", maxQueryLen+1)
	for name, tc := range map[string]struct {
		args map[string]any
		want string
	}{
		"text and title_search":      {map[string]any{"text": "a", "title_search": "b"}, "use only one of text, title_search, query and more_like_id (got text, title_search)"},
		"text and more_like_id":      {map[string]any{"text": "a", "more_like_id": float64(3)}, "use only one of"},
		"title and more_like_id":     {map[string]any{"title_search": "a", "more_like_id": float64(3)}, "use only one of"},
		"query and more_like_id":     {map[string]any{"query": "a", "more_like_id": float64(3)}, "use only one of"},
		"three":                      {map[string]any{"text": "a", "title_search": "b", "query": "c"}, "use only one of"},
		"no words":                   {map[string]any{"text": "!!! --"}, "text must be text with searchable words"},
		"title no words":             {map[string]any{"title_search": "*"}, "title_search must be text with searchable words"},
		"long text":                  {map[string]any{"text": long}, "text must be at most 4096 characters"},
		"long title_search":          {map[string]any{"title_search": long}, "at most 4096"},
		"long query":                 {map[string]any{"query": long}, "query must be at most 4096"},
		"score without search":       {map[string]any{"ordering": "-score"}, "ordering by score needs a search"},
		"score with other fields":    {map[string]any{"text": "a", "ordering": "-score,created"}, "score can't be combined"},
		"ordering with more_like_id": {map[string]any{"more_like_id": float64(3), "ordering": "created"}, "ordering is ignored with more_like_id"},
	} {
		t.Run(name, func(t *testing.T) {
			result := callThroughMiddleware(t, testClientAndServer(t, newRouteHandler(t)), "document_list", tc.args)
			assertIsError(t, result)
			if text := resultText(t, result); !strings.Contains(text, tc.want) {
				t.Errorf("error = %q, want it to contain %q", text, tc.want)
			}
		})
	}
}

func TestSelectionSearchFilters(t *testing.T) {
	for _, tc := range []struct {
		filters map[string]any
		want    map[string]any
	}{
		{map[string]any{"text": "invoice"}, map[string]any{"text": "invoice"}},
		{map[string]any{"title_search": "invoice", "query": "tag:x"}, map[string]any{"title_search": "invoice", "query": "tag:x"}},
		{map[string]any{"has_duplicates": true}, map[string]any{"has_duplicates": "true"}},
	} {
		client, got := captureJSONBody(t, "POST", "/api/documents/selection_data/")
		assertNotError(t, callTool(t, handleDocumentSelectionData(client), map[string]any{"all": true, "filters": tc.filters}))
		if want := map[string]any{"all": true, "filters": tc.want}; !reflect.DeepEqual(*got, want) {
			t.Errorf("body = %v, want %v", *got, want)
		}
	}
}

// Selection schemas repeat the filter table in several tools, so the long
// descriptions stay on document_list.
func TestSelectionFilterDescriptionsAreShort(t *testing.T) {
	srv := NewServer(NewClient("http://unused", "unused"), nil)
	list := srv.GetTool("document_list").Tool.InputSchema.Properties
	sel := srv.GetTool("document_bulk_delete").Tool.InputSchema.Properties["filters"].(map[string]any)["properties"].(map[string]any)
	for name, short := range selectionFilterDesc {
		if got := sel[name].(map[string]any)["description"]; got != short {
			t.Errorf("filters.%s description = %v, want %q", name, got, short)
		}
		if desc := list[name].(map[string]any)["description"].(string); len(desc) < len(short) {
			t.Errorf("document_list %s description = %q, want the full one", name, desc)
		}
	}
}

func TestServerInstructionsSearchSyntax(t *testing.T) {
	for _, want := range []string{"AND, OR and NOT in capitals", "does NOT exclude", "notes.note:", "custom_fields.value:", "barcodes:", "added:[-1 week to now]", "old note: and custom_field: prefixes", "has_duplicates", "10 levels deep"} {
		if !strings.Contains(serverInstructions, want) {
			t.Errorf("server instructions lack %q", want)
		}
	}
}

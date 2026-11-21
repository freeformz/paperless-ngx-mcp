package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

type matchingTool struct {
	tool, method, path string
	handler            func(*Client) server.ToolHandlerFunc
	base               map[string]any // required arguments
	create             bool
}

var matchingTools = []matchingTool{
	{"tag_create", "POST", "/api/tags/", handleTagCreate, map[string]any{"name": "n"}, true},
	{"tag_update", "PATCH", "/api/tags/1/", handleTagUpdate, map[string]any{"id": float64(1), "name": "n"}, false},
	{"correspondent_create", "POST", "/api/correspondents/", handleCorrespondentCreate, map[string]any{"name": "n"}, true},
	{"correspondent_update", "PATCH", "/api/correspondents/1/", handleCorrespondentUpdate, map[string]any{"id": float64(1), "name": "n"}, false},
	{"document_type_create", "POST", "/api/document_types/", handleDocumentTypeCreate, map[string]any{"name": "n"}, true},
	{"document_type_update", "PATCH", "/api/document_types/1/", handleDocumentTypeUpdate, map[string]any{"id": float64(1), "name": "n"}, false},
	{"storage_path_create", "POST", "/api/storage_paths/", handleStoragePathCreate, map[string]any{"name": "n", "path": "p"}, true},
	{"storage_path_update", "PATCH", "/api/storage_paths/1/", handleStoragePathUpdate, map[string]any{"id": float64(1), "name": "n"}, false},
}

func withArg(base map[string]any, k string, v any) map[string]any {
	args := map[string]any{}
	for bk, bv := range base {
		args[bk] = bv
	}
	args[k] = v
	return args
}

func TestMatchingAlgorithmNames(t *testing.T) {
	cases := []struct {
		in   any
		want float64
	}{
		{"regex", 4}, {"Regular Expression", 4}, {"regular  expression", 4}, {" exact-match ", 3},
		{"literal", 3}, {"any_word", 1}, {"all", 2}, {"fuzzy", 5}, {"none", 0},
		{"auto", 6}, {"AUTOMATIC", 6},
		{float64(2), 2}, {"5", 5}, {float64(4.0), 4}, {"04", 4},
		{float64(9), 9}, {"-1", -1}, // out of range: left to paperless-ngx
	}
	for _, mt := range matchingTools {
		for _, tc := range cases {
			client, got := captureJSONBody(t, mt.method, mt.path)
			assertNotError(t, callTool(t, mt.handler(client), withArg(mt.base, "matching_algorithm", tc.in)))
			if v := (*got)["matching_algorithm"]; v != tc.want {
				t.Errorf("%s %#v: matching_algorithm = %#v, want %v", mt.tool, tc.in, v, tc.want)
			}
		}
	}
}

func TestMatchingAlgorithmNotGiven(t *testing.T) {
	for _, mt := range matchingTools {
		for _, in := range []any{nil, "", "  ", map[string]any{}, "absent"} {
			args := withArg(mt.base, "matching_algorithm", in)
			if in == "absent" {
				delete(args, "matching_algorithm")
			}
			client, got := captureJSONBody(t, mt.method, mt.path)
			assertNotError(t, callTool(t, mt.handler(client), args))
			v, ok := (*got)["matching_algorithm"]
			switch {
			case mt.create && v != float64(6):
				t.Errorf("%s %#v: matching_algorithm = %#v, want the auto default 6", mt.tool, in, v)
			case !mt.create && ok:
				t.Errorf("%s %#v: body = %v, want no matching_algorithm", mt.tool, in, *got)
			}
		}
	}
}

func TestMatchingAlgorithmRefused(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"sometimes", `matching_algorithm must be a name (none, any, all, literal, regex, fuzzy, auto) or a number, got "sometimes"`},
		{float64(1.5), "matching_algorithm must be a whole number"},
		{"1.5", "matching_algorithm must be a whole number"},
		{"99999999999", "matching_algorithm must be at most 2147483648"},
		{true, "matching_algorithm must be a name"},
		{[]any{float64(1)}, "matching_algorithm must be a name"},
	}
	none := testClientAndServer(t, newRouteHandler(t)) // any request fails the test
	for _, mt := range matchingTools {
		for _, tc := range cases {
			result := callTool(t, mt.handler(none), withArg(mt.base, "matching_algorithm", tc.in))
			assertIsError(t, result)
			if text := resultText(t, result); !strings.Contains(text, tc.want) {
				t.Errorf("%s %#v: error = %s, want %q", mt.tool, tc.in, text, tc.want)
			}
		}
	}
}

func TestMatchingAlgorithmSchema(t *testing.T) {
	srv := NewServer(nil, nil)
	for _, mt := range matchingTools {
		p, _ := srv.GetTool(mt.tool).Tool.InputSchema.Properties["matching_algorithm"].(map[string]any)
		if !reflect.DeepEqual(p["type"], []string{"integer", "string"}) {
			t.Errorf("%s: type = %v, want [integer string]", mt.tool, p["type"])
		}
		if _, ok := p["pattern"]; ok {
			t.Errorf("%s: has a pattern, which would refuse names", mt.tool)
		}
		desc, _ := p["description"].(string)
		if !strings.Contains(desc, "auto (6") || mt.create != strings.HasSuffix(desc, "default auto") {
			t.Errorf("%s: description = %q", mt.tool, desc)
		}
	}
}

// auto is only for tags, correspondents, document types and storage paths:
// workflow triggers still refuse it.
func TestWorkflowTriggerRefusesAuto(t *testing.T) {
	none := testClientAndServer(t, newRouteHandler(t))
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"workflow_create", map[string]any{"name": "w", "triggers": []any{map[string]any{"type": "manual", "matching_algorithm": "auto"}}, "actions": []any{map[string]any{"type": "assignment"}}}},
		{"workflow_trigger_update", map[string]any{"id": float64(1), "type": "manual", "matching_algorithm": "auto"}},
	} {
		result := callThroughMiddleware(t, none, tc.tool, tc.args)
		assertIsError(t, result)
		if text := resultText(t, result); !strings.Contains(text, "matching_algorithm") {
			t.Errorf("%s: error = %s, want a matching_algorithm error", tc.tool, text)
		}
	}
}

// The workflow tools read numeric strings and names the same way.
func TestNormalizeNamedNumericStrings(t *testing.T) {
	ws := mustWorkflowSchemas()
	for in, want := range map[string]any{"4.0": 4, "-1": -1, " 2 ": 2, "Regular  Expression": 4, "exact-match": 3, "auto": "auto"} {
		if got := ws.normalizeNamed("matching_algorithm", in); got != want {
			t.Errorf("normalizeNamed(%q) = %#v, want %#v", in, got, want)
		}
	}
}

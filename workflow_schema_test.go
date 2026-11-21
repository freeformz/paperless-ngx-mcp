package main

import (
	"encoding/json"
	"flag"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/workflow_tool_schemas.golden.json")

var typedWorkflowTools = []string{"workflow_create", "workflow_update", "workflow_trigger_update", "workflow_action_update"}

// The advertised schemas are what clients see; pin them, since CUE's
// jsonschema.Generate is experimental and may change between releases.
func TestWorkflowToolSchemasGolden(t *testing.T) {
	srv := NewServer(NewClient("http://unused", "unused"), nil)
	got := map[string]any{}
	for _, name := range typedWorkflowTools {
		got[name] = srv.GetTool(name).Tool.InputSchema
	}
	b, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	const path = "testdata/workflow_tool_schemas.golden.json"
	if *updateGolden {
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -run TestWorkflowToolSchemasGolden -update)", err)
	}
	if strings.TrimSpace(string(want)) != strings.TrimSpace(string(b)) {
		t.Errorf("workflow tool schemas changed; review and run go test -run TestWorkflowToolSchemasGolden -update")
	}
}

func TestWorkflowToolSchemasShape(t *testing.T) {
	srv := NewServer(NewClient("http://unused", "unused"), nil)
	for _, name := range typedWorkflowTools {
		schema := srv.GetTool(name).Tool.InputSchema
		if len(schema.Properties) < 3 {
			t.Errorf("%s: only %d properties", name, len(schema.Properties))
		}
		b, _ := json.Marshal(schema)
		var v any
		json.Unmarshal(b, &v)
		walkSchema(v, func(key string) {
			switch key {
			case "$ref", "$defs", "if", "then", "else", "allOf", "anyOf", "oneOf", "not", "prefixItems", "x-names", "const":
				t.Errorf("%s: schema has %q, which strict clients reject or which leaks internals", name, key)
			}
		})
		if strings.Contains(string(b), `"null"`) {
			t.Errorf("%s: schema still offers null; nulls are ignored, clearing goes through clear", name)
		}
	}
	trig := srv.GetTool("workflow_trigger_update").Tool.InputSchema.Properties["type"].(map[string]any)
	if !slices.Contains(trig["enum"].([]any), any("manual")) || !slices.Contains(trig["type"].([]string), "string") {
		t.Errorf("trigger type schema = %v, want names accepted", trig)
	}
	if _, has := trig["pattern"]; has {
		t.Errorf("trigger type has a numeric pattern, which would refuse names")
	}
	act := srv.GetTool("workflow_action_update").Tool.InputSchema.Properties["type"].(map[string]any)
	for _, n := range []any{3, 4, 7, 8, "email"} {
		if slices.Contains(act["enum"].([]any), n) {
			t.Errorf("workflow_action_update type allows %v", n)
		}
	}
	// Every per-type rule is stated in a description, since conditionals are
	// stripped from the advertised schema.
	create := srv.GetTool("workflow_create").Tool.InputSchema.Properties["actions"].(map[string]any)
	actionType, _ := json.Marshal(create["items"].(map[string]any)["properties"].(map[string]any)["type"])
	for _, want := range []string{"needs email", "needs webhook", "needs passwords", "needs ai_suggestion_fields", "needs a consumption trigger", "non-consumption trigger"} {
		if !strings.Contains(string(actionType), want) {
			t.Errorf("action type description lacks %q", want)
		}
	}
	trigType, _ := json.Marshal(trig)
	if !strings.Contains(string(trigType), "needs filter_filename, filter_path or filter_mailrule") {
		t.Errorf("trigger type description lacks the consumption filter rule")
	}
	if !strings.Contains(srv.GetTool("workflow_update").Tool.Description, "type is always required") {
		t.Error("workflow_update description lacks the type rule")
	}
}

func walkSchema(v any, f func(key string)) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			// Property names aren't keywords.
			if k == "properties" {
				if props, ok := e.(map[string]any); ok {
					for _, p := range props {
						walkSchema(p, f)
					}
				}
				continue
			}
			f(k)
			walkSchema(e, f)
		}
	case []any:
		for _, e := range x {
			walkSchema(e, f)
		}
	}
}

type upstreamFixture struct {
	Serializers map[string]struct {
		Fields   []string `json:"fields"`
		Nullable []string `json:"nullable"`
	} `json:"serializers"`
	Choices map[string][]any `json:"choices"`
}

// The CUE schema must describe exactly the fields paperless-ngx's
// serializers accept. Regenerate the fixture with
// testdata/upstream/extract_workflow_fields.py when upgrading.
func TestWorkflowCUEMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/upstream/workflow_serializers_3.5.1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx upstreamFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	ws := mustWorkflowSchemas()
	propsOf := func(def string, path ...string) []string {
		node := ws.internal[def]
		for _, p := range path {
			node = node["properties"].(map[string]any)[p].(map[string]any)
			if items, ok := node["items"].(map[string]any); ok {
				node = items
			}
		}
		names := slices.Sorted(maps.Keys(node["properties"].(map[string]any)))
		return slices.DeleteFunc(names, func(s string) bool { return s == "clear" })
	}
	without := func(fields []string, drop ...string) []string {
		out := slices.DeleteFunc(slices.Clone(fields), func(s string) bool { return slices.Contains(drop, s) })
		slices.Sort(out)
		return out
	}
	ser := fx.Serializers
	for _, c := range []struct {
		what      string
		got, want []string
	}{
		{"workflow create", propsOf(defWorkflowCreate), without(ser["WorkflowSerializer"].Fields, "id")},
		{"workflow update", propsOf(defWorkflowUpdate), without(ser["WorkflowSerializer"].Fields, "id")},
		{"trigger update", propsOf(defTriggerPatch), without(ser["WorkflowTriggerSerializer"].Fields, "id")},
		{"nested trigger on create", propsOf(defWorkflowCreate, "triggers"), without(ser["WorkflowTriggerSerializer"].Fields, "id")},
		{"nested trigger on update", propsOf(defWorkflowUpdate, "triggers"), without(ser["WorkflowTriggerSerializer"].Fields)},
		{"action update", propsOf(defActionPatch), without(ser["WorkflowActionSerializer"].Fields, "id", "email", "webhook")},
		{"nested action on create", propsOf(defWorkflowCreate, "actions"), without(ser["WorkflowActionSerializer"].Fields, "id")},
		{"nested action on update", propsOf(defWorkflowUpdate, "actions"), without(ser["WorkflowActionSerializer"].Fields)},
		{"email on create", propsOf(defWorkflowCreate, "actions", "email"), without(ser["WorkflowActionEmailSerializer"].Fields, "id")},
		{"email on update", propsOf(defWorkflowUpdate, "actions", "email"), without(ser["WorkflowActionEmailSerializer"].Fields)},
		{"webhook on create", propsOf(defWorkflowCreate, "actions", "webhook"), without(ser["WorkflowActionWebhookSerializer"].Fields, "id")},
		{"webhook on update", propsOf(defWorkflowUpdate, "actions", "webhook"), without(ser["WorkflowActionWebhookSerializer"].Fields)},
	} {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("%s fields:\n got  %v\n want %v", c.what, c.got, c.want)
		}
	}

	// Nullability: a field takes null (sent only through clear) exactly when
	// paperless-ngx allows it. email and webhook are refused as null on
	// purpose (a null would silently unlink them); assign_tags takes null
	// upstream (allow_null) though its model field doesn't.
	for _, c := range []struct {
		def, serializer string
		extra           []string
		skip            []string
	}{
		{defTriggerPatch, "WorkflowTriggerSerializer", nil, nil},
		{defActionPatch, "WorkflowActionSerializer", []string{"assign_tags"}, []string{"email", "webhook"}},
	} {
		for _, f := range propsOf(c.def) {
			if slices.Contains(c.skip, f) {
				continue
			}
			want := slices.Contains(ser[c.serializer].Nullable, f) || slices.Contains(c.extra, f)
			got := len(ws.validateWorkflowInput(c.def, map[string]any{f: nil})) == 0
			if got != want {
				t.Errorf("%s %s: null accepted = %v, want %v", c.def, f, got, want)
			}
		}
	}
	webhookNulls := map[string]any{"type": 4, "webhook": map[string]any{"url": "u", "params": nil, "body": nil, "headers": nil}}
	if problems := ws.validateWorkflowInput(defWorkflowUpdate, map[string]any{"actions": []any{webhookNulls}}); len(problems) > 0 {
		t.Errorf("webhook nullable fields refused: %v", problems)
	}

	// Choices.
	for kind, key := range map[string]string{"trigger_type": "trigger_type", "matching_algorithm": "matching_algorithm", "source": "source", "action_type": "action_type"} {
		var want []int
		for _, v := range fx.Choices[key] {
			want = append(want, int(v.(float64)))
		}
		got := slices.Sorted(maps.Keys(ws.labels[kind]))
		if !slices.Equal(got, want) {
			t.Errorf("%s values = %v, want %v", kind, got, want)
		}
	}
	trig := ws.internal[defTriggerPatch]["properties"].(map[string]any)
	if got, want := trig["schedule_date_field"].(map[string]any)["enum"], fx.Choices["schedule_date_field"]; !slices.Equal(toStrings(got), toStrings(want)) {
		t.Errorf("schedule_date_field = %v, want %v", got, want)
	}
	act := ws.internal[defActionPatch]["properties"].(map[string]any)
	ai := act["ai_suggestion_fields"].(map[string]any)["items"].(map[string]any)["enum"]
	if !slices.Equal(toStrings(ai), toStrings(fx.Choices["ai_suggestion_field"])) {
		t.Errorf("ai_suggestion_fields = %v, want %v", ai, fx.Choices["ai_suggestion_field"])
	}
}

func TestWorkflowSchemaDetails(t *testing.T) {
	srv := NewServer(NewClient("http://unused", "unused"), nil)
	create := srv.GetTool("workflow_create").Tool.InputSchema.Properties
	action := create["actions"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	params := action["webhook"].(map[string]any)["properties"].(map[string]any)["params"].(map[string]any)
	if params["type"] != "object" {
		t.Errorf("webhook params schema = %v, want any object", params)
	}
	trig := create["triggers"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	if typ, _ := trig["filter_custom_field_query"].(map[string]any)["type"].([]string); !slices.Contains(typ, "array") {
		t.Errorf("nested filter_custom_field_query type = %v, want array too", typ)
	}
	desc, _ := srv.GetTool("workflow_action_update").Tool.InputSchema.Properties["type"].(map[string]any)["description"].(string)
	if strings.Contains(desc, "3 email") || !strings.Contains(desc, "can only be set through workflow_update") {
		t.Errorf("workflow_action_update type description = %q", desc)
	}
}

// A long-lived server swaps its CUE context now and then; validation keeps
// working across the swap.
func TestWorkflowCUEContextRefresh(t *testing.T) {
	ws := mustWorkflowSchemas()
	body := map[string]any{"type": 5}
	for i := range cueContextUses + 5 {
		if problems := ws.validateWorkflowInput(defActionPatch, body); len(problems) == 0 {
			t.Fatalf("call %d: type 5 without passwords accepted", i)
		}
	}
	if problems := ws.validateWorkflowInput(defActionPatch, map[string]any{"type": 5, "passwords": []any{"p"}}); len(problems) > 0 {
		t.Errorf("after refresh: %v", problems)
	}
}

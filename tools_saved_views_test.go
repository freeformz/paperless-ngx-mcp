package main

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestSavedViewList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/saved_views/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1, "name": "Inbox"}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handlePaginatedList(client, "/api/saved_views/"), nil)
	assertNotError(t, result)
}

func TestSavedViewGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/saved_views/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Inbox"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/saved_views/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

// savedViewBodyServer captures the JSON body sent to method path.
func savedViewBodyServer(t *testing.T, method, path string) (*Client, *map[string]any) {
	t.Helper()
	var got map[string]any
	rh := newRouteHandler(t)
	rh.Handle(method, path, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 1}`))
	})
	return testClientAndServer(t, rh), &got
}

func TestSavedViewCreate(t *testing.T) {
	rules := []any{map[string]any{"rule_type": float64(6), "value": "5"}, map[string]any{"rule_type": float64(3), "value": nil}}
	for name, rulesArg := range map[string]any{
		"native array": rules,
		"json string":  `[{"rule_type": 6, "value": "5"}, {"rule_type": 3, "value": null}]`,
	} {
		t.Run(name, func(t *testing.T) {
			client, got := savedViewBodyServer(t, "POST", "/api/saved_views/")
			result := callTool(t, handleSavedViewCreate(client), map[string]any{
				"name":              "Bills",
				"filter_rules":      rulesArg,
				"icon":              "receipt",
				"sort_field":        "created",
				"sort_reverse":      true,
				"page_size":         float64(10),
				"display_mode":      "table",
				"display_fields":    []any{"title", "custom_field_3"},
				"show_on_dashboard": true, // removed: must not be sent
			})
			assertNotError(t, result)
			want := map[string]any{
				"name": "Bills", "filter_rules": rules, "icon": "receipt", "sort_field": "created",
				"sort_reverse": true, "page_size": float64(10), "display_mode": "table",
				"display_fields": []any{"title", "custom_field_3"},
			}
			if !reflect.DeepEqual(*got, want) {
				t.Errorf("body = %v, want %v", *got, want)
			}
		})
	}
}

func TestSavedViewCreateEmptyRules(t *testing.T) {
	client, got := savedViewBodyServer(t, "POST", "/api/saved_views/")
	assertNotError(t, callTool(t, handleSavedViewCreate(client), map[string]any{"name": "All", "filter_rules": []any{}}))
	if rules, ok := (*got)["filter_rules"].([]any); !ok || len(rules) != 0 {
		t.Errorf("filter_rules = %v, want []", (*got)["filter_rules"])
	}
}

func TestSavedViewFilterRuleValuesSentAsText(t *testing.T) {
	client, got := savedViewBodyServer(t, "POST", "/api/saved_views/")
	assertNotError(t, callTool(t, handleSavedViewCreate(client), map[string]any{
		"name": "x",
		"filter_rules": []any{
			map[string]any{"rule_type": float64(6), "value": float64(5)},
			map[string]any{"rule_type": float64(50), "value": true},
			map[string]any{"rule_type": float64(23), "value": 1.5},
		},
		"display_fields": []any{},
	}))
	want := []any{
		map[string]any{"rule_type": float64(6), "value": "5"},
		map[string]any{"rule_type": float64(50), "value": "true"},
		map[string]any{"rule_type": float64(23), "value": "1.5"},
	}
	if !reflect.DeepEqual((*got)["filter_rules"], want) {
		t.Errorf("filter_rules = %v, want %v", (*got)["filter_rules"], want)
	}
	if v, ok := (*got)["display_fields"]; !ok || v != nil {
		t.Errorf("display_fields = %v (present %v), want null for []", v, ok)
	}
}

func TestSavedViewDisplayFieldsNullString(t *testing.T) {
	client, got := savedViewBodyServer(t, "PATCH", "/api/saved_views/1/")
	assertNotError(t, callTool(t, handleSavedViewUpdate(client), map[string]any{"id": float64(1), "display_fields": "null"}))
	if v, ok := (*got)["display_fields"]; !ok || v != nil {
		t.Errorf("display_fields = %v (present %v), want null", v, ok)
	}
}

func TestSavedViewCreateRejectsInvalidInput(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"no name":             {"filter_rules": []any{}},
		"no filter_rules":     {"name": "x"},
		"null filter_rules":   {"name": "x", "filter_rules": nil},
		"rules not array":     {"name": "x", "filter_rules": map[string]any{"rule_type": float64(6)}},
		"rule not object":     {"name": "x", "filter_rules": []any{float64(6)}},
		"rule_type missing":   {"name": "x", "filter_rules": []any{map[string]any{"value": "5"}}},
		"rule_type string":    {"name": "x", "filter_rules": []any{map[string]any{"rule_type": "6", "value": "5"}}},
		"rule_type fraction":  {"name": "x", "filter_rules": []any{map[string]any{"rule_type": 6.5, "value": "5"}}},
		"value object":        {"name": "x", "filter_rules": []any{map[string]any{"rule_type": float64(6), "value": map[string]any{}}}},
		"value array":         {"name": "x", "filter_rules": []any{map[string]any{"rule_type": float64(6), "value": []any{"5"}}}},
		"invalid json":        {"name": "x", "filter_rules": "[{"},
		"display_fields int":  {"name": "x", "filter_rules": []any{}, "display_fields": []any{float64(1)}},
		"display_fields obj":  {"name": "x", "filter_rules": []any{}, "display_fields": map[string]any{"a": "b"}},
		"display_mode number": {"name": "x", "filter_rules": []any{}, "display_mode": float64(1)},
	} {
		t.Run(name, func(t *testing.T) {
			client := testClientAndServer(t, newRouteHandler(t))
			assertIsError(t, callTool(t, handleSavedViewCreate(client), args))
		})
	}
}

func TestSavedViewUpdate(t *testing.T) {
	client, got := savedViewBodyServer(t, "PATCH", "/api/saved_views/1/")
	result := callTool(t, handleSavedViewUpdate(client), map[string]any{
		"id":              float64(1),
		"name":            "Updated",
		"filter_rules":    `[{"rule_type": 17, "value": "7"}]`,
		"show_in_sidebar": false, // removed: must not be sent
	})
	assertNotError(t, result)
	want := map[string]any{"name": "Updated", "filter_rules": []any{map[string]any{"rule_type": float64(17), "value": "7"}}}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}
}

func TestSavedViewUpdateClearsNullableFields(t *testing.T) {
	client, got := savedViewBodyServer(t, "PATCH", "/api/saved_views/1/")
	assertNotError(t, callTool(t, handleSavedViewUpdate(client), map[string]any{
		"id": float64(1), "page_size": nil, "display_mode": nil, "display_fields": nil,
	}))
	want := map[string]any{"page_size": nil, "display_mode": nil, "display_fields": nil}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}
}

func TestSavedViewUpdateNoFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleSavedViewUpdate(client), map[string]any{"id": float64(1)})
	assertIsError(t, result)
}

func TestSavedViewDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/saved_views/1/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDeleteByID(client, "/api/saved_views/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestSavedViewRuleTypesTable(t *testing.T) {
	if len(savedViewRuleTypes) != 51 {
		t.Fatalf("rule types = %d, want 51 (0 to 50)", len(savedViewRuleTypes))
	}
	for i, r := range savedViewRuleTypes {
		if r.ID != i {
			t.Errorf("entry %d has ID %d; the table must be ordered 0..50 with no gaps", i, r.ID)
		}
		if r.Label == "" || r.Value == "" || r.APIFilter == "" {
			t.Errorf("rule type %d has an empty field: %+v", r.ID, r)
		}
	}
	if len(savedViewIcons) != 57 {
		t.Errorf("icons = %d, want 57 (paperless-ngx 3.5.1)", len(savedViewIcons))
	}
}

func TestSavedViewRuleTypes(t *testing.T) {
	all := callTool(t, handleSavedViewRuleTypes(), nil)
	assertNotError(t, all)
	var got []savedViewRuleType
	if err := json.Unmarshal([]byte(resultText(t, all)), &got); err != nil || len(got) != 51 {
		t.Fatalf("all = %d entries (err %v), want 51", len(got), err)
	}

	for name, ids := range map[string]any{"native": []any{float64(17), float64(6)}, "json string": "[17, 6]"} {
		t.Run(name, func(t *testing.T) {
			res := callTool(t, handleSavedViewRuleTypes(), map[string]any{"ids": ids})
			assertNotError(t, res)
			var sel []savedViewRuleType
			json.Unmarshal([]byte(resultText(t, res)), &sel)
			if len(sel) != 2 || sel[0].ID != 17 || sel[1].ID != 6 || sel[1].APIFilter != "tags__id__all" {
				t.Errorf("selection = %+v, want 17 then 6, in that order", sel)
			}
		})
	}

	nullIDs := callTool(t, handleSavedViewRuleTypes(), map[string]any{"ids": "null"})
	var viaNull []savedViewRuleType
	json.Unmarshal([]byte(resultText(t, nullIDs)), &viaNull)
	if len(viaNull) != 51 {
		t.Errorf("ids \"null\" returned %d entries, want all 51", len(viaNull))
	}

	for name, ids := range map[string]any{"unknown": []any{float64(51)}, "negative": []any{float64(-1)}, "string items": []any{"6"}, "fraction": []any{1.5}, "bad json": "[6,"} {
		t.Run("rejects "+name, func(t *testing.T) {
			res := callTool(t, handleSavedViewRuleTypes(), map[string]any{"ids": ids})
			assertIsError(t, res)
		})
	}
}

func TestSavedViewDescriptionsReferToRuleTypes(t *testing.T) {
	srv := NewServer(NewClient("http://unused", "unused"), nil)
	for _, name := range []string{"saved_view_list", "saved_view_get", "saved_view_create", "saved_view_update"} {
		tool := srv.GetTool(name)
		if tool == nil {
			t.Fatalf("%s not registered", name)
		}
		if !strings.Contains(tool.Tool.Description, "saved_view_rule_types") {
			t.Errorf("%s description should point to saved_view_rule_types", name)
		}
		if _, ok := tool.Tool.InputSchema.Properties["show_on_dashboard"]; ok {
			t.Errorf("%s still has show_on_dashboard", name)
		}
	}
}

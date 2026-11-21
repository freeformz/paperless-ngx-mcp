package main

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestWorkflowList(t *testing.T) {
	client, got := captureQuery(t, "/api/workflows/", `{"count":0,"results":[]}`)
	assertNotError(t, callTool(t, handleWorkflowList(client), map[string]any{"trigger_type": "manual", "enabled": true, "id_in": []any{float64(3), float64(4)}, "page_size": float64(5)}))
	for k, want := range map[string]string{"trigger_type": "5", "enabled": "true", "id__in": "3,4", "page_size": "5"} {
		if g := got.Get(k); g != want {
			t.Errorf("%s = %q, want %q (query %v)", k, g, want, *got)
		}
	}
	assertNotError(t, callTool(t, handleWorkflowList(client), map[string]any{"trigger_type": float64(2), "id": float64(7), "id_in": "[]"}))
	if got.Get("trigger_type") != "2" || got.Get("id") != "7" || got.Has("id__in") {
		t.Errorf("query = %v", *got)
	}
	assertErrorContains(t, callTool(t, handleWorkflowList(client), map[string]any{"trigger_type": "sometimes"}), "trigger_type must be one of: 1 (consumption)")
}

func TestWorkflowGetAndDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/workflows/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Auto-tag"}))
	rh.Handle("DELETE", "/api/workflows/1/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	client := testClientAndServer(t, rh)
	assertNotError(t, callTool(t, handleGetByID(client, "/api/workflows/%d/"), map[string]any{"id": float64(1)}))
	assertNotError(t, callTool(t, handleDeleteByID(client, "/api/workflows/%d/"), map[string]any{"id": float64(1)}))
}

func TestWorkflowCreateSendsTypedBody(t *testing.T) {
	client, got := captureJSONBody(t, "POST", "/api/workflows/")
	assertNotError(t, callThroughMiddleware(t, client, "workflow_create", map[string]any{
		"name":     "Tag invoices",
		"enabled":  "true",
		"order":    "2",
		"triggers": `[{"type": "manual", "filter_has_tags": [3], "matching_algorithm": "regex", "match": "x", "filter_custom_field_query": ["Amount", "gt", 5], "filter_path": null}]`,
		"actions": []any{
			map[string]any{"type": "assignment", "assign_tags": []any{"4"}, "assign_title": "{{ custom_fields['Ref'].value }}", "assign_correspondent": nil},
			map[string]any{"type": "email", "email": map[string]any{"subject": "s", "body": "b", "to": "a@example.com"}},
			map[string]any{"type": 5, "passwords": []any{"pw"}},
		},
	}))
	want := map[string]any{
		"name": "Tag invoices", "enabled": true, "order": float64(2),
		"triggers": []any{map[string]any{"type": float64(5), "filter_has_tags": []any{float64(3)}, "matching_algorithm": float64(4), "match": "x", "filter_custom_field_query": `["Amount","gt",5]`, "sources": []any{float64(1), float64(2), float64(3)}}},
		"actions": []any{
			map[string]any{"type": float64(1), "assign_tags": []any{float64(4)}, "assign_title": "{{ custom_fields['Ref'].value }}"},
			map[string]any{"type": float64(3), "email": map[string]any{"subject": "s", "body": "b", "to": "a@example.com"}},
			map[string]any{"type": float64(5), "passwords": []any{"pw"}},
		},
	}
	if !reflect.DeepEqual(*got, want) {
		gb, _ := json.Marshal(*got)
		wb, _ := json.Marshal(want)
		t.Errorf("body =\n%s\nwant\n%s", gb, wb)
	}
}

func TestWorkflowCreateRefusesBadInput(t *testing.T) {
	client := testClientAndServer(t, newRouteHandler(t)) // any request fails the test
	ok := func(extra map[string]any) map[string]any {
		args := map[string]any{"name": "w", "triggers": []any{map[string]any{"type": 5}}, "actions": []any{map[string]any{"type": 1}}}
		for k, v := range extra {
			args[k] = v
		}
		return args
	}
	for _, tc := range []struct {
		name string
		args map[string]any
		want []string
	}{
		{"misspelled trigger field", ok(map[string]any{"triggers": []any{map[string]any{"type": 5, "filter_has_tag": []any{1}}}}), []string{"triggers[0].filter_has_tag: unknown field"}},
		{"misspelled top-level", ok(map[string]any{"enabeld": true}), []string{"enabeld: unknown field"}},
		{"bad type name", ok(map[string]any{"triggers": []any{map[string]any{"type": "sometimes"}}}), []string{"triggers[0].type: must be one of: 1 (consumption), 2 (document_added), 3 (document_updated), 4 (scheduled), 5 (manual)"}},
		{"trigger id on create", ok(map[string]any{"triggers": []any{map[string]any{"type": 5, "id": 3}}}), []string{"triggers[0].id: unknown field (workflow_create always adds"}},
		{"email id on create", ok(map[string]any{"actions": []any{map[string]any{"type": 3, "email": map[string]any{"id": 2, "subject": "s", "body": "b", "to": "t"}}}}), []string{"actions[0].email.id: unknown field"}},
		{"type 5 without passwords", ok(map[string]any{"actions": []any{map[string]any{"type": "password_removal"}}}), []string{"actions[0].passwords: is required for type 5"}},
		{"blank password", ok(map[string]any{"actions": []any{map[string]any{"type": 5, "passwords": []any{"  "}}}}), []string{"actions[0].passwords[0]: must not be blank"}},
		{"empty passwords", ok(map[string]any{"actions": []any{map[string]any{"type": 5, "passwords": []any{}}}}), []string{"actions[0].passwords: must not be empty"}},
		{"type 8 without fields", ok(map[string]any{"actions": []any{map[string]any{"type": 8}}}), []string{"actions[0].ai_suggestion_fields: is required for type 8"}},
		{"type 8 bad field", ok(map[string]any{"actions": []any{map[string]any{"type": 8, "ai_suggestion_fields": []any{"owner"}}}}), []string{"actions[0].ai_suggestion_fields[0]"}},
		{"email without email", ok(map[string]any{"actions": []any{map[string]any{"type": 3}}}), []string{"actions[0].email: is required for type 3"}},
		{"webhook without url", ok(map[string]any{"actions": []any{map[string]any{"type": 4, "webhook": map[string]any{"as_json": true}}}}), []string{"actions[0].webhook.url: is required"}},
		{"required hidden by another error", ok(map[string]any{"actions": []any{map[string]any{"type": 5, "zz": 1}}}), []string{"actions[0].zz: unknown field", "actions[0].passwords: is required for type 5"}},
		{"fractional id", ok(map[string]any{"actions": []any{map[string]any{"type": 1, "assign_owner": 1.5}}}), []string{"actions[0].assign_owner: must be a whole number"}},
		{"zero id", ok(map[string]any{"actions": []any{map[string]any{"type": 1, "assign_tags": []any{0}}}}), []string{"actions[0].assign_tags[0]: must be at least 1"}},
		{"long match", ok(map[string]any{"triggers": []any{map[string]any{"type": 5, "match": strings.Repeat("x", 257)}}}), []string{"triggers[0].match: is too long: at most 256 characters"}},
		{"no triggers", map[string]any{"name": "w", "actions": []any{map[string]any{"type": 1}}}, []string{"triggers: is required"}},
		{"empty triggers is padding", ok(map[string]any{"triggers": []any{}}), []string{"triggers: is required"}},
		{"padded body", ok(map[string]any{"body": map[string]any{"enabled": true}}), []string{"body was removed"}},
	} {
		result := callThroughMiddleware(t, client, "workflow_create", tc.args)
		assertIsError(t, result)
		text := resultText(t, result)
		for _, w := range tc.want {
			if !strings.Contains(text, w) {
				t.Errorf("%s: error =\n%s\nwant it to contain %q", tc.name, text, w)
			}
		}
	}
	// A padded {} or "" body is ignored, not refused.
	pc, _ := captureJSONBody(t, "POST", "/api/workflows/")
	assertNotError(t, callThroughMiddleware(t, pc, "workflow_create", ok(map[string]any{"body": map[string]any{}})))
}

func workflowGETHandler(t *testing.T, rh *routeHandler) {
	rh.Handle("GET", "/api/workflows/7/", jsonHandler(t, 200, map[string]any{
		"id":       7,
		"triggers": []any{map[string]any{"id": 11, "type": 5}},
		"actions": []any{
			map[string]any{"id": 21, "type": 1, "email": nil, "webhook": nil},
			map[string]any{"id": 22, "type": 3, "email": map[string]any{"id": 31, "subject": "s"}, "webhook": nil},
		},
	}))
}

func TestWorkflowUpdate(t *testing.T) {
	var got map[string]any
	rh := newRouteHandler(t)
	workflowGETHandler(t, rh)
	rh.Handle("PATCH", "/api/workflows/7/", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 7}`))
	})
	client := testClientAndServer(t, rh)

	// Nested nulls are dropped; per-item clear nulls a field; new triggers get the create defaults.
	assertNotError(t, callThroughMiddleware(t, client, "workflow_update", map[string]any{
		"id": float64(7), "name": nil, "enabled": false,
		"triggers": []any{
			map[string]any{"id": 11, "type": "manual", "filter_path": nil, "clear": []any{"filter_has_correspondent"}},
			map[string]any{"type": "document_added", "filter_filename": "*.pdf"},
		},
		"actions": []any{
			map[string]any{"id": 21, "type": 1, "assign_owner": nil, "assign_custom_fields_values": map[string]any{"5": nil}},
			map[string]any{"id": 22, "type": "email", "email": map[string]any{"id": 31, "subject": "s2", "body": "b", "to": "t"}},
		},
	}))
	want := map[string]any{
		"enabled": false,
		"triggers": []any{
			map[string]any{"id": float64(11), "type": float64(5), "filter_has_correspondent": nil},
			map[string]any{"type": float64(2), "filter_filename": "*.pdf", "sources": []any{float64(1), float64(2), float64(3)}},
		},
		"actions": []any{
			map[string]any{"id": float64(21), "type": float64(1), "assign_custom_fields_values": map[string]any{"5": nil}},
			map[string]any{"id": float64(22), "type": float64(3), "email": map[string]any{"id": float64(31), "subject": "s2", "body": "b", "to": "t"}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		gb, _ := json.Marshal(got)
		wb, _ := json.Marshal(want)
		t.Errorf("body =\n%s\nwant\n%s", gb, wb)
	}

	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"foreign trigger id", map[string]any{"triggers": []any{map[string]any{"id": 99, "type": 5}}}, "triggers[0].id: trigger 99 is not part of this workflow"},
		{"foreign action id", map[string]any{"actions": []any{map[string]any{"id": 98, "type": 1}}}, "actions[0].id: action 98 is not part of this workflow"},
		{"email id of another action", map[string]any{"actions": []any{map[string]any{"id": 21, "type": 3, "email": map[string]any{"id": 31, "subject": "s", "body": "b", "to": "t"}}}}, "actions[0].email.id: email settings 31 don't belong to this action"},
		{"email id on a new action", map[string]any{"actions": []any{map[string]any{"type": 3, "email": map[string]any{"id": 31, "subject": "s", "body": "b", "to": "t"}}}}, "actions[0].email.id: email settings 31 don't belong"},
		{"type missing on an existing item", map[string]any{"triggers": []any{map[string]any{"id": 11}}}, "triggers[0].type: is required on every trigger and action"},
		{"id null still needs type", map[string]any{"actions": []any{map[string]any{"id": nil, "assign_title": "x"}}}, "actions[0].type: is required"},
		{"clear with a value", map[string]any{"triggers": []any{map[string]any{"id": 11, "type": 5, "filter_path": "x", "clear": []any{"filter_path"}}}}, "triggers[0].filter_path: is both given a value and listed in clear"},
		{"clear of an unclearable field", map[string]any{"triggers": []any{map[string]any{"id": 11, "type": 5, "clear": []any{"type"}}}}, "can't be cleared"},
		{"nothing to update", map[string]any{"triggers": []any{}, "actions": "[]"}, "no fields to update"},
	} {
		args := map[string]any{"id": float64(7)}
		for k, v := range tc.args {
			args[k] = v
		}
		got = nil
		result := callThroughMiddleware(t, client, "workflow_update", args)
		assertErrorContains(t, result, tc.want)
		if got != nil {
			t.Errorf("%s: sent %v", tc.name, got)
		}
	}
}

func TestWorkflowTriggerAndActionUpdate(t *testing.T) {
	client, got := captureJSONBody(t, "PATCH", "/api/workflow_triggers/4/")
	assertNotError(t, callThroughMiddleware(t, client, "workflow_trigger_update", map[string]any{
		"id": float64(4), "type": "consumption", "filter_filename": "*.pdf", "sources": []any{"web ui", "api"}, "is_insensitive": "false",
		"filter_custom_field_query": `["Amount","gt",5]`, "clear": "filter_mailrule", "match": "",
	}))
	want := map[string]any{"type": float64(1), "filter_filename": "*.pdf", "sources": []any{float64(4), float64(2)}, "is_insensitive": false, "filter_custom_field_query": `["Amount","gt",5]`, "filter_mailrule": nil}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("trigger body = %v, want %v", *got, want)
	}

	client, got = captureJSONBody(t, "PATCH", "/api/workflow_actions/5/")
	assertNotError(t, callThroughMiddleware(t, client, "workflow_action_update", map[string]any{"id": float64(5), "type": "trash", "clear": []any{"assign_owner"}}))
	if want := map[string]any{"type": float64(6), "assign_owner": nil}; !reflect.DeepEqual(*got, want) {
		t.Errorf("action body = %v, want %v", *got, want)
	}
	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"type": "email"}, "type: must be one of: 1 (assignment), 2 (removal), 5 (password_removal), 6 (move_to_trash) (types 3 email, 4 webhook, 7 remote_ocr and 8 apply_ai_suggestions can only be set through workflow_update)"},
		{map[string]any{"type": 8, "ai_suggestion_fields": []any{"title"}}, "can only be set through workflow_update"},
		{map[string]any{"email": map[string]any{"to": "x"}}, "email: unknown field (email and webhook settings can only be written through workflow_update)"},
		{map[string]any{"webhook": map[string]any{"url": "x"}}, "webhook: unknown field"},
		{map[string]any{"type": 5}, "passwords: is required for type 5"},
		{map[string]any{}, "no fields to update"},
	} {
		args := map[string]any{"id": float64(5)}
		for k, v := range tc.args {
			args[k] = v
		}
		assertErrorContains(t, callThroughMiddleware(t, testClientAndServer(t, newRouteHandler(t)), "workflow_action_update", args), tc.want)
	}
}

func TestWorkflowRun(t *testing.T) {
	client, got := captureJSONBody(t, "POST", "/api/documents/bulk_edit/")
	assertNotError(t, callThroughMiddleware(t, client, "workflow_run", map[string]any{"workflow": float64(3), "documents": []any{float64(1), float64(2)}}))
	want := map[string]any{"documents": []any{float64(1), float64(2)}, "method": "run_workflow", "parameters": map[string]any{"workflow": float64(3), "filter_documents": true}}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("body = %v, want %v", *got, want)
	}
	client, got = captureJSONBody(t, "POST", "/api/documents/bulk_edit/")
	assertNotError(t, callThroughMiddleware(t, client, "workflow_run", map[string]any{"workflow": "3", "all": true, "filters": map[string]any{"title": "inv"}, "filter_documents": false}))
	want = map[string]any{"all": true, "filters": map[string]any{"title__icontains": "inv"}, "method": "run_workflow", "parameters": map[string]any{"workflow": float64(3), "filter_documents": false}}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("all body = %v, want %v", *got, want)
	}
	none := testClientAndServer(t, newRouteHandler(t))
	assertErrorContains(t, callThroughMiddleware(t, none, "workflow_run", map[string]any{"documents": []any{float64(1)}}), "workflow is required")
	assertErrorContains(t, callThroughMiddleware(t, none, "workflow_run", map[string]any{"workflow": float64(3)}), "documents is required")
	assertErrorContains(t, callThroughMiddleware(t, none, "workflow_run", map[string]any{"workflow": float64(3), "documents": []any{float64(1)}, "filter_documents": "maybe"}), "filter_documents must be true or false")
}

func TestWorkflowRunWait(t *testing.T) {
	fastPolling(t)
	seq := &taskSequence{statuses: []string{"started", "success"}}
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/bulk_edit/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"task_id":"` + testTaskUUID + `"}`))
	})
	rh.Handle("GET", "/api/tasks/", seq.handle)
	result := callTool(t, handleWorkflowRun(testClientAndServer(t, rh)), map[string]any{"workflow": float64(3), "documents": []any{float64(1)}, "wait": true})
	assertNotError(t, result)
	if m := resultJSON(t, result); m["task"].(map[string]any)["status"] != "success" {
		t.Errorf("result = %v", m)
	}
}

func TestDocumentBulkEditRunWorkflowMoved(t *testing.T) {
	result := callTool(t, handleDocumentBulkEdit(testClientAndServer(t, newRouteHandler(t))), map[string]any{"documents": []any{float64(1)}, "method": "run_workflow", "parameters": map[string]any{"workflow": 3}})
	assertErrorContains(t, result, "document_bulk_edit no longer runs run_workflow; use workflow_run")
	if slices.Contains(bulkEditMethods, "run_workflow") {
		t.Error("run_workflow is still a document_bulk_edit method")
	}
}

func TestWorkflowCreateToolsRemoved(t *testing.T) {
	srv := NewServer(NewClient("http://unused", "unused"), nil)
	for _, name := range []string{"workflow_trigger_create", "workflow_action_create"} {
		if srv.GetTool(name) != nil {
			t.Errorf("%s is still registered", name)
		}
	}
	for _, name := range []string{"workflow_run", "workflow_create", "workflow_update", "workflow_trigger_update", "workflow_action_update"} {
		if srv.GetTool(name) == nil {
			t.Errorf("%s is not registered", name)
		}
	}
}

// The CUE runtime isn't safe for concurrent use; validation is serialised.
func TestWorkflowValidationConcurrent(t *testing.T) {
	ws := mustWorkflowSchemas()
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			args := map[string]any{"name": "w", "triggers": []any{map[string]any{"type": "manual"}}, "actions": []any{map[string]any{"type": 5}}}
			if i%2 == 0 {
				args["actions"] = []any{map[string]any{"type": 5, "passwords": []any{"p"}}}
			}
			body, perr := ws.normalizeWorkflowInput(defWorkflowCreate, args)
			if perr != nil {
				t.Errorf("normalize: %v", perr.problems)
				return
			}
			problems := ws.validateWorkflowInput(defWorkflowCreate, body)
			if (i%2 == 0) != (len(problems) == 0) {
				t.Errorf("call %d: problems = %v", i, problems)
			}
		}()
	}
	wg.Wait()
}

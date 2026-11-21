package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const workflowNestedNote = "Triggers and actions are typed (see the parameter schema); unknown or misspelled fields are refused, because paperless-ngx would silently ignore them. Type and choice values take a name or a number. A null value is ignored; to clear a field, name it in that object's clear."

func registerWorkflowTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(mcp.NewTool("workflow_list",
		mcp.WithDescription("List workflows, with their triggers and actions."),
		withNumber("page", mcp.Description("Page number (default: 1)")),
		withNumber("page_size", mcp.Description("Results per page (default: 25)")),
		mcp.WithAny("trigger_type", mcp.Description("Only workflows with a trigger of this type: 1 consumption, 2 document_added, 3 document_updated, 4 scheduled, 5 manual (name or number)"), func(s map[string]any) {
			s["type"] = []string{"integer", "string"}
			s["enum"] = triggerTypeEnum()
		}),
		mcp.WithBoolean("enabled", mcp.Description("Only enabled (true) or disabled (false) workflows")),
		withNumber("id", mcp.Description("Only the workflow with this ID")),
		withJSONArray("id_in", mcp.Description("Array of workflow IDs to list"), mcp.Items(map[string]any{"type": "integer"})),
	), handleWorkflowList(client))

	srv.AddTool(mcp.NewTool("workflow_get",
		mcp.WithDescription("Get workflow details, with its triggers and actions."),
		withNumber("id", mcp.Description("Workflow ID"), mcp.Required()),
	), handleGetByID(client, "/api/workflows/%d/"))

	srv.AddTool(typedWorkflowTool("workflow_create",
		"Create a workflow with its triggers and actions. "+workflowNestedNote+" paperless-ngx also checks: a consumption (type 1) trigger needs filter_filename, filter_path or filter_mailrule; a remote_ocr (7) action needs a consumption trigger; an apply_ai_suggestions (8) action needs a trigger other than consumption. Triggers that leave out sources get consume_folder, api_upload and mail_fetch.",
		defWorkflowCreate, "",
	), handleWorkflowWrite(client, defWorkflowCreate, "POST", "/api/workflows/", false))

	srv.AddTool(typedWorkflowTool("workflow_update",
		"Update a workflow; only the fields given change. triggers and actions, when given, replace the workflow's whole set: an item with id updates that trigger or action (only the fields it gives; type is always required, so an existing action of type 3, 4, 5 or 8 must repeat its email, webhook, passwords or ai_suggestion_fields), an item without id adds a new one, and every trigger or action left out is deleted. Ids must belong to this workflow. Email or webhook settings given with their id update those settings; without an id they replace them. A consumption (type 1) trigger must repeat filter_filename, filter_path or filter_mailrule in every update. A remote_ocr (7) action needs a consumption trigger; an apply_ai_suggestions (8) action needs a trigger of another type. New triggers that leave out sources get consume_folder, api_upload and mail_fetch; sources: [] means any source. "+strings.Replace(workflowNestedNote, "(see the parameter schema)", "(fields as in workflow_create's triggers and actions)", 1),
		defWorkflowUpdate, "Workflow ID",
	), handleWorkflowWrite(client, defWorkflowUpdate, "PATCH", "/api/workflows/%d/", true))

	srv.AddTool(mcp.NewTool("workflow_delete",
		mcp.WithDescription("Delete a workflow. Its triggers and actions are not deleted with it: delete them with workflow_trigger_delete and workflow_action_delete (paperless-ngx prunes unattached triggers and actions, server-wide, on the next workflow update)."),
		withNumber("id", mcp.Description("Workflow ID"), mcp.Required()),
	), handleDeleteByID(client, "/api/workflows/%d/"))

	runOpts := []mcp.ToolOption{
		mcp.WithDescription(workflowRunDescription),
		withNumber("workflow", mcp.Description("Workflow ID; it must be enabled and have a manual (type 5) trigger"), mcp.Required()),
		mcp.WithBoolean("filter_documents", mcp.Description("Apply the manual trigger's filters, skipping documents that don't match (default true); false runs the actions on every selected document")),
	}
	runOpts = append(runOpts, documentSelectionOptions()...)
	runOpts = append(runOpts, waitToolOptions()...)
	srv.AddTool(mcp.NewTool("workflow_run", runOpts...), handleWorkflowRun(client))

	// Triggers and actions are created through workflow_create and
	// workflow_update: paperless-ngx deletes unattached ones on every workflow
	// update, and its action endpoint can't write email or webhook settings.
	srv.AddTool(mcp.NewTool("workflow_trigger_list",
		mcp.WithDescription("List workflow triggers."),
		withNumber("page", mcp.Description("Page number (default: 1)")),
		withNumber("page_size", mcp.Description("Results per page (default: 25)")),
	), handlePaginatedList(client, "/api/workflow_triggers/"))

	srv.AddTool(mcp.NewTool("workflow_trigger_get",
		mcp.WithDescription("Get workflow trigger details."),
		withNumber("id", mcp.Description("Trigger ID"), mcp.Required()),
	), handleGetByID(client, "/api/workflow_triggers/%d/"))

	srv.AddTool(typedWorkflowTool("workflow_trigger_update",
		"Update one workflow trigger; only the fields given change. A consumption (type 1) trigger must include filter_filename, filter_path or filter_mailrule in every update. Changing a trigger's type here skips the checks paperless-ngx makes on a whole workflow (remote OCR needs a consumption trigger, AI suggestions need another type); prefer workflow_update for that. To add a trigger, use workflow_update. Type and choice values take a name or a number. Unknown or misspelled fields are refused. A null value is ignored; to clear a field, name it in clear.",
		defTriggerPatch, "Trigger ID",
	), handleWorkflowWrite(client, defTriggerPatch, "PATCH", "/api/workflow_triggers/%d/", false))

	srv.AddTool(mcp.NewTool("workflow_trigger_delete",
		mcp.WithDescription("Delete a workflow trigger."),
		withNumber("id", mcp.Description("Trigger ID"), mcp.Required()),
	), handleDeleteByID(client, "/api/workflow_triggers/%d/"))

	srv.AddTool(mcp.NewTool("workflow_action_list",
		mcp.WithDescription("List workflow actions."),
		withNumber("page", mcp.Description("Page number (default: 1)")),
		withNumber("page_size", mcp.Description("Results per page (default: 25)")),
	), handlePaginatedList(client, "/api/workflow_actions/"))

	srv.AddTool(mcp.NewTool("workflow_action_get",
		mcp.WithDescription("Get workflow action details."),
		withNumber("id", mcp.Description("Action ID"), mcp.Required()),
	), handleGetByID(client, "/api/workflow_actions/%d/"))

	srv.AddTool(typedWorkflowTool("workflow_action_update",
		"Update one workflow action; only the fields given change. Types 3 (email), 4 (webhook), 7 (remote_ocr) and 8 (apply_ai_suggestions), and email or webhook settings, can only be set through workflow_update. To add an action, use workflow_update. Type and choice values take a name or a number. Unknown or misspelled fields are refused. A null value is ignored; to clear a field, name it in clear.",
		defActionPatch, "Action ID",
	), handleWorkflowWrite(client, defActionPatch, "PATCH", "/api/workflow_actions/%d/", false))

	srv.AddTool(mcp.NewTool("workflow_action_delete",
		mcp.WithDescription("Delete a workflow action."),
		withNumber("id", mcp.Description("Action ID"), mcp.Required()),
	), handleDeleteByID(client, "/api/workflow_actions/%d/"))
}

const workflowRunDescription = "Run a workflow by hand on documents. The workflow must be enabled and have a manual (type 5) trigger. Needs permission to view workflows, and change permission on and ownership of every selected document (or documents without an owner). With filter_documents (default true), documents that don't match the manual trigger's filters are skipped, not failed; versions are never included. " + documentSelectionNote + " The server queues a task and returns its task_id at once; if a document fails, the task fails naming it. Pass wait: true to wait for the outcome."

// triggerTypeEnum lists the trigger types by number and name.
func triggerTypeEnum() []any {
	ws := mustWorkflowSchemas()
	labels := ws.labels["trigger_type"]
	var nums []int
	for n := range labels {
		nums = append(nums, n)
	}
	slices.Sort(nums)
	values := []any{}
	for _, n := range nums {
		values = append(values, n)
	}
	for _, n := range nums {
		values = append(values, labels[n])
	}
	return values
}

// typedWorkflowTool builds a tool whose parameters come from a CUE
// definition, plus id (the URL path) when idDesc is given.
func typedWorkflowTool(name, description, def, idDesc string) mcp.Tool {
	tool := mcp.NewTool(name, mcp.WithDescription(description))
	tool.InputSchema = workflowToolSchema(def)
	if idDesc != "" {
		idTool := mcp.NewTool("id", withNumber("id", mcp.Description(idDesc), mcp.Required()))
		tool.InputSchema.Properties["id"] = idTool.InputSchema.Properties["id"]
		tool.InputSchema.Required = append([]string{"id"}, tool.InputSchema.Required...)
	}
	return tool
}

// workflowLocks serialises this process's updates of one workflow, so its id
// checks and its write see the same triggers and actions. It can't stop a
// trigger or action being deleted elsewhere (workflow_trigger_delete, the web
// UI) between the check and the write. One entry per workflow id updated.
var workflowLocks sync.Map // int -> chan struct{} (a one-slot semaphore)

// lockWorkflow waits for the workflow's lock, or for ctx to end.
func lockWorkflow(ctx context.Context, id int) (unlock func(), err error) {
	v, _ := workflowLocks.LoadOrStore(id, make(chan struct{}, 1))
	sem := v.(chan struct{})
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func handleWorkflowWrite(client *Client, def, method, pathFmt string, isWorkflowUpdate bool) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		ws := mustWorkflowSchemas()
		args := map[string]any{}
		for k, v := range request.GetArguments() {
			args[k] = v
		}
		path := pathFmt
		isUpdate := method == "PATCH"
		var id int
		if isUpdate {
			var errRes *mcp.CallToolResult
			if id, errRes = getRequiredInt(request, "id"); errRes != nil {
				return errRes, nil
			}
			path = fmt.Sprintf(pathFmt, id)
			delete(args, "id")
		}
		if argIsSet(args["body"]) {
			return errResult("body was removed; pass the fields as parameters (see the tool's schema)"), nil
		}
		delete(args, "body")

		body, perr := ws.normalizeWorkflowInput(def, args)
		if perr != nil {
			return errResult("invalid arguments:\n" + strings.Join(perr.problems, "\n")), nil
		}
		if isUpdate && len(body) == 0 {
			return errResult("no fields to update; " + clearHint), nil
		}
		if problems := ws.validateWorkflowInput(def, body); len(problems) > 0 {
			return errResult("invalid arguments:\n" + strings.Join(problems, "\n")), nil
		}
		fillDefaultSources(body, def)

		if isWorkflowUpdate {
			unlock, err := lockWorkflow(ctx, id)
			if err != nil {
				return errResult("waiting for another update of this workflow: " + err.Error()), nil
			}
			defer unlock()
			if errRes := checkNestedIDs(ctx, client, path, body); errRes != nil {
				return errRes, nil
			}
		}

		var resp *httpResponse
		var err error
		if isUpdate {
			resp, err = client.Patch(ctx, path, body)
		} else {
			resp, err = client.Post(ctx, path, body)
		}
		return doRequest(resp, err, method, path)
	}
}

// fillDefaultSources gives new triggers without sources the default a
// created workflow gets (consume_folder, api_upload, mail_fetch): on an
// update paperless-ngx would otherwise use the model default, which adds web
// UI uploads.
func fillDefaultSources(body map[string]any, def string) {
	if def != defWorkflowUpdate && def != defWorkflowCreate {
		return
	}
	triggers, _ := body["triggers"].([]any)
	for _, t := range triggers {
		m, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if _, hasID := m["id"]; hasID {
			continue
		}
		if _, has := m["sources"]; !has {
			m["sources"] = []any{1, 2, 3}
		}
	}
}

// checkNestedIDs refuses nested ids that don't belong to the workflow:
// paperless-ngx would otherwise create a row with that id, or take over
// another workflow's trigger, action or email or webhook settings.
func checkNestedIDs(ctx context.Context, client *Client, path string, body map[string]any) *mcp.CallToolResult {
	triggers, _ := body["triggers"].([]any)
	actions, _ := body["actions"].([]any)
	if !anyNestedID(triggers, actions) {
		return nil
	}
	resp, err := client.Get(ctx, path, nil)
	raw, errRes := doRequestBody(resp, err, "GET", path)
	if errRes != nil {
		return errRes
	}
	var current struct {
		Triggers []struct {
			ID int `json:"id"`
		} `json:"triggers"`
		Actions []struct {
			ID      int               `json:"id"`
			Email   *struct{ ID int } `json:"email"`
			Webhook *struct{ ID int } `json:"webhook"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(raw, &current); err != nil {
		return errResult("couldn't read the workflow to check ids: " + err.Error())
	}
	triggerIDs := map[int]bool{}
	for _, t := range current.Triggers {
		triggerIDs[t.ID] = true
	}
	type sub struct{ email, webhook int }
	actionSubs := map[int]sub{}
	for _, a := range current.Actions {
		s := sub{}
		if a.Email != nil {
			s.email = a.Email.ID
		}
		if a.Webhook != nil {
			s.webhook = a.Webhook.ID
		}
		actionSubs[a.ID] = s
	}
	var problems []string
	for i, t := range triggers {
		if n, ok := nestedID(t); ok && !triggerIDs[n] {
			problems = append(problems, fmt.Sprintf("triggers[%d].id: trigger %d is not part of this workflow; leave id out to add a new trigger", i, n))
		}
	}
	for i, a := range actions {
		m, _ := a.(map[string]any)
		n, hasID := nestedID(a)
		s, attached := actionSubs[n]
		if hasID && !attached {
			problems = append(problems, fmt.Sprintf("actions[%d].id: action %d is not part of this workflow; leave id out to add a new action", i, n))
			continue
		}
		for _, kind := range []string{"email", "webhook"} {
			subID, ok := nestedID(m[kind])
			if !ok {
				continue
			}
			want := s.email
			if kind == "webhook" {
				want = s.webhook
			}
			if !hasID || subID != want {
				problems = append(problems, fmt.Sprintf("actions[%d].%s.id: %s settings %d don't belong to this action; leave id out to give it new settings", i, kind, kind, subID))
			}
		}
	}
	if len(problems) > 0 {
		return errResult("invalid arguments:\n" + strings.Join(problems, "\n"))
	}
	return nil
}

func anyNestedID(lists ...[]any) bool {
	for _, l := range lists {
		for _, item := range l {
			m, _ := item.(map[string]any)
			if _, ok := nestedID(m); ok {
				return true
			}
			for _, kind := range []string{"email", "webhook"} {
				if _, ok := nestedID(m[kind]); ok {
					return true
				}
			}
		}
	}
	return false
}

func nestedID(v any) (int, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return 0, false
	}
	switch n := m["id"].(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	}
	return 0, false
}

func handleWorkflowList(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}
		if errRes := addPaginationParams(params, request); errRes != nil {
			return errRes, nil
		}
		if raw := request.GetArguments()["trigger_type"]; argIsSet(raw) {
			ws := mustWorkflowSchemas()
			n, ok := ws.normalizeNamed("trigger_type", raw).(int)
			if _, known := ws.labels["trigger_type"][n]; !ok || !known {
				return errResult("trigger_type must be one of: " + ws.describeEnum(map[string]any{namesKey: "trigger_type", "enum": intsToAny(triggerTypeNumbers(ws))})), nil
			}
			params.Set("trigger_type", strconv.Itoa(n))
		}
		if errRes := addBoolParam(params, request, "enabled", "enabled"); errRes != nil {
			return errRes, nil
		}
		if errRes := addIntParam(params, request, "id", "id"); errRes != nil {
			return errRes, nil
		}
		ids, _, err := getJSONInts(request, "id_in")
		if err != nil {
			return errResult(err.Error()), nil
		}
		if len(ids) > 0 {
			parts := make([]string, len(ids))
			for i, n := range ids {
				parts[i] = strconv.Itoa(n)
			}
			params.Set("id__in", strings.Join(parts, ","))
		}
		path := "/api/workflows/"
		resp, err := client.Get(ctx, path, params)
		return doRequest(resp, err, "GET", path)
	}
}

func triggerTypeNumbers(ws *workflowSchemas) []int {
	var nums []int
	for n := range ws.labels["trigger_type"] {
		nums = append(nums, n)
	}
	slices.Sort(nums)
	return nums
}

func intsToAny(xs []int) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = float64(x)
	}
	return out
}

func handleWorkflowRun(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		workflow, errRes := getRequiredInt(request, "workflow")
		if errRes != nil {
			return errRes, nil
		}
		if workflow < 1 {
			return errResult("workflow must be a workflow ID"), nil
		}
		filter, errRes := optionalBool(request, "filter_documents", true)
		if errRes != nil {
			return errRes, nil
		}
		body, _, errRes := getDocumentSelection(request)
		if errRes != nil {
			return errRes, nil
		}
		body["method"] = "run_workflow"
		body["parameters"] = map[string]any{"workflow": workflow, "filter_documents": filter}

		wait, timeout, errRes := getWaitOptions(request)
		if errRes != nil {
			return errRes, nil
		}
		path := "/api/documents/bulk_edit/"
		resp, err := client.Post(ctx, path, body)
		// The workflow runs inside the bulk task: nothing follows on.
		return respondWithWait(ctx, client, request, wait, timeout, resp, err, "POST", path, nil)
	}
}

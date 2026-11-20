package main

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var (
	// uuidPattern validates task IDs as UUIDs to prevent path injection (case-insensitive).
	uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	// logNamePattern validates log file names to prevent path traversal.
	// Disallows ".." sequences to prevent directory traversal.
	logNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+(\.[a-zA-Z0-9_-]+)*$`)
	// runnableTaskTypes are the task types /api/tasks/run/ will dispatch manually.
	runnableTaskTypes = []string{"train_classifier", "sanity_check", "llm_index"}
	// integerIDPattern matches a task's integer primary key.
	integerIDPattern = regexp.MustCompile(`^[0-9]+$`)

	// Values of PaperlessTask.TaskType, TriggerSource and Status (API v10).
	taskTypes = []string{
		"consume_file", "train_classifier", "sanity_check", "index_optimize", "mail_fetch",
		"llm_index", "empty_trash", "check_workflows", "bulk_update", "reprocess_document",
		"build_share_link", "bulk_delete", "apply_ai_suggestions",
	}
	taskTriggerSources = []string{"scheduled", "web_ui", "api_upload", "folder_consume", "email_consume", "system", "manual"}
	taskStatuses       = []string{"pending", "started", "success", "failure", "revoked"}
	// taskOrderingFields are TasksViewSet.ordering_fields.
	taskOrderingFields = []string{"date_created", "date_done", "status", "task_type", "duration_seconds", "wait_time_seconds"}
	// taskStatsModes are the task_stats modes, one per aggregate endpoint.
	taskStatsModes = []string{"summary", "status_counts", "active"}
	// taskCountFilters are the task_list filters /api/tasks/status_counts/ honours
	// (all of them except status and is_complete).
	taskCountFilters = []string{"task_type", "trigger_source", "acknowledged", "owner", "name", "result", "date_created_after", "date_created_before"}
)

// tracebackSnippetLen is how many bytes of result_data.traceback task_list keeps.
const tracebackSnippetLen = 500

// taskFilterOptions returns the schema for the filters task_list shares with
// task_stats status_counts. withStatus adds status and is_complete, which
// status_counts ignores.
func taskFilterOptions(withStatus bool) []mcp.ToolOption {
	opts := []mcp.ToolOption{
		withEnumArray("task_type", taskTypes, mcp.Description("Only these task types (any of)")),
		withEnumArray("trigger_source", taskTriggerSources, mcp.Description("Only these trigger sources (any of)")),
		mcp.WithBoolean("acknowledged", mcp.Description("Filter by acknowledged state")),
		withNumber("owner", mcp.Description("Filter by owner user ID")),
		mcp.WithString("name", mcp.Description("Match the input file name, or the task type / trigger source label (case-insensitive substring)")),
		mcp.WithString("result", mcp.Description("Match result_data.reason or error_message (substring); a number also matches result_data.document_id or duplicate_of, and a value containing \"duplicate\" matches every task that found a duplicate")),
		mcp.WithString("date_created_after", mcp.Description("Created at or after this ISO 8601 datetime; a date alone means midnight at the start of that day")),
		mcp.WithString("date_created_before", mcp.Description("Created at or before this ISO 8601 datetime; a date alone means midnight at the start of that day, so pass the next day (or a datetime) to include all of it")),
	}
	if withStatus {
		opts = append(opts,
			withEnumArray("status", taskStatuses, mcp.Description("Only these statuses (any of)")),
			mcp.WithBoolean("is_complete", mcp.Description("true: success, failure or revoked; false: pending or started")),
		)
	}
	return opts
}

// addTaskFilters adds the task_list filters to params. withStatus adds status
// and is_complete.
func addTaskFilters(params url.Values, request mcp.CallToolRequest, withStatus bool) *mcp.CallToolResult {
	if errRes := addMultiParam(params, request, "task_type", taskTypes); errRes != nil {
		return errRes
	}
	if errRes := addMultiParam(params, request, "trigger_source", taskTriggerSources); errRes != nil {
		return errRes
	}
	addBoolParam(params, request, "acknowledged", "acknowledged")
	addIntParam(params, request, "owner", "owner")
	for _, name := range []string{"name", "result", "date_created_after", "date_created_before"} {
		addStringParam(params, request, name, name)
	}
	if withStatus {
		if errRes := addMultiParam(params, request, "status", taskStatuses); errRes != nil {
			return errRes
		}
		addBoolParam(params, request, "is_complete", "is_complete")
	}
	return nil
}

func registerSystemTools(srv *server.MCPServer, client *Client) {
	// Tasks
	taskListOpts := []mcp.ToolOption{
		mcp.WithDescription("List background tasks, newest first by default. Paginated by the server; use page to iterate. Each task has id (integer, for task_acknowledge), task_id (UUID), task_type, trigger_source, status (pending, started, success, failure, revoked), date_created/started/done, duration_seconds, wait_time_seconds, input_data, result_data (document_id, duplicate_of, reason, error_type, error_message, traceback), related_document_ids and acknowledged. A consume that hit a duplicate has status failure and result_data.duplicate_of. result_data.traceback is cut to its last 500 bytes (traceback_truncated: true) unless include_traceback is set; task_get returns it in full. For counts use task_stats."),
		withNumber("page", mcp.Description("Page number (default: 1)")),
		withNumber("page_size", mcp.Description("Results per page (default: 25)")),
		mcp.WithString("ordering", mcp.Description("Sort field: "+strings.Join(taskOrderingFields, ", ")+"; prefix - for descending (default: -date_created)")),
		mcp.WithBoolean("include_traceback", mcp.Description("Keep result_data.traceback whole (default: false)")),
	}
	srv.AddTool(mcp.NewTool("task_list", append(taskListOpts, taskFilterOptions(true)...)...), handleTaskList(client))

	srv.AddTool(mcp.NewTool("task_get",
		mcp.WithDescription("Get one background task by its task UUID (task_id, as returned by document_upload and task_run) or its integer id (from task_list). Returns the task with result_data in full."),
		mcp.WithAny("id", mcp.Description("Task UUID or integer task id"), mcp.Required(), func(schema map[string]any) {
			schema["type"] = []string{"string", "integer"}
		}),
	), handleTaskGet(client))

	srv.AddTool(mcp.NewTool("task_acknowledge",
		mcp.WithDescription("Acknowledge tasks, which hides them from the web UI's task notifications; nothing is deleted, and there is no way to un-acknowledge. Give exactly one of tasks or all. Returns {\"result\": number acknowledged}."),
		withJSONArray("tasks", mcp.Description("Array of integer task IDs (the id field from task_list, not the task UUID)")),
		mcp.WithBoolean("all", mcp.Description("Acknowledge every visible unacknowledged task, including pending and failed ones")),
	), handleTaskAcknowledge(client))

	statsOpts := []mcp.ToolOption{
		mcp.WithDescription("Aggregate task information. mode summary: per task type over the last N days (days), total/pending/success/failure counts, average duration and wait, and last run/success/failure times. mode status_counts: counts of all, needs_attention (failure or revoked), in_progress (pending or started) and completed (success); accepts task_list's filters except status and is_complete. mode active: pending and started tasks, newest first, at most 50. Parameters that don't belong to the chosen mode are rejected."),
		mcp.WithString("mode", mcp.Description("What to return"), mcp.Required(), mcp.Enum(taskStatsModes...)),
		withNumber("days", mcp.Description("summary only: how many days back, 1 to 365 (default: 30)")),
	}
	srv.AddTool(mcp.NewTool("task_stats", append(statsOpts, taskFilterOptions(false)...)...), handleTaskStats(client))

	srv.AddTool(mcp.NewTool("task_run",
		append([]mcp.ToolOption{
			mcp.WithDescription("Run a system task in the background (superuser only). Returns a task_id at once; pass wait: true to wait for the task and get its outcome, or follow the id with task_wait or task_get. Types: train_classifier (retrain the auto-matching classifier), sanity_check (verify stored files against the database), llm_index (update the AI document index; requires AI to be enabled on the server)."),
			mcp.WithString("task_type", mcp.Description("Task type to run"), mcp.Required(), mcp.Enum(runnableTaskTypes...)),
		}, waitToolOptions()...)...,
	), handleTaskRun(client))

	srv.AddTool(mcp.NewTool("task_wait",
		mcp.WithDescription("Wait for a background task to finish: success, failure or revoked. Returns {task, waited_seconds}; a failed or revoked task comes back as a tool error carrying the task, and if the timeout passes first the current task comes back with timed_out: true (call task_wait again to keep waiting). The MCP host may end a long call before the timeout. Takes the task UUID that document_upload, document_bulk_edit and task_run return, or the integer id from task_list. For a bulk edit, finished means the bulk task: PDF operations then run consume_file (or reprocess_document) tasks of their own."),
		mcp.WithAny("id", mcp.Description("Task UUID or integer task id"), mcp.Required(), func(schema map[string]any) {
			schema["type"] = []string{"string", "integer"}
		}),
		withNumber("timeout", mcp.Description(fmt.Sprintf("Seconds to wait, 1 to %d (default: %d)", int(maxTaskWaitTimeout.Seconds()), int(defaultTaskWaitTimeout.Seconds())))),
	), handleTaskWait(client))

	// Logs
	srv.AddTool(mcp.NewTool("log_list",
		mcp.WithDescription("List available log files."),
	), handleSimpleGet(client, "/api/logs/"))

	srv.AddTool(mcp.NewTool("log_get",
		mcp.WithDescription("Get the last lines of a log file (default: last 100)."),
		mcp.WithString("id", mcp.Description("Log file name"), mcp.Required()),
		withNumber("tail", mcp.Description("Number of lines to return from the end of the log (default: 100)")),
	), handleLogGet(client))

	// Trash
	srv.AddTool(mcp.NewTool("trash_list",
		mcp.WithDescription("List trashed documents."),
		withNumber("page", mcp.Description("Page number (default: 1)")),
		withNumber("page_size", mcp.Description("Results per page (default: 25)")),
	), handlePaginatedList(client, "/api/trash/"))

	srv.AddTool(mcp.NewTool("trash_action",
		mcp.WithDescription("Restore or permanently delete trashed documents."),
		mcp.WithString("action", mcp.Description("Action: restore or empty"), mcp.Required()),
		withJSONArray("documents", mcp.Description("Array of document IDs (required for restore)")),
	), handleTrashAction(client))

	// System
	srv.AddTool(mcp.NewTool("system_status",
		mcp.WithDescription("Get system status including version, database, storage info (admin only)."),
	), handleSimpleGet(client, "/api/status/"))

	srv.AddTool(mcp.NewTool("remote_version",
		mcp.WithDescription("Check for available Paperless-ngx updates."),
	), handleSimpleGet(client, "/api/remote_version/"))

	srv.AddTool(mcp.NewTool("ui_settings_get",
		mcp.WithDescription("Get UI settings for the current user."),
	), handleSimpleGet(client, "/api/ui_settings/"))

	// Config
	srv.AddTool(mcp.NewTool("config_list",
		mcp.WithDescription("List application configuration entries."),
	), handleSimpleGet(client, "/api/config/"))

	srv.AddTool(mcp.NewTool("config_get",
		mcp.WithDescription("Get a configuration entry."),
		withNumber("id", mcp.Description("Config entry ID"), mcp.Required()),
	), handleGetByID(client, "/api/config/%d/"))

	srv.AddTool(mcp.NewTool("config_update",
		mcp.WithDescription("Update a configuration entry."),
		withNumber("id", mcp.Description("Config entry ID"), mcp.Required()),
		withJSONObject("body", mcp.Description("Object with fields to update"), mcp.Required()),
	), handleConfigUpdate(client))
}

// handleTaskList lists background tasks with server-side pagination and the
// v10 task filters, trimming tracebacks unless include_traceback is set.
func handleTaskList(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}
		addPaginationParams(params, request)
		if errRes := addTaskFilters(params, request, true); errRes != nil {
			return errRes, nil
		}
		if ordering := request.GetString("ordering", ""); ordering != "" {
			if !slices.Contains(taskOrderingFields, strings.TrimPrefix(ordering, "-")) {
				return errResult(fmt.Sprintf("ordering must be one of: %s (prefix - for descending)", strings.Join(taskOrderingFields, ", "))), nil
			}
			params.Set("ordering", ordering)
		}
		includeTraceback := request.GetBool("include_traceback", false)

		path := "/api/tasks/"
		resp, err := client.Get(ctx, path, params)
		return doRequestJSON(resp, err, "GET", path, func(v any) any {
			if !includeTraceback {
				if m, ok := v.(map[string]any); ok {
					truncateTracebacks(m["results"])
				}
			}
			return v
		})
	}
}

// truncateTracebacks keeps the last tracebackSnippetLen bytes of each task's
// result_data.traceback (the end, where the exception is), marking shortened
// ones with result_data.traceback_truncated.
func truncateTracebacks(items any) {
	arr, ok := items.([]any)
	if !ok {
		return
	}
	for _, item := range arr {
		task, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rd, ok := task["result_data"].(map[string]any)
		if !ok {
			continue
		}
		tb, ok := rd["traceback"].(string)
		if !ok || len(tb) <= tracebackSnippetLen {
			continue
		}
		rd["traceback"] = "…" + truncateUTF8Tail(tb, tracebackSnippetLen)
		rd["traceback_truncated"] = true
	}
}

// handleTaskGet returns one task. An integer id reads /api/tasks/{id}/; a UUID
// goes through the task_id filter, whose paginated envelope is unwrapped.
func handleTaskGet(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := taskIDArg(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		_, raw, found, errRes := fetchTask(ctx, client, id)
		if errRes != nil {
			return errRes, nil
		}
		if !found {
			return errResult("no task with task_id " + strings.ToLower(id)), nil
		}
		return rawJSONResult(raw)
	}
}

func handleTaskAcknowledge(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]any{}
		if err := setJSONField(body, request, "tasks"); err != nil {
			return errResult(err.Error()), nil
		}
		tasks, hasTasks := body["tasks"]
		if hasTasks && tasks == nil {
			// JSON null: treat as not given.
			hasTasks = false
			delete(body, "tasks")
		}
		if hasTasks {
			ids, ok := tasks.([]any)
			if !ok {
				return errResult("tasks must be a JSON array of integer task IDs"), nil
			}
			for _, v := range ids {
				if f, ok := v.(float64); !ok || f != float64(int64(f)) {
					return errResult("tasks must be a JSON array of integer task IDs"), nil
				}
			}
		}
		all := request.GetBool("all", false)
		switch {
		case all && hasTasks:
			return errResult("Set either all or tasks, not both."), nil
		case !all && !hasTasks:
			return errResult("Either all must be true or tasks must be provided."), nil
		case all:
			body = map[string]any{"all": true}
		}

		path := "/api/tasks/acknowledge/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}

// handleTaskStats serves the three aggregate task endpoints, one per mode,
// rejecting parameters that don't belong to the chosen mode.
func handleTaskStats(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		mode, errRes := getRequiredString(request, "mode")
		if errRes != nil {
			return errRes, nil
		}
		var allowed []string
		switch mode {
		case "summary":
			allowed = []string{"days"}
		case "status_counts":
			allowed = taskCountFilters
		case "active":
		default:
			return errResult(fmt.Sprintf("mode must be one of: %s", strings.Join(taskStatsModes, ", "))), nil
		}
		for name, v := range request.GetArguments() {
			// Some clients send every optional property, null when unused.
			if v != nil && name != "mode" && !slices.Contains(allowed, name) {
				return errResult(fmt.Sprintf("%s does not apply to mode %s", name, mode)), nil
			}
		}

		params := url.Values{}
		switch mode {
		case "summary":
			if v, ok := request.GetArguments()["days"]; ok && v != nil {
				days := request.GetFloat("days", 0)
				if days < 1 || days > 365 || days != float64(int(days)) {
					return errResult("days must be an integer from 1 to 365"), nil
				}
				params.Set("days", strconv.Itoa(int(days)))
			}
		case "status_counts":
			if errRes := addTaskFilters(params, request, false); errRes != nil {
				return errRes, nil
			}
		}

		path := "/api/tasks/" + mode + "/"
		resp, err := client.Get(ctx, path, params)
		return doRequest(resp, err, "GET", path)
	}
}

func handleTaskRun(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		taskType, errRes := getRequiredString(request, "task_type")
		if errRes != nil {
			return errRes, nil
		}
		if !slices.Contains(runnableTaskTypes, taskType) {
			return errResult(fmt.Sprintf("task_type must be one of: %s", strings.Join(runnableTaskTypes, ", "))), nil
		}

		wait, timeout, errRes := getWaitOptions(request)
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{"task_type": taskType}
		path := "/api/tasks/run/"
		resp, err := client.Post(ctx, path, body)
		return respondWithWait(ctx, client, request, wait, timeout, resp, err, "POST", path, nil)
	}
}

func handleLogGet(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredString(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		if !logNamePattern.MatchString(id) {
			return errResult("id must contain only alphanumeric characters, dots, hyphens, and underscores"), nil
		}
		tail := int(request.GetFloat("tail", 100))
		if tail < 1 {
			tail = 100
		}
		path := fmt.Sprintf("/api/logs/%s/", id)
		resp, err := client.Get(ctx, path, nil)
		return doRequestJSON(resp, err, "GET", path, func(v any) any {
			lines, ok := v.([]any)
			if !ok {
				return v
			}
			return map[string]any{
				"total_lines": len(lines),
				"lines":       lines[max(len(lines)-tail, 0):],
			}
		})
	}
}

func handleTrashAction(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		action, errRes := getRequiredString(request, "action")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{"action": action}
		if err := setJSONField(body, request, "documents"); err != nil {
			return errResult(err.Error()), nil
		}

		path := "/api/trash/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}

func handleConfigUpdate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{}
		if err := setJSONField(body, request, "body"); err != nil {
			return errResult(err.Error()), nil
		}
		if parsed, ok := body["body"].(map[string]any); ok {
			body = parsed
		} else if _, ok := body["body"]; ok {
			return errResult("body must be a JSON object"), nil
		}
		if len(body) == 0 {
			return errResult("body is required"), nil
		}

		path := fmt.Sprintf("/api/config/%d/", id)
		resp, err := client.Patch(ctx, path, body)
		return doRequest(resp, err, "PATCH", path)
	}
}

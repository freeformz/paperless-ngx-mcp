package main

import (
	"context"
	"fmt"
	"net/url"
	"regexp"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var (
	// uuidPattern validates task IDs as UUIDs to prevent path injection (case-insensitive).
	uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	// logNamePattern validates log file names to prevent path traversal.
	// Disallows ".." sequences to prevent directory traversal.
	logNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+(\.[a-zA-Z0-9_-]+)*$`)
)

func registerSystemTools(srv *server.MCPServer, client *Client) {
	// Tasks
	srv.AddTool(mcp.NewTool("task_list",
		mcp.WithDescription("List background tasks (e.g., document consumption). Paginated; use page to iterate."),
		withNumber("page", mcp.Description("Page number (default: 1)")),
		withNumber("page_size", mcp.Description("Results per page (default: 25)")),
	), handleTaskList(client))

	srv.AddTool(mcp.NewTool("task_get",
		mcp.WithDescription("Get background task details by task UUID (as returned by document_upload)."),
		mcp.WithString("id", mcp.Description("Task UUID"), mcp.Required()),
	), handleTaskGet(client))

	srv.AddTool(mcp.NewTool("task_acknowledge",
		mcp.WithDescription("Acknowledge completed tasks to clear them from the list."),
		mcp.WithString("tasks", mcp.Description("JSON array of integer task IDs (the id field from task_list, not the task UUID)"), mcp.Required()),
	), handleTaskAcknowledge(client))

	srv.AddTool(mcp.NewTool("task_run",
		mcp.WithDescription("Run a system task (admin only). One of: index_optimize, train_classifier, check_sanity."),
		mcp.WithString("task_name", mcp.Description("Task name to run"), mcp.Required()),
	), handleTaskRun(client))

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
		mcp.WithString("documents", mcp.Description("JSON array of document IDs (required for restore)")),
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
		mcp.WithString("body", mcp.Description("JSON object with fields to update"), mcp.Required()),
	), handleConfigUpdate(client))
}

// handleTaskList lists background tasks. The /api/tasks/ endpoint ignores
// pagination parameters and returns a bare array of every task, so pagination
// is applied client-side.
func handleTaskList(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		page, pageSize := getPagination(request)
		path := "/api/tasks/"
		resp, err := client.Get(ctx, path, nil)
		return doRequestJSON(resp, err, "GET", path, func(v any) any {
			return paginateArray(v, page, pageSize)
		})
	}
}

func handleTaskGet(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredString(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		if !uuidPattern.MatchString(id) {
			return errResult("id must be a valid UUID"), nil
		}
		// Tasks are keyed by integer pk; UUID lookup goes through the task_id filter.
		path := "/api/tasks/"
		params := url.Values{"task_id": {id}}
		resp, err := client.Get(ctx, path, params)
		return doRequest(resp, err, "GET", path+"?"+params.Encode())
	}
}

func handleTaskAcknowledge(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]any{}
		if err := setJSONField(body, request, "tasks"); err != nil {
			return errResult(err.Error()), nil
		}
		if _, ok := body["tasks"]; !ok {
			return errResult("tasks is required"), nil
		}

		path := "/api/tasks/acknowledge/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}

func handleTaskRun(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		taskName, errRes := getRequiredString(request, "task_name")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{"task_name": taskName}
		path := "/api/tasks/run/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
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

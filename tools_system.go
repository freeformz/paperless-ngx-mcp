package main

import (
	"context"
	"fmt"
	"regexp"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var (
	// uuidPattern validates task IDs as UUIDs to prevent path injection.
	uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	// logNamePattern validates log file names to prevent path traversal.
	logNamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
)

func registerSystemTools(srv *server.MCPServer, client *Client) {
	// Tasks
	srv.AddTool(mcp.NewTool("task_list",
		mcp.WithDescription("List background tasks (e.g., document consumption)."),
		mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
		mcp.WithNumber("page_size", mcp.Description("Results per page (default: 25)")),
	), handlePaginatedList(client, "/api/tasks/"))

	srv.AddTool(mcp.NewTool("task_get",
		mcp.WithDescription("Get background task details."),
		mcp.WithString("id", mcp.Description("Task UUID"), mcp.Required()),
	), handleTaskGet(client))

	srv.AddTool(mcp.NewTool("task_acknowledge",
		mcp.WithDescription("Acknowledge completed tasks to clear them from the list."),
		mcp.WithString("tasks", mcp.Description("JSON array of task UUIDs to acknowledge"), mcp.Required()),
	), handleTaskAcknowledge(client))

	srv.AddTool(mcp.NewTool("task_run",
		mcp.WithDescription("Run a system task (admin only). E.g., index_optimize, index_reindex."),
		mcp.WithString("task_name", mcp.Description("Task name to run"), mcp.Required()),
	), handleTaskRun(client))

	// Logs
	srv.AddTool(mcp.NewTool("log_list",
		mcp.WithDescription("List available log files."),
	), handleSimpleGet(client, "/api/logs/"))

	srv.AddTool(mcp.NewTool("log_get",
		mcp.WithDescription("Get log file contents."),
		mcp.WithString("id", mcp.Description("Log file name"), mcp.Required()),
	), handleLogGet(client))

	// Trash
	srv.AddTool(mcp.NewTool("trash_list",
		mcp.WithDescription("List trashed documents."),
		mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
		mcp.WithNumber("page_size", mcp.Description("Results per page (default: 25)")),
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
		mcp.WithNumber("id", mcp.Description("Config entry ID"), mcp.Required()),
	), handleGetByID(client, "/api/config/%d/"))

	srv.AddTool(mcp.NewTool("config_update",
		mcp.WithDescription("Update a configuration entry."),
		mcp.WithNumber("id", mcp.Description("Config entry ID"), mcp.Required()),
		mcp.WithString("body", mcp.Description("JSON object with fields to update"), mcp.Required()),
	), handleConfigUpdate(client))
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
		path := fmt.Sprintf("/api/tasks/%s/", id)
		resp, err := client.Get(ctx, path, nil)
		return doRequest(resp, err, "GET", path)
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
		path := fmt.Sprintf("/api/logs/%s/", id)
		resp, err := client.Get(ctx, path, nil)
		return doRequest(resp, err, "GET", path)
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
		}
		if len(body) == 0 {
			return errResult("body is required"), nil
		}

		path := fmt.Sprintf("/api/config/%d/", id)
		resp, err := client.Patch(ctx, path, body)
		return doRequest(resp, err, "PATCH", path)
	}
}

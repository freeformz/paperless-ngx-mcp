package main

import (
	"context"
	"fmt"
	"maps"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerWorkflowTools(srv *server.MCPServer, client *Client) {
	// Workflows
	srv.AddTool(mcp.NewTool("workflow_list",
		mcp.WithDescription("List workflows."),
		mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
		mcp.WithNumber("page_size", mcp.Description("Results per page (default: 25)")),
	), handlePaginatedList(client, "/api/workflows/"))

	srv.AddTool(mcp.NewTool("workflow_get",
		mcp.WithDescription("Get workflow details."),
		mcp.WithNumber("id", mcp.Description("Workflow ID"), mcp.Required()),
	), handleGetByID(client, "/api/workflows/%d/"))

	srv.AddTool(mcp.NewTool("workflow_create",
		mcp.WithDescription("Create a workflow."),
		mcp.WithString("name", mcp.Description("Workflow name"), mcp.Required()),
		mcp.WithString("body", mcp.Description("JSON object with workflow configuration"), mcp.Required()),
	), handleWorkflowCreate(client))

	srv.AddTool(mcp.NewTool("workflow_update",
		mcp.WithDescription("Update a workflow."),
		mcp.WithNumber("id", mcp.Description("Workflow ID"), mcp.Required()),
		mcp.WithString("body", mcp.Description("JSON object with fields to update"), mcp.Required()),
	), handleWorkflowUpdate(client))

	srv.AddTool(mcp.NewTool("workflow_delete",
		mcp.WithDescription("Delete a workflow."),
		mcp.WithNumber("id", mcp.Description("Workflow ID"), mcp.Required()),
	), handleDeleteByID(client, "/api/workflows/%d/"))

	// Workflow Triggers
	srv.AddTool(mcp.NewTool("workflow_trigger_list",
		mcp.WithDescription("List workflow triggers."),
		mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
		mcp.WithNumber("page_size", mcp.Description("Results per page (default: 25)")),
	), handlePaginatedList(client, "/api/workflow_triggers/"))

	srv.AddTool(mcp.NewTool("workflow_trigger_get",
		mcp.WithDescription("Get workflow trigger details."),
		mcp.WithNumber("id", mcp.Description("Trigger ID"), mcp.Required()),
	), handleGetByID(client, "/api/workflow_triggers/%d/"))

	srv.AddTool(mcp.NewTool("workflow_trigger_create",
		mcp.WithDescription("Create a workflow trigger."),
		mcp.WithString("body", mcp.Description("JSON object with trigger configuration"), mcp.Required()),
	), genericJSONCreate(client, "/api/workflow_triggers/", nil))

	srv.AddTool(mcp.NewTool("workflow_trigger_update",
		mcp.WithDescription("Update a workflow trigger."),
		mcp.WithNumber("id", mcp.Description("Trigger ID"), mcp.Required()),
		mcp.WithString("body", mcp.Description("JSON object with fields to update"), mcp.Required()),
	), handleGenericJSONUpdateByID(client, "/api/workflow_triggers/%d/"))

	srv.AddTool(mcp.NewTool("workflow_trigger_delete",
		mcp.WithDescription("Delete a workflow trigger."),
		mcp.WithNumber("id", mcp.Description("Trigger ID"), mcp.Required()),
	), handleDeleteByID(client, "/api/workflow_triggers/%d/"))

	// Workflow Actions
	srv.AddTool(mcp.NewTool("workflow_action_list",
		mcp.WithDescription("List workflow actions."),
		mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
		mcp.WithNumber("page_size", mcp.Description("Results per page (default: 25)")),
	), handlePaginatedList(client, "/api/workflow_actions/"))

	srv.AddTool(mcp.NewTool("workflow_action_get",
		mcp.WithDescription("Get workflow action details."),
		mcp.WithNumber("id", mcp.Description("Action ID"), mcp.Required()),
	), handleGetByID(client, "/api/workflow_actions/%d/"))

	srv.AddTool(mcp.NewTool("workflow_action_create",
		mcp.WithDescription("Create a workflow action."),
		mcp.WithString("body", mcp.Description("JSON object with action configuration"), mcp.Required()),
	), genericJSONCreate(client, "/api/workflow_actions/", nil))

	srv.AddTool(mcp.NewTool("workflow_action_update",
		mcp.WithDescription("Update a workflow action."),
		mcp.WithNumber("id", mcp.Description("Action ID"), mcp.Required()),
		mcp.WithString("body", mcp.Description("JSON object with fields to update"), mcp.Required()),
	), handleGenericJSONUpdateByID(client, "/api/workflow_actions/%d/"))

	srv.AddTool(mcp.NewTool("workflow_action_delete",
		mcp.WithDescription("Delete a workflow action."),
		mcp.WithNumber("id", mcp.Description("Action ID"), mcp.Required()),
	), handleDeleteByID(client, "/api/workflow_actions/%d/"))
}

// genericJSONCreate parses a JSON "body" param, merges in extra fields, and POSTs.
func genericJSONCreate(client *Client, apiPath string, extraFields map[string]any) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]any{}
		if err := setJSONField(body, request, "body"); err != nil {
			return errResult(err.Error()), nil
		}
		if parsed, ok := body["body"].(map[string]any); ok {
			body = parsed
		} else if _, ok := body["body"]; !ok {
			return errResult("body is required"), nil
		} else {
			return errResult("body must be a JSON object"), nil
		}
		maps.Copy(body, extraFields)

		resp, err := client.Post(ctx, apiPath, body)
		return doRequest(resp, err, "POST", apiPath)
	}
}

// genericJSONUpdate parses a JSON "body" param and PATCHes.
func genericJSONUpdate(client *Client, apiPath string) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

		resp, err := client.Patch(ctx, apiPath, body)
		return doRequest(resp, err, "PATCH", apiPath)
	}
}

// handleGenericJSONUpdateByID extracts an integer ID, then delegates to genericJSONUpdate.
func handleGenericJSONUpdateByID(client *Client, pathFmt string) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf(pathFmt, id)
		handler := genericJSONUpdate(client, path)
		return handler(ctx, request)
	}
}

func handleWorkflowCreate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, errRes := getRequiredString(request, "name")
		if errRes != nil {
			return errRes, nil
		}
		path := "/api/workflows/"
		handler := genericJSONCreate(client, path, map[string]any{"name": name})
		return handler(ctx, request)
	}
}

func handleWorkflowUpdate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/workflows/%d/", id)
		handler := genericJSONUpdate(client, path)
		return handler(ctx, request)
	}
}

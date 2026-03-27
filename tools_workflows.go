package main

import (
	"context"
	"fmt"
	"maps"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerWorkflowTools(srv *server.MCPServer, client *Client) {
	// Workflows
	srv.AddTool(mcp.NewTool("workflow_list",
		mcp.WithDescription("List workflows."),
		mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
		mcp.WithNumber("page_size", mcp.Description("Results per page (default: 25)")),
	), handleWorkflowList(client))

	srv.AddTool(mcp.NewTool("workflow_get",
		mcp.WithDescription("Get workflow details."),
		mcp.WithNumber("id", mcp.Description("Workflow ID"), mcp.Required()),
	), handleWorkflowGet(client))

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
	), handleWorkflowDelete(client))

	// Workflow Triggers
	srv.AddTool(mcp.NewTool("workflow_trigger_list",
		mcp.WithDescription("List workflow triggers."),
		mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
		mcp.WithNumber("page_size", mcp.Description("Results per page (default: 25)")),
	), handleWorkflowTriggerList(client))

	srv.AddTool(mcp.NewTool("workflow_trigger_get",
		mcp.WithDescription("Get workflow trigger details."),
		mcp.WithNumber("id", mcp.Description("Trigger ID"), mcp.Required()),
	), handleWorkflowTriggerGet(client))

	srv.AddTool(mcp.NewTool("workflow_trigger_create",
		mcp.WithDescription("Create a workflow trigger."),
		mcp.WithString("body", mcp.Description("JSON object with trigger configuration"), mcp.Required()),
	), handleWorkflowTriggerCreate(client))

	srv.AddTool(mcp.NewTool("workflow_trigger_update",
		mcp.WithDescription("Update a workflow trigger."),
		mcp.WithNumber("id", mcp.Description("Trigger ID"), mcp.Required()),
		mcp.WithString("body", mcp.Description("JSON object with fields to update"), mcp.Required()),
	), handleWorkflowTriggerUpdate(client))

	srv.AddTool(mcp.NewTool("workflow_trigger_delete",
		mcp.WithDescription("Delete a workflow trigger."),
		mcp.WithNumber("id", mcp.Description("Trigger ID"), mcp.Required()),
	), handleWorkflowTriggerDelete(client))

	// Workflow Actions
	srv.AddTool(mcp.NewTool("workflow_action_list",
		mcp.WithDescription("List workflow actions."),
		mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
		mcp.WithNumber("page_size", mcp.Description("Results per page (default: 25)")),
	), handleWorkflowActionList(client))

	srv.AddTool(mcp.NewTool("workflow_action_get",
		mcp.WithDescription("Get workflow action details."),
		mcp.WithNumber("id", mcp.Description("Action ID"), mcp.Required()),
	), handleWorkflowActionGet(client))

	srv.AddTool(mcp.NewTool("workflow_action_create",
		mcp.WithDescription("Create a workflow action."),
		mcp.WithString("body", mcp.Description("JSON object with action configuration"), mcp.Required()),
	), handleWorkflowActionCreate(client))

	srv.AddTool(mcp.NewTool("workflow_action_update",
		mcp.WithDescription("Update a workflow action."),
		mcp.WithNumber("id", mcp.Description("Action ID"), mcp.Required()),
		mcp.WithString("body", mcp.Description("JSON object with fields to update"), mcp.Required()),
	), handleWorkflowActionUpdate(client))

	srv.AddTool(mcp.NewTool("workflow_action_delete",
		mcp.WithDescription("Delete a workflow action."),
		mcp.WithNumber("id", mcp.Description("Action ID"), mcp.Required()),
	), handleWorkflowActionDelete(client))
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
		}
		maps.Copy(body, extraFields)

		resp, err := client.Post(apiPath, body)
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
		}
		if len(body) == 0 {
			return errResult("body is required"), nil
		}

		resp, err := client.Patch(apiPath, body)
		return doRequest(resp, err, "PATCH", apiPath)
	}
}

func handleWorkflowList(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}
		addPaginationParams(params, request)
		path := "/api/workflows/"
		resp, err := client.Get(path, params)
		return doRequest(resp, err, "GET", path)
	}
}

func handleWorkflowGet(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/workflows/%d/", id)
		resp, err := client.Get(path, nil)
		return doRequest(resp, err, "GET", path)
	}
}

func handleWorkflowCreate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := request.GetString("name", "")
		if name == "" {
			return errResult("name is required"), nil
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

func handleWorkflowDelete(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/workflows/%d/", id)
		resp, err := client.Delete(path, nil)
		return doRequest(resp, err, "DELETE", path)
	}
}

func handleWorkflowTriggerList(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}
		addPaginationParams(params, request)
		path := "/api/workflow_triggers/"
		resp, err := client.Get(path, params)
		return doRequest(resp, err, "GET", path)
	}
}

func handleWorkflowTriggerGet(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/workflow_triggers/%d/", id)
		resp, err := client.Get(path, nil)
		return doRequest(resp, err, "GET", path)
	}
}

func handleWorkflowTriggerCreate(client *Client) server.ToolHandlerFunc {
	return genericJSONCreate(client, "/api/workflow_triggers/", nil)
}

func handleWorkflowTriggerUpdate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/workflow_triggers/%d/", id)
		handler := genericJSONUpdate(client, path)
		return handler(ctx, request)
	}
}

func handleWorkflowTriggerDelete(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/workflow_triggers/%d/", id)
		resp, err := client.Delete(path, nil)
		return doRequest(resp, err, "DELETE", path)
	}
}

func handleWorkflowActionList(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}
		addPaginationParams(params, request)
		path := "/api/workflow_actions/"
		resp, err := client.Get(path, params)
		return doRequest(resp, err, "GET", path)
	}
}

func handleWorkflowActionGet(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/workflow_actions/%d/", id)
		resp, err := client.Get(path, nil)
		return doRequest(resp, err, "GET", path)
	}
}

func handleWorkflowActionCreate(client *Client) server.ToolHandlerFunc {
	return genericJSONCreate(client, "/api/workflow_actions/", nil)
}

func handleWorkflowActionUpdate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/workflow_actions/%d/", id)
		handler := genericJSONUpdate(client, path)
		return handler(ctx, request)
	}
}

func handleWorkflowActionDelete(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/workflow_actions/%d/", id)
		resp, err := client.Delete(path, nil)
		return doRequest(resp, err, "DELETE", path)
	}
}

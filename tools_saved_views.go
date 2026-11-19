package main

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const savedViewRulesHint = "Rules are combined with AND; a multi-value rule type takes one rule per value. Call saved_view_rule_types for every rule_type code, its value format and the matching document_list filter."

const savedViewVisibilityHint = "Whether a view shows on the dashboard or in the sidebar is a per-user setting changed in the web UI; ui_settings_get shows it under settings.saved_views (dashboard_views_visible_ids, sidebar_views_visible_ids)."

// filterRuleItems describes one filter rule for clients that validate arrays.
func filterRuleItems(schema map[string]any) {
	schema["items"] = map[string]any{
		"type":       "object",
		"properties": map[string]any{"rule_type": map[string]any{"type": "integer"}, "value": map[string]any{"type": []string{"string", "number", "boolean", "null"}}},
		"required":   []string{"rule_type"},
	}
}

// savedViewFieldOptions are the writable saved view fields shared by create
// and update. filter_rules is added separately because it is required on create.
func savedViewFieldOptions() []mcp.ToolOption {
	return []mcp.ToolOption{
		mcp.WithString("icon", mcp.Description("Icon name (default: funnel). One of: "+strings.Join(savedViewIcons, ", "))),
		mcp.WithString("sort_field", mcp.Description("Document field to sort by, e.g. created, added, title, archive_serial_number")),
		mcp.WithBoolean("sort_reverse", mcp.Description("Reverse sort order")),
		withNullableNumber("page_size", mcp.Description("Documents per page, 1 or more; null clears it (use the user's default)")),
		mcp.WithAny("display_mode", mcp.Description("How documents are shown: "+strings.Join(savedViewDisplayModes, ", ")+"; null clears it"), func(schema map[string]any) {
			schema["type"] = []string{"string", "null"}
		}),
		withJSONArray("display_fields", mcp.Items(map[string]any{"type": "string"}), mcp.Description("Array of fields to show, from: "+strings.Join(savedViewDisplayFields, ", ")+", or custom_field_<id> for an existing custom field; null or [] clears it (use the defaults)"), func(schema map[string]any) {
			schema["type"] = []string{"array", "string", "null"}
		}),
	}
}

func registerSavedViewTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("saved_view_list",
			mcp.WithDescription("List saved views, with their filter_rules ([{rule_type, value}]; see saved_view_rule_types for the codes), sort, icon and display settings. "+savedViewVisibilityHint),
			withNumber("page", mcp.Description("Page number (default: 1)")),
			withNumber("page_size", mcp.Description("Results per page (default: 25)")),
		),
		handlePaginatedList(client, "/api/saved_views/"),
	)

	srv.AddTool(
		mcp.NewTool("saved_view_get",
			mcp.WithDescription("Get saved view details. filter_rules is a list of {rule_type, value}; see saved_view_rule_types for the codes."),
			withNumber("id", mcp.Description("Saved view ID"), mcp.Required()),
		),
		handleGetByID(client, "/api/saved_views/%d/"),
	)

	srv.AddTool(
		mcp.NewTool("saved_view_create",
			append([]mcp.ToolOption{
				mcp.WithDescription("Create a saved view: a named document filter with sort and display settings. " + savedViewRulesHint + " " + savedViewVisibilityHint),
				mcp.WithString("name", mcp.Description("View name"), mcp.Required()),
				withJSONArray("filter_rules", mcp.Description("Array of {\"rule_type\": <code>, \"value\": <string or null; numbers and booleans are sent as text>}, at most 255 characters per value; [] matches every document"), mcp.Required(), filterRuleItems),
			}, savedViewFieldOptions()...)...,
		),
		handleSavedViewCreate(client),
	)

	srv.AddTool(
		mcp.NewTool("saved_view_update",
			append([]mcp.ToolOption{
				mcp.WithDescription("Update a saved view; only the fields given change. " + savedViewRulesHint + " " + savedViewVisibilityHint),
				withNumber("id", mcp.Description("Saved view ID"), mcp.Required()),
				mcp.WithString("name", mcp.Description("View name")),
				withJSONArray("filter_rules", mcp.Description("Array of {\"rule_type\": <code>, \"value\": <string or null; numbers and booleans are sent as text>}, at most 255 characters per value; replaces all of the view's rules"), filterRuleItems),
			}, savedViewFieldOptions()...)...,
		),
		handleSavedViewUpdate(client),
	)

	srv.AddTool(
		mcp.NewTool("saved_view_delete",
			mcp.WithDescription("Delete a saved view."),
			withNumber("id", mcp.Description("Saved view ID"), mcp.Required()),
		),
		handleDeleteByID(client, "/api/saved_views/%d/"),
	)

	srv.AddTool(
		mcp.NewTool("saved_view_rule_types",
			mcp.WithDescription("List the saved view filter rule types (paperless-ngx 3.5.1): each rule_type code with its label, value format, whether it takes one rule per value (multi), the matching /api/documents/ filter, and notes. Answered locally, without calling the server. Use it to read or build a saved view's filter_rules."),
			withJSONArray("ids", mcp.Description("Array of rule_type codes to return (default: all)")),
		),
		handleSavedViewRuleTypes(),
	)
}

// setSavedViewFields copies the optional writable fields into body. Nullable
// fields send null when the argument is explicitly null, which clears them.
func setSavedViewFields(body map[string]any, request mcp.CallToolRequest) *mcp.CallToolResult {
	args := request.GetArguments()
	if v := request.GetString("icon", ""); v != "" {
		body["icon"] = v
	}
	if v := request.GetString("sort_field", ""); v != "" {
		body["sort_field"] = v
	}
	if _, ok := args["sort_reverse"]; ok {
		body["sort_reverse"] = request.GetBool("sort_reverse", false)
	}
	setNullableInt(body, args, request, "page_size")
	if v, ok := args["display_mode"]; ok {
		switch v := v.(type) {
		case nil:
			body["display_mode"] = nil
		case string:
			if v != "" {
				body["display_mode"] = v
			}
		default:
			return errResult("display_mode must be a string or null")
		}
	}
	if v, ok := args["display_fields"]; ok && v == nil {
		body["display_fields"] = nil
	} else {
		fields, present, err := getJSONArg(request, "display_fields")
		if err != nil {
			return errResult(err.Error())
		}
		if present && fields == nil {
			body["display_fields"] = nil // the JSON string "null"
		} else if present {
			arr, ok := fields.([]any)
			if !ok {
				return errResult("display_fields must be an array of strings")
			}
			for _, f := range arr {
				if _, ok := f.(string); !ok {
					return errResult("display_fields must be an array of strings")
				}
			}
			if len(arr) == 0 {
				// The server stores [] as null on update but not on create;
				// send null so both store the same "use the defaults".
				body["display_fields"] = nil
			} else {
				body["display_fields"] = arr
			}
		}
	}
	return nil
}

// setFilterRules reads filter_rules and checks its shape: an array of objects
// with an integer rule_type and a string or null value. Unknown rule_type codes
// are left to the server. It reports whether filter_rules was given.
func setFilterRules(body map[string]any, request mcp.CallToolRequest) (bool, *mcp.CallToolResult) {
	v, present, err := getJSONArg(request, "filter_rules")
	if err != nil {
		return false, errResult(err.Error())
	}
	if !present {
		return false, nil
	}
	const shape = `filter_rules must be an array of {"rule_type": <integer>, "value": <string, number, boolean or null>}`
	rules, ok := v.([]any)
	if !ok {
		return false, errResult(shape)
	}
	for _, r := range rules {
		m, ok := r.(map[string]any)
		if !ok {
			return false, errResult(shape)
		}
		if rt, ok := m["rule_type"].(float64); !ok || rt < 0 || rt != float64(int(rt)) {
			return false, errResult(shape)
		}
		// paperless-ngx stores value as text and converts numbers itself;
		// send numbers and booleans in the form the web UI uses.
		switch val := m["value"].(type) {
		case string, nil:
		case float64:
			m["value"] = strconv.FormatFloat(val, 'f', -1, 64)
		case bool:
			m["value"] = strconv.FormatBool(val)
		default:
			return false, errResult(shape)
		}
	}
	body["filter_rules"] = rules
	return true, nil
}

func handleSavedViewCreate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, errRes := getRequiredString(request, "name")
		if errRes != nil {
			return errRes, nil
		}
		body := map[string]any{"name": name}
		present, errRes := setFilterRules(body, request)
		if errRes != nil {
			return errRes, nil
		}
		if !present {
			return errResult("filter_rules is required; pass [] for a view that matches every document"), nil
		}
		if errRes := setSavedViewFields(body, request); errRes != nil {
			return errRes, nil
		}

		path := "/api/saved_views/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}

func handleSavedViewUpdate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		body := map[string]any{}
		if v := request.GetString("name", ""); v != "" {
			body["name"] = v
		}
		if _, errRes := setFilterRules(body, request); errRes != nil {
			return errRes, nil
		}
		if errRes := setSavedViewFields(body, request); errRes != nil {
			return errRes, nil
		}
		if len(body) == 0 {
			return errResult("no fields to update"), nil
		}

		path := fmt.Sprintf("/api/saved_views/%d/", id)
		resp, err := client.Patch(ctx, path, body)
		return doRequest(resp, err, "PATCH", path)
	}
}

// handleSavedViewRuleTypes returns the rule type table, or the entries for the
// requested ids in the order given.
func handleSavedViewRuleTypes() server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		ids, present, err := getJSONInts(request, "ids")
		if err != nil {
			return errResult(err.Error()), nil
		}
		if !present {
			return jsonResult(savedViewRuleTypes)
		}
		out := make([]savedViewRuleType, 0, len(ids))
		for _, id := range ids {
			i := slices.IndexFunc(savedViewRuleTypes, func(r savedViewRuleType) bool { return r.ID == id })
			if i < 0 {
				return errResult(fmt.Sprintf("unknown rule_type %d; codes run from 0 to %d", id, savedViewRuleTypes[len(savedViewRuleTypes)-1].ID)), nil
			}
			out = append(out, savedViewRuleTypes[i])
		}
		return jsonResult(out)
	}
}

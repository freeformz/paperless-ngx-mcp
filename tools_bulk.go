package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerBulkTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("document_bulk_edit",
			append([]mcp.ToolOption{
				mcp.WithDescription("Bulk edit document metadata. parameters by method: set_correspondent {correspondent: id or null}; set_document_type {document_type: id or null}; set_storage_path {storage_path: id or null}; add_tag / remove_tag {tag: id}; modify_tags {add_tags: [id], remove_tags: [id]} (both required, may be empty); modify_custom_fields {add_custom_fields: [id] or {id: value}, remove_custom_fields: [id]} (both required); set_permissions {set_permissions: {view?: {users?: [id], groups?: [id]}, change?: {...}} or null, owner?: id or null, merge?: bool} (needs ownership; without merge, leaving out owner clears it); run_workflow {workflow: id, filter_documents?: bool, default true} (needs view_workflow permission, an enabled workflow with a manual trigger, and ownership). Every method needs change permission on every document. " + documentSelectionNote + " The server queues a bulk task and returns its task_id at once; errors show up in that task. Pass wait: true to wait for it; for these methods the bulk task finishing means the change is done. File operations have their own tools: rotate -> document_rotate, merge -> document_merge, edit_pdf / split / delete_pages -> document_edit_pdf, remove_password -> document_remove_password, reprocess -> document_reprocess, delete -> document_bulk_delete."),
				mcp.WithString("method", mcp.Description("Operation to perform"), mcp.Required(), mcp.Enum(bulkEditMethods...)),
				withJSONObject("parameters", mcp.Description("Object with method-specific parameters, see the description"), mcp.Required()),
			}, append(documentSelectionOptions(), waitToolOptions()...)...)...,
		),
		handleDocumentBulkEdit(client),
	)

	srv.AddTool(
		mcp.NewTool("document_selection_data", append([]mcp.ToolOption{
			mcp.WithDescription("Get per-object document counts (correspondents, tags, document types, storage paths, custom fields) for a selection of documents, to see what a bulk change would touch. Objects with zero matching documents are omitted. It has no total; for the number of documents an all: true selection matches, use document_list with the same filters. " + documentSelectionNote),
		}, documentSelectionOptions()...)...),
		handleDocumentSelectionData(client),
	)

	srv.AddTool(
		mcp.NewTool("bulk_edit_objects",
			mcp.WithDescription("Set permissions on, or delete, tags, correspondents, document types or storage paths. Select them by objects (explicit IDs) or with all: true and filters (the *_list tool's filters: name for every type, is_root for tags, path for storage paths); preview with the *_list tool. all: true acts on the objects you can change (set_permissions) or delete (delete), not merely view, so it can differ from a *_list count; people who aren't superusers must own every selected object, and because the filters can't select by owner, all: true usually fails with a 403 for them. For tags, all: true also takes editable child tags, and deleting a tag always deletes its child tags too, including ones you can't edit (a 403 for a non-superuser if any isn't theirs). set_permissions takes owner, permissions and merge: without merge, each action given in permissions replaces its users and groups (an empty array revokes) and change implies view; with merge, they are added, and owner is set only on objects that have no owner. Leaving out owner keeps it; clear: [\"owner\"] removes it. "+clearHint+"."),
			mcp.WithString("object_type", mcp.Description("Object type"), mcp.Required(), mcp.Enum(bulkObjectTypes...)),
			mcp.WithString("operation", mcp.Description("Operation"), mcp.Required(), mcp.Enum(bulkObjectOperations...)),
			withJSONArray("objects", mcp.Description("Array of object IDs; leave out with all: true"), mcp.Items(map[string]any{"type": "integer"})),
			mcp.WithBoolean("all", mcp.Description("Select every object matching filters instead of listing objects")),
			withJSONObject("filters", mcp.Description("Object with filters for all: true: name (contains), is_root (tags), path (storage paths); unknown names and unusable values are rejected"), filtersSchema(objectFilterParams["tags"], objectFilterParams["storage_paths"])),
			withNumber("owner", mcp.Description("set_permissions: the new owner's user ID")),
			withJSONObject("permissions", mcp.Description("Object with set_permissions grants: {\"view\": {\"users\": [id], \"groups\": [id]}, \"change\": {...}}; give at least one action, each with users and/or groups")),
			mcp.WithBoolean("merge", mcp.Description("set_permissions: add to the current owner and grants instead of replacing them (default: false)")),
			withClear("owner"),
		),
		handleBulkEditObjects(client),
	)
}

func handleDocumentBulkEdit(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body, _, errRes := getDocumentSelection(request)
		if errRes != nil {
			return errRes, nil
		}

		method, errRes := getRequiredString(request, "method")
		if errRes != nil {
			return errRes, nil
		}
		if tool, moved := bulkEditMovedMethods[method]; moved {
			return errResult(fmt.Sprintf("document_bulk_edit no longer runs %s; use %s", method, tool)), nil
		}
		if !slices.Contains(bulkEditMethods, method) {
			return errResult("method must be one of: " + strings.Join(bulkEditMethods, ", ")), nil
		}
		body["method"] = method

		if err := setJSONField(body, request, "parameters"); err != nil {
			return errResult(err.Error()), nil
		}
		if _, ok := body["parameters"]; !ok {
			return errResult("parameters is required"), nil
		}

		wait, timeout, errRes := getWaitOptions(request)
		if errRes != nil {
			return errRes, nil
		}

		path := "/api/documents/bulk_edit/"
		resp, err := client.Post(ctx, path, body)
		// Metadata methods finish inside the bulk task: nothing follows on.
		return respondWithWait(ctx, client, request, wait, timeout, resp, err, "POST", path, nil)
	}
}

func handleDocumentSelectionData(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body, _, errRes := getDocumentSelection(request)
		if errRes != nil {
			return errRes, nil
		}

		path := "/api/documents/selection_data/"
		resp, err := client.Post(ctx, path, body)
		// The endpoint returns every object in the instance, almost all with
		// document_count: 0 — keep only objects present in the selection.
		return doRequestJSON(resp, err, "POST", path, func(v any) any {
			m, ok := v.(map[string]any)
			if !ok {
				return v
			}
			for key, val := range m {
				if !strings.HasPrefix(key, "selected_") {
					continue
				}
				arr, ok := val.([]any)
				if !ok {
					continue
				}
				filtered := make([]any, 0, len(arr))
				for _, item := range arr {
					if obj, ok := item.(map[string]any); ok {
						if count, ok := obj["document_count"].(float64); ok && count == 0 {
							continue
						}
					}
					filtered = append(filtered, item)
				}
				m[key] = filtered
			}
			return v
		})
	}
}

func handleBulkEditObjects(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		objectType, errRes := getRequiredString(request, "object_type")
		if errRes != nil {
			return errRes, nil
		}
		if !slices.Contains(bulkObjectTypes, objectType) {
			return errResult("object_type must be one of: " + strings.Join(bulkObjectTypes, ", ")), nil
		}
		operation, errRes := getRequiredString(request, "operation")
		if errRes != nil {
			return errRes, nil
		}
		if !slices.Contains(bulkObjectOperations, operation) {
			return errResult("operation must be one of: " + strings.Join(bulkObjectOperations, ", ")), nil
		}
		if argIsSet(request.GetArguments()["parameters"]) {
			return errResult("parameters was removed: pass owner, permissions and merge as their own parameters (clear: [\"owner\"] removes the owner)"), nil
		}

		body := map[string]any{"object_type": objectType, "operation": operation}
		if errRes := setObjectSelection(body, request, objectType); errRes != nil {
			return errRes, nil
		}
		if errRes := setObjectPermissions(body, request, operation); errRes != nil {
			return errRes, nil
		}

		// This endpoint mutates metadata objects without going through their
		// resource paths, so Post's path-based invalidation misses their cache.
		if client.cache != nil {
			if prefix := cachePrefix("/api/" + objectType + "/"); prefix != "" {
				client.cache.Invalidate(prefix)
			}
		}

		path := "/api/bulk_edit_objects/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}

// setObjectSelection reads objects, or all: true with filters, into body.
func setObjectSelection(body map[string]any, request mcp.CallToolRequest, objectType string) *mcp.CallToolResult {
	all, _, errRes := getStrictBool(request, "all")
	if errRes != nil {
		return errRes
	}
	objects, _, err := getJSONInts(request, "objects")
	if err != nil {
		return errResult(err.Error())
	}
	label := strings.TrimSuffix(strings.ReplaceAll(objectType, "_", " "), "s")
	filters, present, errRes := parseSelectionFilters(request, objectFilterParams[objectType], label)
	if errRes != nil {
		return errRes
	}
	if !all {
		if present && len(filters) > 0 {
			return errResult("filters needs all: true; to act on specific objects, list them in objects")
		}
		if len(objects) == 0 {
			return errResult("objects is required: a non-empty array of IDs, or all: true with filters")
		}
		if d, dup := firstDuplicate(objects); dup {
			return errResult(fmt.Sprintf("objects lists %d twice", d))
		}
		body["objects"] = objects
		return nil
	}
	if len(objects) > 0 {
		return errResult("objects can't be combined with all: true")
	}
	if filters == nil {
		filters = map[string]any{}
	}
	body["all"] = true
	body["filters"] = filters
	return nil
}

// setObjectPermissions reads set_permissions' owner, permissions, merge and
// clear into body, as the top-level fields paperless-ngx reads.
func setObjectPermissions(body map[string]any, request mcp.CallToolRequest, operation string) *mcp.CallToolResult {
	args := request.GetArguments()
	if operation == "delete" {
		var given []string
		for _, name := range []string{"owner", "permissions", "clear"} {
			if argIsSet(args[name]) {
				given = append(given, name)
			}
		}
		// merge: false (in any form setOptionalBool reads) is harmless.
		if merge, errRes := optionalBool(request, "merge", false); errRes != nil || merge {
			given = append(given, "merge")
		}
		if len(given) > 0 {
			return errResult("delete takes no " + strings.Join(given, ", ") + "; those are for set_permissions")
		}
		return nil
	}
	if errRes := setOptionalInt(body, args, "owner"); errRes != nil {
		return errRes
	}
	perms, present, err := getJSONArg(request, "permissions")
	if err != nil {
		return errResult(err.Error())
	}
	if m, ok := perms.(map[string]any); ok && len(m) == 0 {
		present = false // {} padding
	}
	if present {
		clean, err := validateObjectPermissions(perms)
		if err != nil {
			return errResult(err.Error())
		}
		body["permissions"] = clean
	}
	if errRes := setOptionalBool(body, request, "merge"); errRes != nil {
		return errRes
	}
	if errRes := applyClear(body, request, []string{"owner"}); errRes != nil {
		return errRes
	}
	owner, hasOwner := body["owner"]
	if hasOwner && owner == nil && body["merge"] == true {
		return errResult("clear: [\"owner\"] doesn't work with merge: true (paperless-ngx keeps the owner); leave merge out")
	}
	if _, hasPerms := body["permissions"]; !hasOwner && !hasPerms {
		return errResult("set_permissions needs owner, permissions or clear: [\"owner\"]")
	}
	return nil
}

// validateObjectPermissions checks a permissions object: view and/or change,
// each with users and/or groups as arrays of ids (an empty array revokes).
func validateObjectPermissions(v any) (map[string]any, error) {
	const shape = `permissions must be {"view": {"users": [id], "groups": [id]}, "change": {...}} with at least one action, each with users and/or groups`
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return nil, fmt.Errorf("%s", shape)
	}
	out := map[string]any{}
	for action, spec := range m {
		if action != "view" && action != "change" {
			return nil, fmt.Errorf("permissions: unknown action %q; %s", action, shape)
		}
		sm, ok := spec.(map[string]any)
		if !ok || len(sm) == 0 {
			return nil, fmt.Errorf("permissions.%s: %s", action, shape)
		}
		clean := map[string]any{}
		for key, ids := range sm {
			if key != "users" && key != "groups" {
				return nil, fmt.Errorf("permissions.%s: unknown key %q; %s", action, key, shape)
			}
			arr, ok := ids.([]any)
			if !ok {
				return nil, fmt.Errorf("permissions.%s.%s must be an array of IDs", action, key)
			}
			list := make([]int, 0, len(arr))
			for _, id := range arr {
				n, whole := wholeNumber(id)
				if _, isNum := id.(float64); !isNum || !whole {
					return nil, fmt.Errorf("permissions.%s.%s must be an array of IDs", action, key)
				}
				list = append(list, n)
			}
			clean[key] = list
		}
		out[action] = clean
	}
	return out, nil
}

// bulkObjectTypes and bulkObjectOperations are bulk_edit_objects' choices.
var (
	bulkObjectTypes      = []string{"tags", "correspondents", "document_types", "storage_paths"}
	bulkObjectOperations = []string{"set_permissions", "delete"}
)

// bulkEditMethods are the document_bulk_edit methods: metadata changes and
// run_workflow. File operations moved to the document action tools.
var bulkEditMethods = []string{
	"set_correspondent", "set_document_type", "set_storage_path", "add_tag", "remove_tag",
	"modify_tags", "modify_custom_fields", "set_permissions", "run_workflow",
}

// bulkEditMovedMethods maps the bulk_edit file methods paperless-ngx still
// accepts (deprecated, until API v9 is dropped) to the tool that replaces them.
var bulkEditMovedMethods = map[string]string{
	"rotate":          "document_rotate",
	"merge":           "document_merge",
	"edit_pdf":        "document_edit_pdf",
	"split":           "document_edit_pdf",
	"delete_pages":    "document_edit_pdf",
	"remove_password": "document_remove_password",
	"reprocess":       "document_reprocess",
	"delete":          "document_bulk_delete",
}

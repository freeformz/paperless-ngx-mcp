package main

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Document actions are paperless-ngx's dedicated endpoints for operations that
// change a document's file (3.0+). Each queues a bulk task and returns
// {result, task_id}; the work is done, and permissions are checked again,
// inside that task.

// documentSourceModes are bulk_edit.SourceModeChoices.
var documentSourceModes = []string{"latest_version", "explicit_selection"}

const documentActionAsync = "The server queues a task and returns its task_id at once, before anything changes; errors such as a missing permission show up in that task. Pass wait: true to wait for it and get the outcome; follow_on then lists the tasks the work continues in."

const documentActionPermissionNote = "A 403 says only \"Insufficient permissions\", and the same check runs again in the task. Superusers skip all checks."

func sourceModeOption() mcp.ToolOption {
	return mcp.WithString("source_mode", mcp.Description("Which file to use: latest_version (default) resolves a root document to its latest version; explicit_selection uses each given id as is. A version id is always used as given."), mcp.Enum(documentSourceModes...))
}

func documentsOption(desc string) mcp.ToolOption {
	return withJSONArray("documents", mcp.Description(desc), mcp.Required(), mcp.Items(map[string]any{"type": "integer"}))
}

func registerDocumentActionTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(mcp.NewTool("document_rotate", append([]mcp.ToolOption{
		mcp.WithDescription("Rotate documents' PDFs; each rotation adds a new version of the document (via a consume_file task). Non-PDF documents are skipped without failing the task. degrees 0 adds a version that changes nothing. Needs change permission on every document and ownership of each (or no owner). " + documentActionPermissionNote + " " + documentActionAsync),
		documentsOption("Array of document IDs"),
		withNumber("degrees", mcp.Description("Clockwise rotation, a multiple of 90 (e.g. 90, 180, 270, -90)"), mcp.Required()),
		sourceModeOption(),
	}, waitToolOptions()...)...), handleDocumentAction(client, "rotate", buildRotateBody))

	srv.AddTool(mcp.NewTool("document_merge", append([]mcp.ToolOption{
		mcp.WithDescription("Merge documents into one new PDF document, pages in the order of documents. Without metadata_document_id the new document gets no metadata; with it, it copies that document's metadata and its title gets \" (merged)\". The caller owns the new document. Non-PDF documents are skipped unless archive_fallback uses their archived PDF. The originals stay unless delete_originals is set. Needs change permission on every document and add-document permission; delete_originals also needs ownership of each document and delete permission. " + documentActionPermissionNote + " " + documentActionAsync),
		documentsOption("Array of at least 2 document IDs, in page order"),
		withNullableNumber("metadata_document_id", mcp.Description("Copy metadata from this document; must be one of documents")),
		mcp.WithBoolean("delete_originals", mcp.Description("Move the source documents to the trash once the merged document is consumed (default: false)")),
		mcp.WithBoolean("archive_fallback", mcp.Description("Use the archived PDF of documents that aren't PDFs (default: false; they are skipped)")),
		sourceModeOption(),
	}, waitToolOptions()...)...), handleDocumentAction(client, "merge", buildMergeBody))

	srv.AddTool(mcp.NewTool("document_edit_pdf", append([]mcp.ToolOption{
		mcp.WithDescription("Rebuild one document's PDF page by page: split it, drop pages, reorder or rotate pages. operations lists the OUTPUT pages in order, each {page: original page number, rotate?: degrees (relative, multiple of 90), doc?: output document index, default 0}. Pages left out are dropped. Without update_document each output becomes a new document (the original stays unless delete_original is set); with update_document the single output becomes a new version of the document. Page numbers are checked against the selected file; after versions, take page_count from document_get, and expect an out-of-range page to fail in the task. The output keeps no PDF Info, XMP or outlines. Examples for a 5-page document: split into pages 1-2, 3 and 4-5: [{\"page\":1},{\"page\":2},{\"page\":3,\"doc\":1},{\"page\":4,\"doc\":2},{\"page\":5,\"doc\":2}]. Delete page 3: [{\"page\":1},{\"page\":2},{\"page\":4},{\"page\":5}] with update_document: true. Rotate only page 2: list every page with {\"page\":2,\"rotate\":90}, with update_document: true. Needs change permission and ownership of the document (or no owner); new documents need add-document permission; delete_original needs delete permission. " + documentActionPermissionNote + " " + documentActionAsync),
		documentsOption("Array with exactly one document ID"),
		withJSONArray("operations", mcp.Description("Array of output pages {page, rotate?, doc?}, see the description"), mcp.Required(), mcp.Items(map[string]any{
			"type":                 "object",
			"properties":           map[string]any{"page": map[string]any{"type": "integer", "minimum": 1}, "rotate": map[string]any{"type": "integer"}, "doc": map[string]any{"type": "integer", "minimum": 0}},
			"required":             []string{"page"},
			"additionalProperties": false,
		})),
		mcp.WithBoolean("update_document", mcp.Description("Store the single output as a new version of the document instead of creating new documents (default: false)")),
		mcp.WithBoolean("delete_original", mcp.Description("Without update_document: move the original to the trash once the outputs are consumed (default: false)")),
		mcp.WithBoolean("include_metadata", mcp.Description("Copy the document's metadata to new documents (default: true)")),
		sourceModeOption(),
	}, waitToolOptions()...)...), handleDocumentAction(client, "edit_pdf", buildEditPDFBody))

	srv.AddTool(mcp.NewTool("document_remove_password", append([]mcp.ToolOption{
		mcp.WithDescription("Remove the password from encrypted PDF documents. Without update_document each result becomes a new document (the original stays unless delete_original is set); with update_document it becomes a new version. Unencrypted PDFs are skipped. Documents are processed one at a time, so a wrong password partway through leaves the earlier ones queued. Needs change permission and ownership of every document (or no owner); new documents need add-document permission; delete_original needs delete permission. " + documentActionPermissionNote + " " + documentActionAsync),
		documentsOption("Array of document IDs"),
		mcp.WithString("password", mcp.Description("The PDF password"), mcp.Required()),
		mcp.WithBoolean("update_document", mcp.Description("Store the result as a new version instead of a new document (default: false)")),
		mcp.WithBoolean("delete_original", mcp.Description("Without update_document: move the original to the trash once the result is consumed (default: false)")),
		mcp.WithBoolean("include_metadata", mcp.Description("Copy the document's metadata to new documents (default: true)")),
		sourceModeOption(),
	}, waitToolOptions()...)...), handleDocumentAction(client, "remove_password", buildRemovePasswordBody))

	srv.AddTool(mcp.NewTool("document_reprocess", append([]mcp.ToolOption{
		mcp.WithDescription("Re-run parsing (OCR and content extraction) for documents; a root document with versions reprocesses its latest version. Consumption workflows don't run. Needs change permission on every document. " + documentActionPermissionNote + " " + documentActionAsync),
		documentsOption("Array of document IDs"),
		mcp.WithBoolean("remote_ocr", mcp.Description("Use the remote OCR engine (default: false)")),
	}, waitToolOptions()...)...), handleDocumentAction(client, "reprocess", buildReprocessBody))

	srv.AddTool(mcp.NewTool("document_bulk_delete", append([]mcp.ToolOption{
		mcp.WithDescription("Move documents, with their versions, to the trash. trash_action restore brings them back until the trash is emptied (after 30 days by default). Needs change permission, ownership (or no owner) and delete permission for every document. " + documentActionPermissionNote + " " + documentActionAsync),
		documentsOption("Array of document IDs"),
	}, waitToolOptions()...)...), handleDocumentAction(client, "delete", buildDeleteBody))
}

// documentActionPaths are the endpoints of the document actions.
var documentActionPaths = map[string]string{
	"rotate":          "/api/documents/rotate/",
	"merge":           "/api/documents/merge/",
	"edit_pdf":        "/api/documents/edit_pdf/",
	"remove_password": "/api/documents/remove_password/",
	"reprocess":       "/api/documents/reprocess/",
	"delete":          "/api/documents/delete/",
}

// handleDocumentAction validates an action's arguments with build, sends the
// request and optionally waits for its task.
func handleDocumentAction(client *Client, op string, build func(mcp.CallToolRequest, []int) (map[string]any, *mcp.CallToolResult)) server.ToolHandlerFunc {
	path := documentActionPaths[op]
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		docs, errRes := getDocumentIDs(request)
		if errRes != nil {
			return errRes, nil
		}
		body, errRes := build(request, docs)
		if errRes != nil {
			return errRes, nil
		}
		body["documents"] = docs
		wait, timeout, errRes := getWaitOptions(request)
		if errRes != nil {
			return errRes, nil
		}
		resp, err := client.Post(ctx, path, body)
		return respondWithWait(ctx, client, request, wait, timeout, resp, err, "POST", path, func(task map[string]any) map[string]any {
			return documentActionFollowOn(op, body, task)
		})
	}
}

// getDocumentIDs reads documents: a non-empty array of distinct ids.
func getDocumentIDs(request mcp.CallToolRequest) ([]int, *mcp.CallToolResult) {
	docs, present, err := getJSONInts(request, "documents")
	if err != nil {
		return nil, errResult(err.Error())
	}
	if !present || len(docs) == 0 {
		return nil, errResult("documents is required: a non-empty array of document IDs")
	}
	seen := map[int]bool{}
	for _, d := range docs {
		if seen[d] {
			return nil, errResult(fmt.Sprintf("documents lists %d twice", d))
		}
		seen[d] = true
	}
	return docs, nil
}

// setOptionalBool copies a boolean argument into body only when it was given,
// so the server's default applies otherwise.
func setOptionalBool(body map[string]any, request mcp.CallToolRequest, name string) {
	if v, ok := request.GetArguments()[name]; ok && v != nil {
		body[name] = request.GetBool(name, false)
	}
}

func setSourceMode(body map[string]any, request mcp.CallToolRequest) *mcp.CallToolResult {
	v := request.GetString("source_mode", "")
	if v == "" {
		return nil
	}
	if !slices.Contains(documentSourceModes, v) {
		return errResult("source_mode must be one of: " + strings.Join(documentSourceModes, ", "))
	}
	body["source_mode"] = v
	return nil
}

// wholeNumber reports whether v is a JSON number with no fractional part.
func wholeNumber(v any) (int, bool) {
	f, ok := v.(float64)
	if !ok || f != math.Trunc(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return int(f), true
}

func buildRotateBody(request mcp.CallToolRequest, _ []int) (map[string]any, *mcp.CallToolResult) {
	raw, ok := request.GetArguments()["degrees"]
	if !ok || raw == nil {
		return nil, errResult("degrees is required")
	}
	deg := request.GetFloat("degrees", math.NaN())
	if deg != math.Trunc(deg) || int(deg)%90 != 0 {
		return nil, errResult("degrees must be a multiple of 90")
	}
	body := map[string]any{"degrees": int(deg)}
	return body, setSourceMode(body, request)
}

func buildMergeBody(request mcp.CallToolRequest, docs []int) (map[string]any, *mcp.CallToolResult) {
	if len(docs) < 2 {
		return nil, errResult("document_merge needs at least 2 documents")
	}
	body := map[string]any{}
	args := request.GetArguments()
	if v, ok := args["metadata_document_id"]; ok {
		if v == nil {
			body["metadata_document_id"] = nil
		} else {
			id := int(request.GetFloat("metadata_document_id", -1))
			if !slices.Contains(docs, id) {
				return nil, errResult("metadata_document_id must be one of documents")
			}
			body["metadata_document_id"] = id
		}
	}
	setOptionalBool(body, request, "delete_originals")
	setOptionalBool(body, request, "archive_fallback")
	return body, setSourceMode(body, request)
}

func buildEditPDFBody(request mcp.CallToolRequest, docs []int) (map[string]any, *mcp.CallToolResult) {
	if len(docs) != 1 {
		return nil, errResult("document_edit_pdf takes exactly one document")
	}
	raw, present, err := getJSONArg(request, "operations")
	if err != nil {
		return nil, errResult(err.Error())
	}
	ops, ok := raw.([]any)
	if !present || !ok || len(ops) == 0 {
		return nil, errResult("operations is required: a non-empty array of {page, rotate?, doc?}")
	}
	body := map[string]any{}
	setOptionalBool(body, request, "update_document")
	setOptionalBool(body, request, "delete_original")
	setOptionalBool(body, request, "include_metadata")
	update := body["update_document"] == true

	out := make([]any, 0, len(ops))
	used := map[int]bool{}
	maxDoc := 0
	for i, item := range ops {
		op, ok := item.(map[string]any)
		if !ok {
			return nil, errResult(fmt.Sprintf("operations[%d] must be an object {page, rotate?, doc?}", i))
		}
		clean := map[string]any{}
		for k, v := range op {
			n, whole := wholeNumber(v)
			switch {
			case k != "page" && k != "rotate" && k != "doc":
				return nil, errResult(fmt.Sprintf("operations[%d]: unknown key %q; use page, rotate and doc", i, k))
			case !whole:
				return nil, errResult(fmt.Sprintf("operations[%d].%s must be a whole number", i, k))
			case k == "page" && n < 1:
				return nil, errResult(fmt.Sprintf("operations[%d].page must be 1 or more", i))
			case k == "rotate" && n%90 != 0:
				return nil, errResult(fmt.Sprintf("operations[%d].rotate must be a multiple of 90", i))
			case k == "doc" && n < 0:
				return nil, errResult(fmt.Sprintf("operations[%d].doc must be 0 or more", i))
			}
			clean[k] = n
		}
		if _, ok := clean["page"]; !ok {
			return nil, errResult(fmt.Sprintf("operations[%d].page is required", i))
		}
		doc, _ := clean["doc"].(int)
		if update && doc != 0 {
			return nil, errResult("with update_document every operation must use doc 0: the result is a single version")
		}
		used[doc] = true
		maxDoc = max(maxDoc, doc)
		out = append(out, clean)
	}
	for d := 0; d <= maxDoc; d++ {
		if !used[d] {
			return nil, errResult(fmt.Sprintf("operations use output document %d but not %d; output indices must run from 0 without gaps", maxDoc, d))
		}
	}
	body["operations"] = out
	return body, setSourceMode(body, request)
}

func buildRemovePasswordBody(request mcp.CallToolRequest, _ []int) (map[string]any, *mcp.CallToolResult) {
	password, errRes := getRequiredString(request, "password")
	if errRes != nil {
		return nil, errRes
	}
	body := map[string]any{"password": password}
	setOptionalBool(body, request, "update_document")
	setOptionalBool(body, request, "delete_original")
	setOptionalBool(body, request, "include_metadata")
	return body, setSourceMode(body, request)
}

func buildReprocessBody(request mcp.CallToolRequest, _ []int) (map[string]any, *mcp.CallToolResult) {
	body := map[string]any{}
	setOptionalBool(body, request, "remote_ocr")
	return body, nil
}

func buildDeleteBody(mcp.CallToolRequest, []int) (map[string]any, *mcp.CallToolResult) {
	return map[string]any{}, nil
}

// documentConsumeActions are the actions whose work continues in consume_file
// tasks scheduled by their bulk task (paperless-ngx bulk_edit.py).
var documentConsumeActions = []string{"rotate", "merge", "edit_pdf", "remove_password"}

// documentActionFollowOn describes the tasks an action's bulk task schedules,
// which the bulk task record doesn't link to. It returns nil for actions that
// complete inside the bulk task (delete).
func documentActionFollowOn(op string, body map[string]any, task map[string]any) map[string]any {
	var types []string
	switch {
	case slices.Contains(documentConsumeActions, op):
		types = append(types, "consume_file")
	case op == "reprocess":
		types = append(types, "reprocess_document")
	default:
		return nil
	}
	switch {
	case op == "merge" && body["delete_originals"] == true,
		(op == "edit_pdf" || op == "remove_password") && body["delete_original"] == true && body["update_document"] != true:
		types = append(types, "bulk_delete")
	}
	created, _ := task["date_created"].(string)
	return map[string]any{
		"task_types": types,
		"note":       "The bulk task may schedule these tasks separately and doesn't link to them: none are scheduled for documents an operation skips (rotate and merge skip non-PDFs, remove_password skips unencrypted PDFs), and bulk_delete is queued only after the consume tasks finish. Find them with task_list, filtering task_type and date_created_after; other tasks created since may match too, so check input_data (filename, overrides). Wait on each with task_wait.",
		"task_list":  map[string]any{"task_type": types, "date_created_after": created},
	}
}

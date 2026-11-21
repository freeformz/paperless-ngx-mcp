package main

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
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

// documentSelectionNote explains all: true selections, shared by every tool
// that takes one.
const documentSelectionNote = "Select documents either by documents (explicit IDs) or with all: true, which acts on every root document you can view that matches filters (the same filter names as document_list), minus excluded_documents. The selection is resolved when the request arrives; versions are never included. Without filters, all: true means every document. Preview first with document_list and the same filters (page_size: 1 gives count). Filters that widen by design: more_like_id (matches most documents sharing a word), tags_id_none, is_in_inbox: false, NOT in custom_field_query, and query's full search syntax. Permissions are all-or-nothing: one selected document you can't change (or don't own, where ownership is needed) fails the whole call with a 403."

// documentSelectionOptions declares documents (optional), all, filters and
// excluded_documents for the tools that accept an all: true selection.
func documentSelectionOptions() []mcp.ToolOption {
	return []mcp.ToolOption{
		withJSONArray("documents", mcp.Description("Array of document IDs; leave out with all: true"), mcp.Items(map[string]any{"type": "integer"})),
		mcp.WithBoolean("all", mcp.Description("Select every matching root document instead of listing documents (see filters)")),
		withJSONObject("filters", mcp.Description("Object with document_list filters for all: true, e.g. {\"tags_id_all\": [3], \"created_before\": \"2020-01-01\"}; unknown names and unusable values are rejected"), filtersSchema(documentFilterParams)),
		withJSONArray("excluded_documents", mcp.Description("Array of document IDs to leave out of an all: true selection"), mcp.Items(map[string]any{"type": "integer"})),
	}
}

// getStrictBool reads an optional boolean: true, false, or the strings "true"
// and "false". Anything else is an error, not a guess.
func getStrictBool(request mcp.CallToolRequest, name string) (value, present bool, errRes *mcp.CallToolResult) {
	if !argIsSet(request.GetArguments()[name]) {
		return false, false, nil
	}
	switch v := request.GetArguments()[name].(type) {
	case bool:
		return v, true, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true":
			return true, true, nil
		case "false":
			return false, true, nil
		}
	}
	return false, true, errResult(name + " must be true or false")
}

// getDocumentSelection reads a document selection: explicit documents, or
// all: true with filters and excluded_documents. It returns the body fields
// to send and the explicit ids (nil with all).
func getDocumentSelection(request mcp.CallToolRequest) (map[string]any, []int, *mcp.CallToolResult) {
	all, _, errRes := getStrictBool(request, "all")
	if errRes != nil {
		return nil, nil, errRes
	}
	if !all {
		// An empty filters object (as some clients pad unused params) is not a selection.
		if raw, present, errRes := parseSelectionFilters(request, documentFilterParams, "document"); errRes != nil || (present && len(raw) > 0) {
			if errRes == nil {
				errRes = errResult("filters needs all: true; to act on specific documents, list them in documents")
			}
			return nil, nil, errRes
		}
		if excluded, _, err := getJSONInts(request, "excluded_documents"); err != nil || len(excluded) > 0 {
			if err != nil {
				return nil, nil, errResult(err.Error())
			}
			return nil, nil, errResult("excluded_documents needs all: true")
		}
		docs, errRes := getDocumentIDs(request)
		if errRes != nil {
			return nil, nil, errResult(resultTextOf(errRes) + ", or all: true with filters")
		}
		return map[string]any{"documents": docs}, docs, nil
	}
	if docs, _, err := getJSONInts(request, "documents"); err != nil || len(docs) > 0 {
		if err != nil {
			return nil, nil, errResult(err.Error())
		}
		return nil, nil, errResult("documents can't be combined with all: true; use excluded_documents to leave some out, or list documents without all")
	}
	filters, _, errRes := parseSelectionFilters(request, documentFilterParams, "document")
	if errRes != nil {
		return nil, nil, errRes
	}
	if filters == nil {
		filters = map[string]any{}
	}
	sel := map[string]any{"all": true, "filters": filters}
	excluded, _, err := getJSONInts(request, "excluded_documents")
	if err != nil {
		return nil, nil, errResult(err.Error())
	}
	if len(excluded) > 0 {
		if d, dup := firstDuplicate(excluded); dup {
			return nil, nil, errResult(fmt.Sprintf("excluded_documents lists %d twice", d))
		}
		sel["excluded_documents"] = excluded
	}
	return sel, nil, nil
}

// rejectSelectionArgs refuses all / filters / excluded_documents on a tool
// that only takes explicit documents, instead of silently ignoring them.
func rejectSelectionArgs(request mcp.CallToolRequest, tool string) *mcp.CallToolResult {
	args := request.GetArguments()
	for _, name := range []string{"all", "filters", "excluded_documents"} {
		v := args[name]
		if v == false || v == "false" || !argIsSet(v) {
			continue
		}
		return errResult(fmt.Sprintf("%s doesn't support %s: paperless-ngx only takes explicit documents here", tool, name))
	}
	return nil
}

// firstDuplicate returns the first id that appears twice.
func firstDuplicate(ids []int) (int, bool) {
	seen := map[int]bool{}
	for _, d := range ids {
		if seen[d] {
			return d, true
		}
		seen[d] = true
	}
	return 0, false
}

// resultTextOf returns the text of a single-text error result.
func resultTextOf(r *mcp.CallToolResult) string {
	if len(r.Content) > 0 {
		if tc, ok := r.Content[0].(mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

func registerDocumentActionTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(mcp.NewTool("document_rotate", append(append([]mcp.ToolOption{
		mcp.WithDescription("Rotate documents' PDFs; each rotation adds a new version of the document (via a consume_file task). Non-PDF documents are skipped without failing the task. Ids that share a root document are rotated once. degrees 0 adds a version that changes nothing. Needs change permission on every document and ownership of each (or no owner). " + documentActionPermissionNote + " " + documentSelectionNote + " " + documentActionAsync),
	}, documentSelectionOptions()...), append([]mcp.ToolOption{
		withNumber("degrees", mcp.Description("Clockwise rotation, a multiple of 90 (e.g. 90, 180, 270, -90)"), mcp.Required(), func(schema map[string]any) { schema["multipleOf"] = 90 }),
		sourceModeOption(),
	}, waitToolOptions()...)...)...), handleDocumentAction(client, "rotate", buildRotateBody))

	srv.AddTool(mcp.NewTool("document_merge", append([]mcp.ToolOption{
		mcp.WithDescription("Merge documents into one new PDF document, pages in the order of documents. Without metadata_document_id the new document gets no metadata; with it, it copies that document's metadata and its title gets \" (merged)\". The caller owns the new document. Non-PDF documents are skipped unless archive_fallback uses their archived PDF. The originals stay unless delete_originals is set. Needs change permission on every document and the global add-document permission; delete_originals also needs ownership of each document and the global delete-document permission. " + documentActionPermissionNote + " " + documentActionAsync),
		documentsOption("Array of at least 2 document IDs, in page order"),
		withNumber("metadata_document_id", mcp.Description("Copy metadata from this document; must be one of documents")),
		mcp.WithBoolean("delete_originals", mcp.Description("Move the source documents to the trash once the merged document has been consumed successfully (default: false)")),
		mcp.WithBoolean("archive_fallback", mcp.Description("Use the archived PDF of documents that aren't PDFs (default: false; they are skipped)")),
		sourceModeOption(),
	}, waitToolOptions()...)...), handleDocumentAction(client, "merge", buildMergeBody))

	srv.AddTool(mcp.NewTool("document_edit_pdf", append([]mcp.ToolOption{
		mcp.WithDescription("Rebuild one document's PDF page by page: split it, drop pages, reorder or rotate pages. operations lists the OUTPUT pages in order, each {page: original page number, rotate?: degrees (relative, multiple of 90), doc?: output document index, default 0}. Pages left out are dropped. Without update_document each output becomes a new document (the original stays unless delete_original is set); with update_document the single output becomes a new version of the document. The server checks page numbers against the stored page count of the id you pass (for a root document, its original file; document_get's page_count is the latest version's), and the task checks them again against the file it actually uses. To edit the latest version of a root that has versions, pass that version's id (from document_get's versions). The caller owns every output. The output keeps no PDF Info, XMP or outlines. Examples for a 5-page document: split into pages 1-2, 3 and 4-5: [{\"page\":1},{\"page\":2},{\"page\":3,\"doc\":1},{\"page\":4,\"doc\":2},{\"page\":5,\"doc\":2}]. Delete page 3: [{\"page\":1},{\"page\":2},{\"page\":4},{\"page\":5}] with update_document: true. Rotate only page 2: list every page with {\"page\":2,\"rotate\":90}, with update_document: true. Needs change permission and ownership of the document (or no owner); new documents need the global add-document permission; delete_original needs the global delete-document permission. " + documentActionPermissionNote + " " + documentActionAsync),
		documentsOption("Array with exactly one document ID"),
		withJSONArray("operations", mcp.Description("Array of output pages {page, rotate?, doc?}, see the description"), mcp.Required(), mcp.Items(map[string]any{
			"type":                 "object",
			"properties":           map[string]any{"page": map[string]any{"type": "integer", "minimum": 1}, "rotate": map[string]any{"type": "integer", "multipleOf": 90}, "doc": map[string]any{"type": "integer", "minimum": 0}},
			"required":             []string{"page"},
			"additionalProperties": false,
		})),
		mcp.WithBoolean("update_document", mcp.Description("Store the single output as a new version of the document instead of creating new documents (default: false)")),
		mcp.WithBoolean("delete_original", mcp.Description("Without update_document: move the original to the trash once the outputs have been consumed successfully (default: false)")),
		mcp.WithBoolean("include_metadata", mcp.Description("Copy the document's metadata (title, tags, correspondent and so on) to the outputs, new documents or new version (default: true)")),
		sourceModeOption(),
	}, waitToolOptions()...)...), handleDocumentAction(client, "edit_pdf", buildEditPDFBody))

	srv.AddTool(mcp.NewTool("document_remove_password", append([]mcp.ToolOption{
		mcp.WithDescription("Remove the password from encrypted PDF documents. Without update_document each result becomes a new document (the original stays unless delete_original is set); with update_document it becomes a new version. Unencrypted PDFs are skipped. Documents are processed one at a time, so a wrong password partway through leaves the earlier ones queued. The caller owns every output. Needs change permission and ownership of every document (or no owner); new documents need the global add-document permission; delete_original needs the global delete-document permission. " + documentActionPermissionNote + " " + documentActionAsync),
		documentsOption("Array of document IDs"),
		mcp.WithString("password", mcp.Description("The PDF password"), mcp.Required()),
		mcp.WithBoolean("update_document", mcp.Description("Store the result as a new version instead of a new document (default: false)")),
		mcp.WithBoolean("delete_original", mcp.Description("Without update_document: move the original to the trash once the result has been consumed successfully (default: false)")),
		mcp.WithBoolean("include_metadata", mcp.Description("Copy the document's metadata to the result, new document or new version (default: true)")),
		sourceModeOption(),
	}, waitToolOptions()...)...), handleDocumentAction(client, "remove_password", buildRemovePasswordBody))

	srv.AddTool(mcp.NewTool("document_reprocess", append([]mcp.ToolOption{
		mcp.WithDescription("Re-run parsing (OCR and content extraction) for documents, replacing their content; a root document with versions reprocesses its latest version. Consumption workflows don't run. Needs change permission on every document. " + documentActionPermissionNote + " " + documentSelectionNote + " " + documentActionAsync),
		mcp.WithBoolean("remote_ocr", mcp.Description("Use the remote OCR engine (default: false)")),
	}, append(documentSelectionOptions(), waitToolOptions()...)...)...), handleDocumentAction(client, "reprocess", buildReprocessBody))

	srv.AddTool(mcp.NewTool("document_bulk_delete", append([]mcp.ToolOption{
		mcp.WithDescription("Move documents to the trash; a root document takes its versions with it, while a version id trashes only that version. trash_action restore brings them back until the trash is emptied (after 30 days by default). Needs change permission and ownership (or no owner) for every document, and the global delete-document permission. " + documentActionPermissionNote + " " + documentSelectionNote + " " + documentActionAsync),
	}, append(documentSelectionOptions(), waitToolOptions()...)...)...), handleDocumentAction(client, "delete", buildDeleteBody))
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

// documentActionTools maps each document action to its registered tool name.
var documentActionTools = map[string]string{
	"rotate": "document_rotate", "merge": "document_merge", "edit_pdf": "document_edit_pdf",
	"remove_password": "document_remove_password", "reprocess": "document_reprocess", "delete": "document_bulk_delete",
}

// documentSelectionActions accept an all: true selection upstream; the
// others take explicit documents only.
var documentSelectionActions = []string{"rotate", "reprocess", "delete"}

// handleDocumentAction validates an action's arguments with build, sends the
// request and optionally waits for its task.
func handleDocumentAction(client *Client, op string, build func(mcp.CallToolRequest, []int) (map[string]any, *mcp.CallToolResult)) server.ToolHandlerFunc {
	path := documentActionPaths[op]
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var sel map[string]any
		var docs []int
		var errRes *mcp.CallToolResult
		if slices.Contains(documentSelectionActions, op) {
			sel, docs, errRes = getDocumentSelection(request)
		} else if errRes = rejectSelectionArgs(request, documentActionTools[op]); errRes == nil {
			docs, errRes = getDocumentIDs(request)
			sel = map[string]any{"documents": docs}
		}
		if errRes != nil {
			return errRes, nil
		}
		// With all: true docs is nil; only the selection actions accept that.
		body, errRes := build(request, docs)
		if errRes != nil {
			return errRes, nil
		}
		maps.Copy(body, sel)
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
	if d, dup := firstDuplicate(docs); dup {
		return nil, errResult(fmt.Sprintf("documents lists %d twice", d))
	}
	return docs, nil
}

// setOptionalBool copies a boolean argument into body only when it was given,
// so the server's default applies otherwise. A value that isn't a boolean (or
// a string such as "true") is an error rather than a silent false.
func setOptionalBool(body map[string]any, request mcp.CallToolRequest, name string) *mcp.CallToolResult {
	if !argIsSet(request.GetArguments()[name]) {
		return nil
	}
	switch v := request.GetArguments()[name].(type) {
	case nil:
	case bool:
		body[name] = v
	case float64, int, int64:
		// 1 and 0, as some clients send booleans that way.
		n, whole := wholeNumber(v)
		if !whole || (n != 0 && n != 1) {
			return errResult(name + " must be true or false")
		}
		body[name] = n == 1
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return errResult(name + " must be true or false")
		}
		body[name] = b
	default:
		return errResult(name + " must be true or false")
	}
	return nil
}

// setOptionalBools applies setOptionalBool to each name, stopping at the first error.
func setOptionalBools(body map[string]any, request mcp.CallToolRequest, names ...string) *mcp.CallToolResult {
	for _, n := range names {
		if errRes := setOptionalBool(body, request, n); errRes != nil {
			return errRes
		}
	}
	return nil
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

// maxWholeNumber bounds the integers the action tools accept, so a huge value
// can't overflow on conversion.
const maxWholeNumber = 1 << 31

// wholeNumber reports whether v is a JSON number with no fractional part,
// within ±maxWholeNumber. A numeric string such as "90" is accepted too.
func wholeNumber(v any) (int, bool) {
	return wholeNumberUpTo(v, maxWholeNumber)
}

// wholeNumberError is the error text for a value wholeNumberUpTo rejected:
// it names the limit when the value is a whole number that is only too big.
func wholeNumberError(name string, v any, limit float64) string {
	if n, whole := wholeNumberUpTo(v, math.MaxFloat64); whole {
		if n < 0 {
			return fmt.Sprintf("%s must be at least -%d", name, int64(limit))
		}
		return fmt.Sprintf("%s must be at most %d", name, int64(limit))
	}
	return name + " must be a whole number"
}

// wholeNumberUpTo is wholeNumber with its own bound, for fields whose range
// is wider than maxWholeNumber.
func wholeNumberUpTo(v any, limit float64) (int, bool) {
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case int:
		f = float64(x)
	case int64:
		f = float64(x)
	case string:
		p, err := strconv.ParseFloat(x, 64)
		if err != nil {
			return 0, false
		}
		f = p
	default:
		return 0, false
	}
	if f != math.Trunc(f) || math.Abs(f) > limit {
		return 0, false
	}
	return int(f), true
}

func buildRotateBody(request mcp.CallToolRequest, _ []int) (map[string]any, *mcp.CallToolResult) {
	raw, ok := request.GetArguments()["degrees"]
	if !ok || raw == nil {
		return nil, errResult("degrees is required")
	}
	deg, whole := wholeNumber(raw)
	if !whole || deg%90 != 0 {
		return nil, errResult("degrees must be a multiple of 90")
	}
	body := map[string]any{"degrees": deg}
	return body, setSourceMode(body, request)
}

func buildMergeBody(request mcp.CallToolRequest, docs []int) (map[string]any, *mcp.CallToolResult) {
	if len(docs) < 2 {
		return nil, errResult("document_merge needs at least 2 documents")
	}
	body := map[string]any{}
	args := request.GetArguments()
	if v := args["metadata_document_id"]; argIsSet(v) {
		id, whole := wholeNumber(v)
		if !whole {
			return nil, errResult("metadata_document_id must be a document ID")
		}
		if !slices.Contains(docs, id) {
			return nil, errResult("metadata_document_id must be one of documents")
		}
		body["metadata_document_id"] = id
	}
	if errRes := setOptionalBools(body, request, "delete_originals", "archive_fallback"); errRes != nil {
		return nil, errRes
	}
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
	if errRes := setOptionalBools(body, request, "update_document", "delete_original", "include_metadata"); errRes != nil {
		return nil, errRes
	}
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
			if _, isNum := v.(float64); !isNum {
				whole = false
			}
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
	if errRes := setOptionalBools(body, request, "update_document", "delete_original", "include_metadata"); errRes != nil {
		return nil, errRes
	}
	return body, setSourceMode(body, request)
}

func buildReprocessBody(request mcp.CallToolRequest, _ []int) (map[string]any, *mcp.CallToolResult) {
	body := map[string]any{}
	return body, setOptionalBool(body, request, "remote_ocr")
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
		"note":       "The bulk task may schedule these tasks separately and doesn't link to them: none are scheduled for documents an operation skips (rotate and merge skip non-PDFs, remove_password skips unencrypted PDFs), and bulk_delete is queued only after the consume tasks succeed (if one fails, none appears). Find them with task_list, filtering task_type and date_created_after; other tasks created since may match too, so check input_data (filename, overrides). Wait on each with task_wait.",
		"task_list":  map[string]any{"task_type": types, "date_created_after": created},
	}
}

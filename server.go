package main

import (
	"github.com/mark3labs/mcp-go/server"
)

// NewServer creates and configures the MCP server with all tool registrations.
func NewServer(client *Client, dl *Downloader) *server.MCPServer {
	srv := server.NewMCPServer(
		"paperless-ngx-mcp",
		version,
		server.WithToolCapabilities(true),
		server.WithInstructions(serverInstructions),
	)

	registerDocumentTools(srv, client)
	registerDownloadTools(srv, client, dl)
	registerPageImageTools(srv, client)
	registerTagTools(srv, client)
	registerCorrespondentTools(srv, client)
	registerDocumentTypeTools(srv, client)
	registerStoragePathTools(srv, client)
	registerCustomFieldTools(srv, client)
	registerSearchTools(srv, client)
	registerBulkTools(srv, client)
	registerDocumentActionTools(srv, client)
	registerSavedViewTools(srv, client)
	registerShareLinkTools(srv, client)
	registerUserTools(srv, client)
	registerMailTools(srv, client)
	registerWorkflowTools(srv, client)
	registerSystemTools(srv, client)

	// Reads each tool's schema at call time, so registration order doesn't matter.
	srv.Use(dropNullArgs(srv))

	return srv
}

const serverInstructions = `Paperless-ngx MCP server — manage documents and metadata via AI agents. Requires Paperless-ngx 3.5.0 or newer (REST API v10).

## Tool Categories
- **Documents**: document_list (search/filter), document_get, document_update, document_delete, document_upload, document_download, document_metadata, document_suggestions, document_next_asn, document_share_links, document_history, document_email
- **Document Notes**: document_note_list, document_note_add, document_note_delete
- **Downloads**: document_download (fetch files, inline or disk), cleanup_downloads (remove downloaded files), document_page_image (render a page as a viewable image)
- **Bulk Operations**: document_bulk_edit (metadata), document_selection_data, bulk_edit_objects
- **Editing Documents**: document_rotate, document_merge, document_edit_pdf, document_remove_password, document_reprocess, document_bulk_delete
- **Tags**: tag_list, tag_get, tag_create, tag_update, tag_delete
- **Correspondents**: correspondent_list, correspondent_get, correspondent_create, correspondent_update, correspondent_delete
- **Document Types**: document_type_list, document_type_get, document_type_create, document_type_update, document_type_delete
- **Storage Paths**: storage_path_list, storage_path_get, storage_path_create, storage_path_update, storage_path_delete, storage_path_test
- **Custom Fields**: custom_field_list, custom_field_get, custom_field_create, custom_field_update, custom_field_delete
- **Search**: search_autocomplete, search_global
- **Statistics**: statistics
- **Saved Views**: saved_view_list, saved_view_get, saved_view_create, saved_view_update, saved_view_delete, saved_view_rule_types
- **Share Links**: share_link_list, share_link_get, share_link_create, share_link_delete (public URL: {PAPERLESS_URL}/share/{slug}; links can't be edited, so delete and recreate to change one)
- **Users & Groups**: user_list, user_get, user_create, user_update, user_delete, group_list, group_get, group_create, group_update, group_delete, profile_get, profile_update
- **Mail**: mail_account_list, mail_account_get, mail_account_create, mail_account_update, mail_account_delete, mail_account_test, mail_account_process, mail_rule_list, mail_rule_get, mail_rule_create, mail_rule_update, mail_rule_delete, processed_mail_list, processed_mail_get, processed_mail_bulk_delete
- **Workflows**: workflow_list, workflow_get, workflow_create, workflow_update, workflow_delete, workflow_run, workflow_trigger_list, workflow_trigger_get, workflow_trigger_update, workflow_trigger_delete, workflow_action_list, workflow_action_get, workflow_action_update, workflow_action_delete
- **Tasks**: task_list, task_get, task_wait, task_acknowledge, task_run, task_stats
- **System**: system_status, remote_version, ui_settings_get, config_list, config_get, config_update, log_list, log_get, trash_list, trash_action

## Pagination
All list endpoints support page (default: 1) and page_size (default: 25) parameters. Responses include count, next, previous, and results fields.

## Response Shaping
Responses are trimmed to stay compact. Document content in document_list results is truncated to a snippet (default 500 bytes, content_truncated: true marks truncated entries) — adjust with content_snippet_bytes (0 disables), pass full_content: true, or use document_get for full text. Notes in document_list results are replaced with a notes_count field (include_notes: true re-includes them; document_note_list and document_get return full notes). log_get returns only the last 100 lines by default (tail parameter). document_selection_data omits objects with zero matching documents. user_list omits inherited_permissions unless include_permissions: true. task_list trims result_data.traceback to its last 500 bytes (include_traceback: true keeps it; task_get returns it in full).

## Document Search
document_list searches three ways: text (words in title or content), title_search (words in titles) and query (advanced syntax below). text and title_search split on spaces only (punctuation is searchable), match each word inside words (art finds party; with several words, numbers match from a word start), drop words over 128 bytes, and don't search correspondents, tags or notes. Use one of them, or query together with text or title_search as an extra filter (score and highlights then come from text or title_search alone); more_like_id (similar documents; score 0, no highlights, a 403 for an invalid or hidden id) combines with none. content and title are slow substring scans; prefer text. Each search text is at most 4096 characters. Results carry __search_hit__ (score, highlights, note_highlights, rank); ordering: -score is best first (the default). If the server enables fuzzy search, query (not text or title_search) also matches near-misses; when sorted by relevance, hits below its score threshold are dropped.

query syntax: AND, OR and NOT in capitals; words without an operator must all match; "quotes" match a phrase; a leading - does NOT exclude (use NOT). Field prefixes apply to the next word: title:, content:, correspondent:, document_type: (type:), storage_path: (path:), tag: (tag:a,b means both), original_filename:, asn:, page_count:, num_notes: (numbers take ranges like asn:[50 to 150]), checksum:, created:, added:, modified:. Plain words don't search notes, custom fields or barcodes: use notes.note:, notes.user:, custom_fields.value:, custom_fields.name:, barcodes.value:, barcodes.format: (notes:, custom_fields: and barcodes: are shorthands). The old note: and custom_field: prefixes, like any unknown field, are searched as plain text. Wildcards * and ?; [..] only together with * or ?; with a stemmer a long prefix can miss (use univers*, not universit*). Dates: today, yesterday, previous week, this month, previous month, previous quarter, this year, previous year, 2005, 2005-03, 2005-03-04, or a range such as created:[2005 to 2009] or added:[-1 week to now]; relative times (-1 week, now) work only as range bounds, quoted bounds need single quotes, and "this week", "3 days ago" or a bare weekday aren't supported. A date or number that can't be parsed is an error naming it.

Duplicates: documents with the same file can exist (uploads aren't refused by default); has_duplicates: true lists them, and document_get's duplicate_documents names the others. search_global is only a quick lookup across object types: at most 3 results per type, no pagination, a 3 to 4096 character query, and documents with only id, title and created.

## Tag Filtering
- tags_id_all: document must have ALL specified tags (comma-separated IDs)
- tags_id_none: document must have NONE of the specified tags
- tags_id_in: document must have ANY of the specified tags

## Custom Field Queries
Use custom_field_query parameter with a JSON filter expression on document_list. Date values may be relative (today, in 30 days, 1 month ago); a query is at most 10 levels deep with 20 conditions.

## Metadata Workflow
1. Get document details with document_get
2. Review current metadata fields
3. Update specific fields with document_update (only changed fields need to be sent)
4. To clear a field, name it in clear (e.g., clear: ["correspondent"] removes the correspondent); a null value is ignored

## Bulk Operations
Use document_bulk_edit for metadata changes across multiple documents. Methods: set_correspondent, set_document_type, set_storage_path, add_tag, remove_tag, modify_tags, modify_custom_fields, set_permissions; its description lists each method's parameters. Run a manual workflow with workflow_run. bulk_edit_objects sets permissions on, or deletes, tags, correspondents, document types and storage paths (owner, permissions, merge and clear are its own parameters).

## Selecting Documents by Filter
document_bulk_edit, document_selection_data, document_rotate, document_reprocess, document_bulk_delete and workflow_run take documents (explicit IDs) or all: true with filters (document_list's filter names, e.g. {"tags_id_all": [3], "created_before": "2020-01-01"}) and excluded_documents. all: true acts on every root document you can view that matches; without filters, every document. Unknown filter names and unusable values are rejected, because paperless-ngx would silently ignore them and select everything. Preview with document_list and the same filters (page_size: 1 gives count). One selected document you can't change fails the whole call with a 403. bulk_edit_objects takes all: true with name (all types), is_root (tags) or path (storage paths) filters; it acts on the objects you can change or delete, and for tags also on child tags. trash_action empty needs documents, or all: true to permanently empty the whole trash.

## Editing Documents
File operations have their own tools: document_rotate (adds a version), document_merge (a new document from several), document_edit_pdf (split, drop, reorder or rotate pages of one document: operations lists the output pages, and pages left out are dropped), document_remove_password, document_reprocess (re-run OCR) and document_bulk_delete (to the trash; trash_action restore undoes it). Each queues a task, like document_bulk_edit; use wait. edit_pdf and remove_password create new documents unless update_document is set, which adds a version instead. Every one needs change permission on each document; rotate, edit_pdf, remove_password, delete and merge with delete_originals also need ownership (or no owner); creating documents needs add permission and deleting needs delete permission. A 403 says only "Insufficient permissions".

## Document Downloads
Use document_download to fetch document files. By default, files are saved to local temp storage and file paths are returned — use cleanup_downloads to remove when done. Set content=true to return file content inline instead (no cleanup needed): image files come back as viewable MCP image content, other types as base64 JSON. Specify variant: archived (default, OCR'd PDF/A), original (as uploaded), or thumbnail. If the server is configured with PAPERLESS_MCP_DOWNLOAD_DIR, disk downloads go there (optionally into a dest_dir subdirectory) with their real filenames, and cleanup_downloads leaves them alone.

## Saved Views
A saved view is a named document filter: filter_rules is a list of {rule_type, value} combined with AND, and multi-value rule types take one rule per value. Call saved_view_rule_types for the codes, value formats and matching document_list filters instead of guessing. filter_rules is required on create ([] matches every document) and replaces all rules on update. Dashboard and sidebar visibility is a per-user web UI setting (ui_settings_get, settings.saved_views); these tools don't change it.

## Structured Parameters
Array and object parameters accept a native JSON value or the same value as a JSON-encoded string. A top-level null means "not given" in every tool (some clients send null for every unused parameter), and so do "" and, outside search text and required text fields, the string "null"; for number and true/false parameters [] and {} are not given too, and so is {} for optional object parameters, but [] is a real empty list where a list replaces values (document_update tags: [] removes every tag); to clear a field, name it in the tool's clear parameter (document_update, tag_update, saved_view_update, bulk_edit_objects, workflow_trigger_update, workflow_action_update, and each trigger and action object in workflow_create and workflow_update).

## Workflows
A workflow has triggers and actions, created and changed through workflow_create and workflow_update (paperless-ngx deletes triggers and actions no workflow uses, so they can't be created on their own). Their fields are typed: unknown or misspelled fields are refused, because paperless-ngx would silently ignore them, and type and choice values take a name or a number.
- Trigger types: 1 consumption (needs filter_filename, filter_path or filter_mailrule, repeated in every update), 2 document_added, 3 document_updated, 4 scheduled (schedule_* fields), 5 manual (run with workflow_run). Sources: 1 consume_folder, 2 api_upload, 3 mail_fetch, 4 web_ui; sources: [] means any source, and new triggers that leave sources out get 1, 2 and 3.
- Action types: 1 assignment (assign_* fields), 2 removal (remove_* fields), 3 email (email: subject, body, to), 4 webhook (webhook: url), 5 password_removal (passwords), 6 move_to_trash, 7 remote_ocr (needs a consumption trigger), 8 apply_ai_suggestions (ai_suggestion_fields; needs a trigger other than consumption). assign_title is a Jinja2 template and may use custom fields, e.g. {{ custom_fields['Invoice number'].value }}.
- workflow_update's triggers and actions replace the workflow's whole set: give id (and type) to keep and update an item, leave id out to add one; items left out are deleted. Because type is always given, an existing email, webhook, password_removal or apply_ai_suggestions action must repeat its email, webhook, passwords or ai_suggestion_fields. Each trigger, action and webhook object takes its own clear (match is cleared to ""). Ids must belong to the workflow. workflow_delete leaves its triggers and actions behind; delete them by id.
- workflow_run runs an enabled workflow with a manual trigger on documents or all: true with filters (see Selecting Documents by Filter). With filter_documents (default true) documents not matching the trigger's filters are skipped. It queues a task; use wait.
- workflow_list filters by trigger_type, enabled, id and id_in.

## Tasks
Background work (consuming uploads, mail fetch, bulk edits, scheduled jobs) runs as tasks. status is pending, started, success, failure or revoked. task_get takes either the task UUID that document_upload, document_bulk_edit, the document editing tools and task_run return, or the integer id from task_list; task_acknowledge takes integer ids (or all: true). task_list filters by status, task_type and trigger_source (arrays: any of the values), and trims result_data.traceback to its last 500 bytes unless include_traceback is set. task_stats gives per-type summaries, status counts, or the currently active tasks.
document_upload, document_bulk_edit, the document editing tools and task_run queue a task and return its id at once, before anything has happened; errors such as missing permissions or a wrong PDF password show up only in the task. Pass wait: true (timeout in seconds, default 60, at most 600) to wait for the task and get its outcome, or call task_wait with the id later. A finished wait returns {task, waited_seconds}; a failed or revoked task is a tool error carrying the task, while a problem with waiting itself comes back as wait_error next to the queued response (don't resend it); if time runs out the task comes back with timed_out: true, and task_wait continues. The MCP host may end a long call before the timeout; task_wait picks it up again. An upload's task is the whole consume. document_bulk_edit's metadata methods and document_bulk_delete are done when their task is, while document_rotate, document_merge, document_edit_pdf and document_remove_password run consume_file tasks of their own, document_reprocess runs reprocess_document tasks, and merge with delete_originals, or edit_pdf or remove_password with delete_original (and no update_document), also run bulk_delete once those succeed; follow_on in the wait result says how to find them.

## Viewing Documents
Use document_page_image to actually see a document page — essential when OCR content is missing or garbled (handwriting, bad scans, stamps, diagrams). Render the full page first; if fine detail is illegible, call it again with region=[x0,y0,x1,y1] (fractions of page width/height, 0-1 — NOT pixels; e.g. [0,0,0.5,0.5] is the top-left quarter) to re-render that area at high resolution.

## API Version
All requests use Paperless-ngx REST API version 10 (Paperless-ngx 3.5.0 or newer).`

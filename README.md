# paperless-ngx-mcp

MCP server for [Paperless-ngx](https://docs.paperless-ngx.com/) document management. Enables AI agents (Claude Code, CoWork) to search, browse, and manage documents and metadata in a Paperless-ngx instance.

## Features

- **100+ MCP tools** covering the full Paperless-ngx REST API v10
- **Document management** — list, search, get, update, delete, upload, download, email
- **Metadata CRUD** — tags, correspondents, document types, storage paths, custom fields
- **Full-text search** with tag filtering, date ranges, and custom field queries
- **Bulk operations** — batch edit, reprocess, merge, split, rotate, delete
- **Notes** — add, list, and delete notes on documents
- **Saved views, share links, mail, workflows** — full admin toolset
- **User & group management** — CRUD, profile, TOTP deactivation
- **System tools** — status, tasks, logs, config, trash management
- **In-memory caching** of metadata lists (tags, correspondents, document types, storage paths, custom fields) with automatic invalidation

## Requirements

Paperless-ngx 3.5.0 or newer. The server speaks REST API v10 only. For older Paperless-ngx servers, use a v0.3.x release of this MCP server. The version isn't checked at runtime: a 2.x server rejects v10 requests with 406, and 3.0 to 3.4 accept them but differ in ways that can make tools misbehave.

## Installation

### CoWork / Claude Desktop (MCPB bundle)

Download the `.mcpb` bundle for your platform from the [latest release](https://github.com/freeformz/paperless-ngx-mcp/releases/latest) and install it as an extension. You'll be prompted for your Paperless-ngx URL and API token.

### Claude Code (CLI)

Add to your project's `.mcp.json`:

```json
{
  "mcpServers": {
    "paperless": {
      "command": "/path/to/paperless-ngx-mcp",
      "args": ["mcp"],
      "env": {
        "PAPERLESS_URL": "http://localhost:8000",
        "PAPERLESS_TOKEN": "your-api-token"
      }
    }
  }
}
```

### Docker

```bash
docker run --rm -i \
  -e PAPERLESS_URL=http://localhost:8000 \
  -e PAPERLESS_TOKEN=your-api-token \
  ghcr.io/freeformz/paperless-ngx-mcp:latest mcp
```

### From source

```bash
go install github.com/freeformz/paperless-ngx-mcp@latest
```

## Configuration

| Variable | Required | Description |
|----------|----------|-------------|
| `PAPERLESS_URL` | yes | Base URL of the Paperless-ngx instance |
| `PAPERLESS_TOKEN` | yes | API authentication token |
| `PAPERLESS_MCP_AI_TIMEOUT` | no | Seconds `document_ai_suggestions` and `document_chat` may take, 1 to 3600 (default 300). Other requests keep a 30-second limit. A value outside that range stops the server at startup. |
| `PAPERLESS_MCP_DOWNLOAD_DIR` | no | Directory for document downloads. When set, files are saved here (or in a `dest_dir` subdirectory) with their real filenames and are left alone by `cleanup_downloads`. When unset, a per-instance temp directory (`paperless-ngx-mcp-*` in the system temp dir) is used and deleted with its contents when the server shuts down: on stdin EOF, SIGINT, SIGTERM or SIGHUP, or when the MCP host exits mid-call. A server killed with SIGKILL, or by a second signal during shutdown, can't clean up and leaves that directory behind. |

Generate an API token in Paperless-ngx under **Settings > Administration > Auth Tokens**.

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--download-concurrency` | 5 | Max parallel document downloads (at least 1) |

### Exit Status

| Status | When |
|--------|------|
| 0 | Graceful shutdown: the MCP host closed stdin, or sent a first SIGINT, SIGTERM or SIGHUP. The shutdown doesn't print an error or usage, though a response that can't be written because the host has gone is still logged to stderr. |
| 1 | An error, printed to stderr as `Error: <message>`, such as an unset `PAPERLESS_URL` or `PAPERLESS_TOKEN`. Only flag errors, such as an unknown flag or a `--download-concurrency` below 1, also print usage. |
| 128 + signal number | A second signal ended a stalled shutdown: shells and Docker report 130 for SIGINT, 143 for SIGTERM and 129 for SIGHUP. The temp download directory is left behind. |

## Tools

Array and object parameters accept a native JSON value or the same value as a JSON-encoded string.

A top-level `null` means "not given" in every tool, because some MCP clients fill every unused parameter with a placeholder. The string `"null"` does too, except in search text (`query`, `text`, `title_search`, `term`, the list tools' name/title/content/path filters, task `result`) and required free-text fields such as a note, where it is kept as text; an empty string is likewise "not given", and so are `[]` and `{}` for number and true/false parameters and `{}` for optional object parameters. An empty list that replaces values is real: `document_update` `tags: []` removes every tag. To clear a field, name it in the tool's `clear` array: `document_update` (`correspondent`, `document_type`, `storage_path`, `archive_serial_number`, `owner`, `content`), `tag_update` (`parent`, `match`), `correspondent_update`, `document_type_update` and `storage_path_update` (`match`, cleared to `""`), `user_update` and `profile_update` (`first_name`, `last_name`, `email`, cleared to `""`), `saved_view_update` (`page_size`, `display_mode`, `display_fields`), `bulk_edit_objects` (`owner`), `workflow_trigger_update` and `workflow_action_update` (their nullable single-value fields, `match`, `assign_custom_fields_values`), and each trigger, action and webhook object in `workflow_create`/`workflow_update`. Nulls inside those objects are ignored too.

`document_bulk_edit`, `document_selection_data`, `document_rotate`, `document_reprocess`, `document_bulk_delete` and `workflow_run` select documents by `documents` or by `all: true` with `filters` (the same names as `document_list`'s filters) and `excluded_documents`; `bulk_edit_objects` takes `all: true` with its own `filters` (`name`, tags' `is_root`, storage paths' `path`). Unknown filter names and values the server would silently ignore are rejected, since paperless-ngx would otherwise select everything. Preview with `document_list` and the same filters (`page_size: 1` gives `count`).

`document_upload`, `document_version_upload`, `document_bulk_edit`, the document editing tools (including `document_merge_as_versions`), `workflow_run` and `task_run` start background tasks and return at once; pass `wait: true` (optional `timeout`, default 60 s, at most 600 s) to get the task's outcome in the same call, or use `task_wait` later. A failed or revoked task comes back as a tool error.

Responses are shaped to stay compact for MCP clients: document `content` in `document_list` results is truncated to a snippet — default 500 bytes, adjustable per call with `content_snippet_bytes` (0 disables); `full_content: true` opts out and `document_get` returns full text — document `notes` in `document_list` results are replaced with a `notes_count` field (`include_notes: true` keeps them; `document_note_list` returns full notes), `task_list` trims each task's `result_data.traceback` to its last 500 bytes (`include_traceback: true` keeps it; `task_get` returns it in full), `log_get` returns only the last `tail` lines (default 100), `document_selection_data` omits zero-count objects, `user_list` omits `inherited_permissions` unless `include_permissions: true`, and `profile_get` redacts the API auth token.

### Documents
`document_list`, `document_get`, `document_update`, `document_delete`, `document_upload`, `document_download`, `document_metadata`, `document_classifier_suggestions`, `document_next_asn`, `document_share_links`, `document_history`, `document_email`

### Document Notes
`document_note_list`, `document_note_add`, `document_note_delete`

### Document Versions
`document_version_upload`, `document_version_list`, `document_version_label`, `document_version_delete`, `document_root`, `document_merge_as_versions`

### AI
`document_ai_suggestions`, `document_chat`

They need AI on the server (`ui_settings_get` `settings.ai_enabled`); chat also needs the LLM index. AI suggestions apply nothing. `document_chat` returns `{answer, references}` and sends progress notifications while the answer streams. Both can take minutes, bounded by `PAPERLESS_MCP_AI_TIMEOUT`.

### Downloads & Page Images
`document_download` (disk or inline; images come back as viewable MCP image content), `document_page_image` (render a PDF page or region as an image the model can see — for handwriting and bad scans where OCR fails), `cleanup_downloads`

PDF rendering happens in-process via [go-pdfium](https://github.com/klippa-app/go-pdfium)'s WebAssembly runtime (PDFium compiled to wasm, executed by [wazero](https://wazero.io)) — pure Go, no cgo, no external binaries to install.

### Tags
`tag_list`, `tag_get`, `tag_create`, `tag_update`, `tag_delete`

### Correspondents
`correspondent_list`, `correspondent_get`, `correspondent_create`, `correspondent_update`, `correspondent_delete`

### Document Types
`document_type_list`, `document_type_get`, `document_type_create`, `document_type_update`, `document_type_delete`

### Storage Paths
`storage_path_list`, `storage_path_get`, `storage_path_create`, `storage_path_update`, `storage_path_delete`, `storage_path_test`

### Custom Fields
`custom_field_list`, `custom_field_get`, `custom_field_create`, `custom_field_update`, `custom_field_delete`

### Search & Statistics
`search_autocomplete`, `search_global`, `statistics`

### Bulk Operations
`document_bulk_edit` (metadata methods), `document_selection_data`, `bulk_edit_objects`

### Editing Documents
`document_rotate`, `document_merge`, `document_edit_pdf` (split, drop, reorder or rotate pages), `document_remove_password`, `document_reprocess`, `document_bulk_delete`

Since v0.4.0 these file operations are their own tools with typed parameters; `document_bulk_edit` no longer accepts `rotate`, `merge`, `split`, `delete_pages`, `edit_pdf`, `remove_password`, `reprocess` or `delete`, and names the replacement tool if asked.

Other v0.4.0 changes:
- `document_list`'s `include_all_ids` is gone: API v10 lists no longer carry the `all` ID array. Use `all: true` selections instead.
- A `null` no longer clears a field; use `clear`. This also fixes padded nulls that used to become real values (`document_update` `content: null` wiped the OCR text, `user_update` `is_superuser: null` sent `false`, `matching_algorithm: null` sent `0`, list filters such as `task_list` `acknowledged: null` became `false`). An empty string is "not given" too (so `document_update` `content: ""` no longer empties the OCR text; use `clear: ["content"]`), and a number or true/false value that doesn't parse is now an error instead of a silent `0` or `false`. Booleans also accept `1` and `0`, except the `all` selectors, which take only true/false.
- `bulk_edit_objects` takes `owner`, `permissions`, `merge` and `clear` as top-level parameters; the old `parameters` object (which the server silently ignored) is refused. `object_type` and `operation` are checked.
- `trash_action` `empty` without `documents` is an error; pass `all: true` to empty the whole trash. `restore` needs `documents`.
- `document_list` rejects filter values that don't parse (`is_in_inbox: "yes"`, `correspondent_id: "abc"`, a bad date or tag list) instead of sending `false` or `0`.

### Saved Views
`saved_view_list`, `saved_view_get`, `saved_view_create`, `saved_view_update`, `saved_view_delete`, `saved_view_rule_types`

A saved view's `filter_rules` is a list of `{rule_type, value}` rules; `saved_view_rule_types` lists every code with its value format, so agents don't have to guess. Dashboard and sidebar visibility is a per-user setting changed in the web UI (readable with `ui_settings_get`), not something these tools set.

### Share Links
`share_link_list`, `share_link_get`, `share_link_create`, `share_link_delete`

A share link's public URL is `{PAPERLESS_URL}/share/{slug}`. The server generates the slug, and share links can't be edited: to change a link's expiration or file version, delete it and create a new one, which gets a new slug. `share_link_list` filters by `created` and `expiration` date and sorts by `created`, `expiration` or `document__title`. Results include a read-only `document_title`.

### Users & Groups
`user_list`, `user_get`, `user_create`, `user_update`, `user_delete`, `user_deactivate_totp`, `group_list`, `group_get`, `group_create`, `group_update`, `group_delete`, `profile_get`, `profile_update`

### Mail
`mail_account_list`, `mail_account_get`, `mail_account_create`, `mail_account_update`, `mail_account_delete`, `mail_account_test`, `mail_account_process`, `mail_rule_list`, `mail_rule_get`, `mail_rule_create`, `mail_rule_update`, `mail_rule_delete`, `processed_mail_list`, `processed_mail_get`, `processed_mail_bulk_delete`

### Workflows
`workflow_list`, `workflow_get`, `workflow_create`, `workflow_update`, `workflow_delete`, `workflow_run`, `workflow_trigger_list`, `workflow_trigger_get`, `workflow_trigger_update`, `workflow_trigger_delete`, `workflow_action_list`, `workflow_action_get`, `workflow_action_update`, `workflow_action_delete`

Workflow, trigger and action parameters are typed, generated from a [CUE](https://cuelang.org) schema (`schema/workflow.cue`) that also validates every call: paperless-ngx silently ignores unknown workflow fields, so a misspelled field is refused here instead. Trigger and action types, sources and matching algorithms take a name (`manual`, `password_removal`, `web_ui`, `regex`) or a number. `workflow_run` runs an enabled workflow that has a manual trigger on selected documents. `workflow_list` filters by `trigger_type`, `enabled`, `id` and `id_in`.

v0.5.0 changes:
- `workflow_create`, `workflow_update`, `workflow_trigger_update` and `workflow_action_update` take typed parameters; the free-form `body` is gone. Unknown fields are refused, and each type's required fields (email for 3, webhook for 4, non-blank passwords for 5, `ai_suggestion_fields` for 8) are checked before sending.
- `workflow_trigger_create` and `workflow_action_create` are gone: paperless-ngx deletes triggers and actions that no workflow uses on every workflow update, and its action endpoint can't save email or webhook settings. Add them through `workflow_create` or `workflow_update`. `workflow_action_update` can't set types 3, 4, 7 or 8, or email and webhook settings; use `workflow_update`.
- `workflow_update` requires `type` on every nested trigger and action (so existing actions of type 3, 4, 5 or 8 repeat their email, webhook, passwords or `ai_suggestion_fields`), refuses ids that don't belong to the workflow, and treats `triggers: []`/`actions: []` as not given (upstream would delete every trigger or action). `workflow_create` refuses ids in nested items. A trigger's `sources: []` means any source; left out, new triggers get consume folder, API upload and mail fetch.
- To keep the tool list small, only `workflow_create`'s schema lists the trigger and action fields; `workflow_update`'s items take the same fields plus `id`, and are validated the same way. A client that hides `workflow_create` leaves agents without that field list.
- `document_bulk_edit` no longer runs `run_workflow`; use `workflow_run`.
- The binary is larger, since it embeds the CUE evaluator.

v0.5.1 changes:
- `document_list` and the selection `filters` gain `text` (indexed search of title and content), `title_search` (titles) and `has_duplicates`. paperless-ngx's rule on combining them is checked before sending: one of `text`, `title_search`, `query` and `more_like_id`, or `query` together with `text` or `title_search`. The server instructions describe the 3.x `query` syntax (`NOT` rather than a leading `-`; `notes.note:`, `custom_fields.value:` and `barcodes:` prefixes; date ranges such as `added:[-1 week to now]`).
- Newly refused: `text`/`title_search` words over 128 bytes (upstream drops them silently), any search text over 4096 characters, `ordering` by `score` without a search or beside other fields, and any `ordering` with `more_like_id` (upstream ignores these).

v0.6.0 changes:
- The tag, correspondent, document type and storage path create and update tools take a `matching_algorithm` name as well as a number: `none`, `any`, `all`, `literal`, `regex`, `fuzzy` or `auto` (and the web UI's labels, such as `regular expression`), the same as the workflow tools plus `auto`.
- `tag_create`, `document_type_create` and `storage_path_create` now default to `auto`, like `correspondent_create` and the web UI; before, paperless-ngx's API default (`any`) applied. With `any` and an empty `match` a new object matched nothing; with `auto`, once the classifier next retrains on the documents that have it, it is assigned to newly consumed documents it predicts (a storage path then files them there). Inbox tags are never learned. Pass `matching_algorithm: "none"` for no automatic matching.

v0.7.0 changes:
- Document versions (paperless-ngx 3): `document_version_upload` (add a file as the new latest version, with `wait`), `document_version_list`, `document_version_label`, `document_version_delete` (to the trash), `document_root` (find a version's root) and `document_merge_as_versions` (make documents versions of another; can't be undone). Thanks to MarcelAnderson, whose #51 started this.
- `document_get`, `document_update` (content only), `document_metadata`, `document_download` (one id) and `document_page_image` take `version` to read or edit one version instead of the latest.
- The note tools map a version id to its root document, where paperless-ngx keeps a document's notes (a second text block says so). Notes already stored on a version are still listed and can be deleted.

v0.8.0 changes:
- `document_upload` takes `custom_fields`: an array of custom field ids (each added with no value) or an object mapping field id to value.
- `document_email` takes `use_archive_version` (default true); false sends the uploaded file instead of the archived PDF.
- `search_global` takes `db_only` to match documents by title in the database instead of the search index.
- `log_get` asks the server for only the last `tail` lines instead of fetching the whole log.
- Breaking: `log_get` returns `{lines}` without `total_lines`, and refuses a negative `tail` (before, it became 100; 0 still means the default). `share_link_create`'s `file_version_archive` is now `use_archive_version`; the old name is refused.

v0.9.0 changes:
- AI tools (paperless-ngx 3, with AI on): `document_ai_suggestions` (LLM suggestions for a document; nothing is applied) and `document_chat` (ask across your documents or about one; returns `{answer, references}`, with progress notifications while it streams).
- `PAPERLESS_MCP_AI_TIMEOUT` (default 300 seconds, also in the MCPB settings) bounds the AI calls; other requests keep their 30-second limit.
- Breaking: `document_suggestions` is renamed `document_classifier_suggestions`, since it is the classifier, not the LLM.

v0.9.1 changes:
- Tool descriptions and server instructions corrected for paperless-ngx 3.5: pagination (only tools with `page`/`page_size` are paginated), what `config_*` returns (database overrides, masked API keys, env fallback), where `ui_settings_get` shows effective feature state, `system_status` and `statistics` permissions and fields, and hierarchical tags (ancestors added, descendants removed and deleted).
- `document_history` adds a hint when the audit log is off, and `document_email` when paperless-ngx reports a send failure. The AI-off hint now says that a database `ai_enabled` value beats `PAPERLESS_AI_ENABLED`.

v0.9.2 changes:
- `document_update`'s `custom_fields` replaces the whole set; the descriptions and instructions now point to `document_bulk_edit` `modify_custom_fields` for changing a few fields.

Next release:
- `notes_count` in `document_list` results is now always present, 0 when a document has no notes.
- `document_get` takes `include_permissions: true` to return the permissions object (view and change users and groups) in place of `user_can_change` and `is_shared_by_requester`.
- `clear` can now empty `match` in `tag_update`, `correspondent_update`, `document_type_update` and `storage_path_update`, and `first_name`, `last_name` and `email` in `user_update` and `profile_update`.
- `user_create` and `user_update` take `user_permissions` (bare permission codenames). `user_create`'s `password` is optional, as it is upstream; a padded `"null"` password is now left out instead of sent.
- `mail_account_update` takes `character_set`.
- Fixes: `group_create` without `permissions` no longer gets a 400; `mail_rule_create` refuses `name` or `account` inside `body` (they overrode the parameters); `search_autocomplete` `limit: 0` is "not given" instead of a 400; a metadata list fetched while a write runs is no longer cached stale; `document_page_image` explains a file it can't render (no archived PDF) and points to the thumbnail.

### System
`system_status`, `remote_version`, `ui_settings_get`, `config_list`, `config_get`, `config_update`, `task_list`, `task_get`, `task_wait`, `task_acknowledge`, `task_run`, `task_stats`, `log_list`, `log_get`, `trash_list`, `trash_action`

## Development

```bash
make build          # build binary
make test           # run tests
make test-coverage  # tests with coverage report
make lint           # vet + staticcheck + test
```

## License

MIT

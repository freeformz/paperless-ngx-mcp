# Product Requirements Document: paperless-ngx-mcp

## Overview

paperless-ngx-mcp is an MCP (Model Context Protocol) server that exposes the Paperless-ngx REST API as MCP tools. It enables AI agents (Claude Code, CoWork teams) to search, browse, and manage documents and metadata in a Paperless-ngx instance. The server is a stateless pass-through — it has no local database and delegates all storage to the Paperless-ngx server.

## Problem Statement

Paperless-ngx is a powerful document management system, but interacting with it programmatically requires direct REST API calls. AI agents working in Claude Code or CoWork cannot natively browse, search, or update documents in Paperless-ngx. Common workflows — like finding documents missing a tag, reviewing document metadata, or bulk-updating correspondents — require manual effort or custom scripts.

## Goals

1. **Full API coverage** — Expose all Paperless-ngx REST API endpoints as MCP tools
2. **Document-centric prioritization** — Document, tag, correspondent, document type, and custom field operations are first-class
3. **Metadata management** — Enable agents to read and update document metadata (title, tags, correspondent, document type, created date, custom fields, notes, etc.)
4. **Search and filtering** — Full-text search, tag-based filtering, custom field queries, and document listing with rich filter support
5. **Minimal local state** — Pass-through to the Paperless-ngx server with in-memory caching of metadata lists (tags, correspondents, document types); no local database
6. **CoWork compatible** — Works as an MCPB bundle for Claude Code and CoWork agent teams
7. **Same toolchain as tasks-mcp** — Go, mcp-go, Cobra, GoReleaser, same CI/CD patterns

## Non-Goals

- Local document storage or persistent caching (in-memory metadata caching is in scope)
- Document file modification (PDF editing, image manipulation, re-OCR, etc.) — the `content` text field (extracted OCR text) is editable via `document_update`
- Permission management (`set_permissions`) — intentionally excluded for now
- Paperless-ngx server administration (installation, upgrades, backups)
- Real-time event streaming (WebSocket status endpoint)
- OAuth flow handling (tokens are provided via configuration)

## Target Users

- AI coding agents (Claude Code, CoWork teams) that need to interact with Paperless-ngx
- Developers who want to automate document management workflows via MCP
- CoWork agent teams performing bulk document classification, tagging, or metadata cleanup

## Architecture

### System Design

```
┌─────────────────┐     stdio      ┌──────────────────┐     HTTPS      ┌──────────────────┐
│  Claude Code    │◄──────────────►│  paperless-ngx-  │◄─────────────►│  Paperless-ngx   │
│  (AI Agent)     │    MCP JSON    │  mcp (Go binary) │   REST API     │  Server          │
└─────────────────┘                └──────────────────┘                └──────────────────┘

┌─────────────────┐     stdio      ┌──────────────────┐
│  CoWork Agent   │◄──────────────►│  (same binary,   │
│  (Teammate)     │    MCP JSON    │   separate proc) │
└─────────────────┘                └──────────────────┘
```

The MCP server is a stateless proxy. Each tool call translates to one or more HTTP requests to the Paperless-ngx REST API. Multiple MCP server instances can run concurrently against the same Paperless-ngx server without coordination.

### Components

| Component | Purpose |
|-----------|---------|
| MCP Server | Stdio transport, tool registration, request handling |
| HTTP Client | Paperless-ngx REST API client with auth, versioning, pagination |
| Tool Handlers | Translate MCP tool calls to HTTP requests and format responses |
| CLI | Cobra command structure with `mcp` subcommand |

### Configuration

The server is configured entirely via environment variables:

| Variable | Required | Description |
|----------|----------|-------------|
| `PAPERLESS_URL` | yes | Base URL of the Paperless-ngx instance (e.g., `https://paperless.example.com`) |
| `PAPERLESS_TOKEN` | yes | API authentication token |
| `PAPERLESS_MCP_DOWNLOAD_DIR` | no | Directory for `document_download` disk saves. When set, downloads land here (or in a `dest_dir` subdirectory of it) with their real filenames, are never touched by `cleanup_downloads`, and remain after the server exits. When unset, downloads use a per-instance temp directory (`paperless-ngx-mcp-*` in the system temp dir) that is deleted with its contents on shutdown; see [Shutdown](#shutdown). |

The server always uses Paperless-ngx API version 10 and requires Paperless-ngx 3.5.0 or newer. These are passed via the MCP server configuration (`.mcp.json` or MCPB manifest) as environment variables, which is compatible with CoWork and MCPB bundle format.

### Shutdown

The server shuts down when the MCP host closes stdin or sends SIGINT, SIGTERM or SIGHUP (on Windows: Ctrl-C/Ctrl-Break, or console close, logoff or system shutdown). In-flight tool calls are cancelled, then the per-instance temp download directory is deleted with everything in it before the process exits with status 0 (see [Exit Status](#exit-status)). Signal handling is installed before that directory is created, so a signal during startup can't leak it.

Shutdown doesn't wait for work that can't be interrupted: PDFium renders and image decoding or encoding (`document_page_image`, and image normalization in `document_download` with `content=true`). A cancelled call returns at once; the abandoned work keeps running in the background with its result discarded, or dies with the process at shutdown. PDFium could be interrupted mid-render only by enabling wazero's `WithCloseOnContextDone`, which makes every render about 5x slower. The same applies when a client cancels a single call with `notifications/cancelled`; an abandoned render then keeps the single PDFium renderer busy until it finishes, and the next render waits for it.

A host that exits while a tool call is in flight closes the server's stdout as well as its stdin. SIGPIPE is ignored, so the failed write of the cancelled call's response is logged to stderr instead of killing the process, and the temp directory is still deleted.

Only the first signal starts a graceful shutdown; later signals get the default action. If shutdown stalls, for example because a response write blocks when the host stops reading stdout without closing it, a second SIGINT, SIGTERM or SIGHUP ends the process immediately without deleting the temp directory.

SIGKILL can't be caught. A server killed that way (`kill -9`, the OOM killer, or a host that escalates to SIGKILL when shutdown outlasts its grace period), or by a second signal during shutdown, leaves its temp directory behind, and the server doesn't remove it later. Delete leftover `paperless-ngx-mcp-*` directories from the system temp dir while no server is running. Files in `PAPERLESS_MCP_DOWNLOAD_DIR` are never removed.

### Exit Status

| Status | When |
|--------|------|
| 0 | Graceful shutdown: stdin EOF, or a first SIGINT, SIGTERM or SIGHUP. A requested shutdown isn't an error, so it doesn't print an error or usage. A response that can't be written because the host has gone is still logged (see [Shutdown](#shutdown)). |
| 1 | An error, printed to stderr as `Error: <message>`, such as an unset `PAPERLESS_URL` or `PAPERLESS_TOKEN`, or a temp download directory that can't be created. Only flag errors (an unknown flag, a value that doesn't parse, or a `--download-concurrency` below 1) also print usage: the `mcp` command checks its flags, then sets `SilenceUsage`. |
| 128 + signal number | A second SIGINT, SIGTERM or SIGHUP ended a stalled shutdown. The signal gets its default action, so it kills the process, and shells and Docker report 130, 143 or 129. The temp download directory is left behind. |

### HTTP Client

- **Authentication**: Token-based via `Authorization: Token <token>` header
- **API versioning**: `Accept: application/json; version=10` header on all requests (hardcoded to v10)
- **Minimum server**: Paperless-ngx 3.5.0. Older servers stay on MCP v0.3.x. There is no runtime version check: a 2.x server answers v10 requests with 406, and 3.0 to 3.4 accept them with silent differences (for example, 3.0 and 3.1 ignore parameters added in 3.2.0, and 3.5.0 made the bulk and action endpoints asynchronous).
- **Structured parameters**: every array or object parameter accepts a native JSON value or the same value as a JSON-encoded string; both produce the same request
- **Pagination**: Automatic handling — list tools accept `page` and `page_size` parameters; responses include `count`, `next`, `previous`, and `results`
- **Error handling**: HTTP error responses are translated to MCP error results with status code, detail message, and endpoint context
- **Timeouts**: Configurable per-request timeout with sensible default (30s)
- **Content types**: JSON for most endpoints; multipart/form-data for document uploads
- **Response caching**: In-memory TTL cache for metadata list endpoints (tags, correspondents, document types, storage paths, custom fields). These change infrequently but are queried often by agents doing classification work. Cache is invalidated on create/update/delete operations for the same resource type. No document content or search results are cached.

## MCP Tools

Tools are organized into tiers. Tier 1 (document-centric) tools are implemented first; Tier 2 (admin/system) tools follow.

### Tier 1: Document-Centric

#### Documents

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `document_list` | GET | `/api/documents/` | List/search documents with filtering and full-text search |
| `document_get` | GET | `/api/documents/{id}/` | Get document details |
| `document_update` | PATCH | `/api/documents/{id}/` | Update document metadata |
| `document_delete` | DELETE | `/api/documents/{id}/` | Delete document (soft-delete to trash) |
| `document_upload` | POST | `/api/documents/post_document/` | Upload a new document |
| `document_metadata` | GET | `/api/documents/{id}/metadata/` | Get file metadata (checksums, sizes, MIME) |
| `document_suggestions` | GET | `/api/documents/{id}/suggestions/` | Get AI suggestions for tags, correspondent, etc. |
| `document_next_asn` | GET | `/api/documents/next_asn/` | Get next archive serial number |
| `document_share_links` | GET | `/api/documents/{id}/share_links/` | List share links for a document |
| `document_history` | GET | `/api/documents/{id}/history/` | Get audit trail for a document |
| `document_email` | POST | `/api/documents/email/` | Email one or more documents |
| `document_download` | GET | `/api/documents/{id}/download/` | Download document files to disk or inline (images as viewable MCP image content) |
| `document_page_image` | GET | `/api/documents/{id}/download/` | Render a document page (or region) as a viewable image |
| `cleanup_downloads` | — | — | Clean up downloaded document files |

##### document_list

The primary search and filtering tool. Supports the full range of Paperless-ngx query parameters.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| query | string | no | Full-text search query |
| more_like_id | integer | no | Find documents similar to this document ID |
| correspondent_id | integer | no | Filter by correspondent ID |
| document_type_id | integer | no | Filter by document type ID |
| storage_path_id | integer | no | Filter by storage path ID |
| tags_id_all | string | no | Comma-separated tag IDs — document must have ALL |
| tags_id_none | string | no | Comma-separated tag IDs — document must have NONE |
| tags_id_in | string | no | Comma-separated tag IDs — document must have ANY |
| is_tagged | boolean | no | Filter by whether document has any tags |
| is_in_inbox | boolean | no | Filter by inbox status |
| title | string | no | Filter by title (icontains) |
| content | string | no | Filter by content (icontains) |
| custom_field_query | array or JSON string | no | Custom field filter expression, e.g. `["AND", [[1, "exists", true]]]` |
| created_after | string | no | Filter by created date (gte, YYYY-MM-DD) |
| created_before | string | no | Filter by created date (lte, YYYY-MM-DD) |
| added_after | string | no | Filter by added date (gte, YYYY-MM-DD) |
| added_before | string | no | Filter by added date (lte, YYYY-MM-DD) |
| owner_id | integer | no | Filter by owner |
| ordering | string | no | Sort field (prefix `-` for descending) |
| page | integer | no | Page number (default: 1) |
| page_size | integer | no | Results per page (default: 25, max: 100000) |
| full_content | boolean | no | Include full document content instead of a truncated snippet (default: false) |
| content_snippet_bytes | integer | no | Max bytes of document content per result (default: 500; 0 disables truncation) |
| include_notes | boolean | no | Include full note objects in results (default: false — notes are replaced with `notes_count`) |

**Returns:** Paginated document list with count, next/previous page URLs, and document objects. Each document object includes API URLs for download (`/api/documents/{id}/download/`), preview (`/api/documents/{id}/preview/`), and thumbnail (`/api/documents/{id}/thumb/`) — these are unauthenticated URL paths relative to `PAPERLESS_URL` that require a token to fetch. When `query` is used, includes `__search_hit__` with score, highlights, and rank.

By default each document's `content` (OCR text) is truncated to a snippet (default 500 bytes, adjustable with `content_snippet_bytes`; `content_truncated: true` marks truncated entries) — full OCR content can be huge, making default responses unusable for MCP clients. Use `document_get` for full content of a single document, and `full_content` (or `content_snippet_bytes: 0`) to disable truncation. `count` is the number of matching documents: with `page_size: 1` it previews an `all: true` selection (see Selecting Documents by Filter). Filter values are checked: `null`, `""` and (for non-text filters) `"null"` mean not given, while an id, tag list, boolean or date that doesn't parse is an error rather than a silent `0` or `false`. Each document's `notes` array is likewise replaced with a `notes_count` field (omitted when zero) — notes are unbounded and can dominate response size. Pass `include_notes: true` to keep full note objects, or use `document_note_list`/`document_get` for a single document's notes.

##### document_get

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| id | integer | yes | Document ID |

**Returns:** Full document object including tags, correspondent, document type, storage path, custom fields, notes, permissions, and API URL paths for download, preview, and thumbnail (unauthenticated paths relative to `PAPERLESS_URL`).

##### document_update

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| id | integer | yes | Document ID |
| title | string | no | New title |
| content | string | no | New document content text (OCR text); an empty string is ignored, `clear: ["content"]` empties it |
| created | string | no | New created date (YYYY-MM-DD) |
| correspondent | integer | no | Correspondent ID |
| document_type | integer | no | Document type ID |
| storage_path | integer | no | Storage path ID |
| tags | array or JSON string | no | Array of tag IDs (replaces all tags) |
| archive_serial_number | integer | no | Archive serial number |
| owner | integer | no | Owner user ID |
| custom_fields | array or JSON string | no | Array of custom field assignments |
| clear | array of strings | no | Fields to clear (sent as null): `correspondent`, `document_type`, `storage_path`, `archive_serial_number`, `owner`. A `null` value is ignored (see Null Values) |

**Returns:** Updated document object.

##### document_upload

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| file_path | string | yes | Local path to the file to upload |
| title | string | no | Document title |
| created | string | no | Created date |
| correspondent | integer | no | Correspondent ID |
| document_type | integer | no | Document type ID |
| storage_path | integer | no | Storage path ID |
| tags | array or JSON string | no | Array of tag IDs |
| archive_serial_number | integer | no | Archive serial number |

**Returns:** Task UUID for tracking consumption status. Follow it with `task_get`; a consume that hit a duplicate is reported as status `failure` with `result_data.duplicate_of`.

##### document_download

Downloads one or more document files. By default, files are saved to disk and file paths are returned: to a per-instance temp directory, or to `PAPERLESS_MCP_DOWNLOAD_DIR` when configured (optionally into a `dest_dir` subdirectory of it). Saved files keep the document's real filename (sanitised, de-duplicated with a numeric suffix on collision). Set `content=true` to return file content inline instead: image files (PNG, JPEG, WebP, GIF) come back as MCP image content blocks the model can see, subject to the image size discipline (longest edge ≤ 1568 px, oversized images downscaled and re-encoded as JPEG); other types come back as base64 in the JSON summary. Files are downloaded in parallel with configurable concurrency (default 5, override with `--download-concurrency` CLI flag). Returns mixed results: each document ID gets either a successful result or an error message.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| ids | array or JSON string | yes | Array of document IDs to download |
| variant | string | no | File variant: `archived` (default, OCR'd PDF/A), `original` (as uploaded), or `thumbnail` |
| content | boolean | no | Return file content inline instead of saving to disk (images as viewable image blocks, other types as base64) |
| dest_dir | string | no | Save into this subdirectory of `PAPERLESS_MCP_DOWNLOAD_DIR` (error if that variable is unset, or if the path escapes it). Incompatible with `content=true`. |

**Returns:** JSON object with `results` array. Each result contains `id`, `content_type`, `filename`, and either `path` (disk mode, default), `content` (base64 string, content mode, non-image types), a `note` (content mode, image delivered as an image block), or `error`. Disk mode also includes `download_dir` at the top level. In content mode, image files additionally appear as MCP image content blocks after the JSON summary, and PDF results carry a `note` pointing agents at `document_page_image` for viewing pages as images.

##### document_page_image

Renders one page of a document as an image the model can see. This is the tool for reading documents whose OCR `content` is missing or garbled (handwriting, bad scans, stamps, diagrams). PDFs are rasterized in-process by PDFium compiled to WebAssembly (via `go-pdfium`/wazero — pure Go, no cgo, no external binaries). Documents whose archived file is itself an image are served directly as a single page. The optional `region` re-renders the page at a higher resolution computed from the region size before cropping, so small areas (a phone number, a dollar amount) come back at high effective DPI rather than as an upscaled blur.

Output follows the image size discipline: longest edge capped at 1568 px, grayscale JPEG (quality 80) by default, downscale-and-retry if the encoded image exceeds ~1.5 MB.

A cancelled call returns immediately, even mid-render; see [Shutdown](#shutdown).

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| id | integer | yes | Document ID |
| page | integer | no | Page number, 1-indexed (default: 1) |
| max_width | integer | no | Maximum rendered width in pixels, clamped to 200–2000 (default: 1500) |
| region | array or JSON string | no | Crop region as an array `[x0,y0,x1,y1]`, values as fractions (0–1) of page width/height, not pixels. Pixel-looking values (> 1) are rejected with a hint |
| grayscale | boolean | no | Render grayscale (default: true; ~3x smaller with no legibility loss on scans) |
| format | string | no | Output format: `jpeg` (default) or `png` |

**Returns:** A text block noting document ID, page number, total page count, rendered pixel dimensions, and region (if any), followed by an MCP image content block. Errors are returned for out-of-range pages (naming the actual page count) and non-PDF, non-image documents (naming the content type).

##### cleanup_downloads

Removes downloaded document files. With no arguments, removes all files in the instance's temp download directory. With a file list, removes only those specific files (validated to be inside the temp download directory). Files saved to `PAPERLESS_MCP_DOWNLOAD_DIR` are never tracked or removed by this tool — they are the user's to manage.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| files | array or JSON string | no | Array of file paths to remove. Omit to remove all downloaded files. |

**Returns:** JSON object with `removed` (array of removed paths) and `removed_count`. When removing specific files, may include `failed` and `failed_count` for files that could not be removed.

#### Document Notes

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `document_note_list` | GET | `/api/documents/{id}/notes/` | List notes on a document |
| `document_note_add` | POST | `/api/documents/{id}/notes/` | Add a note to a document |
| `document_note_delete` | DELETE | `/api/documents/{id}/notes/?id={note_id}` | Delete a note |

#### Document Bulk Operations

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `document_bulk_edit` | POST | `/api/documents/bulk_edit/` | Bulk edit document metadata (and run manual workflows) |
| `document_selection_data` | POST | `/api/documents/selection_data/` | Get per-object document counts for a selection (objects with zero matching documents are omitted; no total) |

Both take a selection: `documents`, or `all: true` with `filters` and `excluded_documents` (see Selecting Documents by Filter).

##### document_bulk_edit

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| documents / all / filters / excluded_documents | | yes (one form) | The selection, see Selecting Documents by Filter |
| method | string (enum) | yes | `set_correspondent`, `set_document_type`, `set_storage_path`, `add_tag`, `remove_tag`, `modify_tags`, `modify_custom_fields`, `set_permissions`, `run_workflow` |
| parameters | object or JSON string | yes | Method-specific: `{correspondent}`, `{document_type}`, `{storage_path}` (id or null); `{tag}`; `{add_tags, remove_tags}`; `{add_custom_fields, remove_custom_fields}`; `{set_permissions, owner?, merge?}` (without `merge`, leaving out `owner` clears it); `{workflow, filter_documents?}` |
| wait, timeout | boolean, integer | no | See Waiting for Tasks |

**Returns:** `{result, task_id}`. The file methods paperless-ngx still accepts as deprecated `bulk_edit` methods (`rotate`, `merge`, `split`, `delete_pages`, `edit_pdf`, `remove_password`, `reprocess`, `delete`) are refused with an error naming the replacement tool below; nothing is sent.

#### Editing Documents

Dedicated endpoints for operations that change a document's file (paperless-ngx 3.0+). Each queues a bulk task and returns `{result, task_id}`; the work runs, and permissions are checked again, inside that task. They take `documents` (a non-empty array of distinct ids) plus `wait` / `timeout`, and never send `from_webui`; `document_rotate`, `document_reprocess` and `document_bulk_delete` can take an `all: true` selection instead (see Selecting Documents by Filter), while merge, edit_pdf and remove_password refuse `all`, `filters` and `excluded_documents`. `source_mode` is `latest_version` (default: a root id resolves to its latest version) or `explicit_selection`.

| Tool | Endpoint | Parameters | Notes |
|------|----------|------------|-------|
| `document_rotate` | `/api/documents/rotate/` | `degrees` (multiple of 90), `source_mode` | Adds a version; skips non-PDFs |
| `document_merge` | `/api/documents/merge/` | `metadata_document_id` (one of `documents`), `delete_originals`, `archive_fallback`, `source_mode` | At least 2 documents, in page order; a new document owned by the caller |
| `document_edit_pdf` | `/api/documents/edit_pdf/` | `operations` (`[{page, rotate?, doc?}]`), `update_document`, `delete_original`, `include_metadata`, `source_mode` | One document; operations list the output pages, omitted pages are dropped, `doc` indices must run from 0 without gaps, and with `update_document` all are 0. The server checks pages against the stored page count of the id passed (a root's original file), so to edit a root's latest version pass the version's id |
| `document_remove_password` | `/api/documents/remove_password/` | `password`, `update_document`, `delete_original`, `include_metadata`, `source_mode` | Skips unencrypted PDFs; one document at a time |
| `document_reprocess` | `/api/documents/reprocess/` | `remote_ocr` | A root reprocesses its latest version |
| `document_bulk_delete` | `/api/documents/delete/` | — | To the trash, with versions; `trash_action restore` undoes it |

The caller owns every new document and version these create. `POST /api/documents/merge_as_versions/` is a seventh action endpoint, not exposed yet (#44).

Permissions (a 403 says only "Insufficient permissions"; superusers skip them; add and delete permissions are global, not per document): change permission on every document for all tools; ownership (or no owner) for delete, rotate, edit_pdf and remove_password, and for merge with `delete_originals`; add permission for merge and for edit_pdf / remove_password without `update_document`; delete permission for delete, merge with `delete_originals`, and edit_pdf / remove_password with `delete_original` and no `update_document`.

#### Tags

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `tag_list` | GET | `/api/tags/` | List all tags |
| `tag_get` | GET | `/api/tags/{id}/` | Get tag details |
| `tag_create` | POST | `/api/tags/` | Create a new tag |
| `tag_update` | PATCH | `/api/tags/{id}/` | Update a tag |
| `tag_delete` | DELETE | `/api/tags/{id}/` | Delete a tag |

##### tag_list

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| name | string | no | Filter by name (icontains) |
| is_root | boolean | no | Filter root tags only |
| ordering | string | no | Sort field |
| page | integer | no | Page number |
| page_size | integer | no | Results per page |

**Returns:** Paginated tag list with id, name, color, document_count, is_inbox_tag, parent, children.

##### tag_create

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| name | string | yes | Tag name |
| color | string | no | Hex color (e.g., `#a6cee3`) |
| is_inbox_tag | boolean | no | Whether this is an inbox tag |
| matching_algorithm | integer | no | Auto-matching algorithm |
| match | string | no | Match pattern |
| is_insensitive | boolean | no | Case-insensitive matching |
| parent | integer | no | Parent tag ID for hierarchy |

**Returns:** Created tag object.

#### Correspondents

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `correspondent_list` | GET | `/api/correspondents/` | List correspondents |
| `correspondent_get` | GET | `/api/correspondents/{id}/` | Get correspondent details |
| `correspondent_create` | POST | `/api/correspondents/` | Create correspondent |
| `correspondent_update` | PATCH | `/api/correspondents/{id}/` | Update correspondent |
| `correspondent_delete` | DELETE | `/api/correspondents/{id}/` | Delete correspondent |

##### correspondent_list

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| name | string | no | Filter by name (icontains) |
| ordering | string | no | Sort field |
| page | integer | no | Page number |
| page_size | integer | no | Results per page |

**Returns:** Paginated list with id, name, document_count, last_correspondence.

#### Document Types

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `document_type_list` | GET | `/api/document_types/` | List document types |
| `document_type_get` | GET | `/api/document_types/{id}/` | Get document type details |
| `document_type_create` | POST | `/api/document_types/` | Create document type |
| `document_type_update` | PATCH | `/api/document_types/{id}/` | Update document type |
| `document_type_delete` | DELETE | `/api/document_types/{id}/` | Delete document type |

#### Storage Paths

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `storage_path_list` | GET | `/api/storage_paths/` | List storage paths |
| `storage_path_get` | GET | `/api/storage_paths/{id}/` | Get storage path details |
| `storage_path_create` | POST | `/api/storage_paths/` | Create storage path |
| `storage_path_update` | PATCH | `/api/storage_paths/{id}/` | Update storage path |
| `storage_path_delete` | DELETE | `/api/storage_paths/{id}/` | Delete storage path |
| `storage_path_test` | POST | `/api/storage_paths/test/` | Test storage path template against a document |

#### Custom Fields

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `custom_field_list` | GET | `/api/custom_fields/` | List custom field definitions |
| `custom_field_get` | GET | `/api/custom_fields/{id}/` | Get custom field details |
| `custom_field_create` | POST | `/api/custom_fields/` | Create custom field |
| `custom_field_update` | PATCH | `/api/custom_fields/{id}/` | Update custom field |
| `custom_field_delete` | DELETE | `/api/custom_fields/{id}/` | Delete custom field |

##### custom_field_create

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| name | string | yes | Field name (must be unique) |
| data_type | string (enum) | yes | One of `string`, `url`, `date`, `boolean`, `integer`, `float`, `monetary`, `documentlink`, `select`, `longtext` (paperless-ngx `CustomField.FieldDataType`). Values outside this list are rejected before the request is sent. Treated as fixed after creation: paperless-ngx's UI doesn't allow changing it, and `custom_field_update` doesn't send it. |
| extra_data | object or JSON string | no | Object. Required for `select`: `{"select_options": [{"label": "..."}]}` (option ids are generated when omitted). Optional for `monetary`: `{"default_currency": "EUR"}` (3-letter code). |

##### custom_field_update

Accepts `id`, `name` and `extra_data`. It does not send `data_type`, because paperless-ngx treats the type as fixed (its model marks the field non-editable). For `select` fields, paperless-ngx requires `extra_data` with the full `select_options` list on every update (even a rename); keep each existing option's `id` so documents keep their values.

#### Search

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `search_autocomplete` | GET | `/api/search/autocomplete/` | Autocomplete search terms |
| `search_global` | GET | `/api/search/` | Quick lookup across object types: at most 3 results per type, not paginated. `query` (required) must be 3 to 4096 characters. Documents carry only `id`, `title` and `created`; use `document_list` with `query` for real document search |

#### Statistics

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `statistics` | GET | `/api/statistics/` | Get system statistics |

### Tier 2: Admin and System

#### Saved Views

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `saved_view_list` | GET | `/api/saved_views/` | List saved views |
| `saved_view_get` | GET | `/api/saved_views/{id}/` | Get saved view |
| `saved_view_create` | POST | `/api/saved_views/` | Create saved view. `name` and `filter_rules` are required (`[]` matches every document); optional `icon`, `sort_field`, `sort_reverse`, `page_size`, `display_mode`, `display_fields` |
| `saved_view_update` | PATCH | `/api/saved_views/{id}/` | Update saved view. `filter_rules` replaces all rules; `clear` resets `page_size`, `display_mode` and `display_fields` (`display_fields: []` also resets it) |
| `saved_view_delete` | DELETE | `/api/saved_views/{id}/` | Delete saved view |
| `saved_view_rule_types` | — | — | The filter rule type table (paperless-ngx 3.5.1), answered locally: each `rule_type` code's label, value format, whether it takes one rule per value, the matching `/api/documents/` filter, and notes. Optional `ids` selects codes |

A saved view's `filter_rules` is a list of `{rule_type, value}` combined with AND; `value` is a string or null. Multi-value rule types (such as 6, "has tag") take one rule per value. Field values: `icon` is one of paperless-ngx's 57 icon names (default `funnel`); `display_mode` is `table`, `smallCards` or `largeCards`; `display_fields` draws from `title`, `created`, `added`, `tag`, `correspondent`, `documenttype`, `storagepath`, `note`, `owner`, `shared`, `asn`, `pagecount` and `custom_field_<id>`. The server validates them.

Dashboard and sidebar visibility is not a saved view field under API v10: it is a per-user UI setting (`settings.saved_views.dashboard_views_visible_ids` / `sidebar_views_visible_ids`, readable with `ui_settings_get`), changed in the web UI. The tools don't write it.

#### Share Links

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `share_link_list` | GET | `/api/share_links/` | List share links (filter by created/expiration date, sort) |
| `share_link_get` | GET | `/api/share_links/{id}/` | Get share link |
| `share_link_create` | POST | `/api/share_links/` | Create share link (the server generates the slug) |
| `share_link_delete` | DELETE | `/api/share_links/{id}/` | Delete share link |

The share link API has no update: `ShareLinkViewSet` supports create, retrieve, list and destroy only, so a PATCH gets 405. To change a link, delete it and create a new one, which gets a new random 50-character slug. The public URL of a link is `{PAPERLESS_URL}/share/{slug}`. Results have `id`, `created`, `expiration` (null: never expires), `slug`, `document`, `file_version` and a read-only `document_title`.

**share_link_list parameters:**

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| created_after | string | no | Created on or after (YYYY-MM-DD, `created__date__gte`) |
| created_before | string | no | Created on or before (YYYY-MM-DD, `created__date__lte`) |
| expiration_after | string | no | Expires on or after (YYYY-MM-DD, `expiration__date__gte`) |
| expiration_before | string | no | Expires on or before (YYYY-MM-DD, `expiration__date__lte`) |
| ordering | string | no | `created`, `expiration` or `document__title` (prefix `-` for descending) |
| page | number | no | Page number |
| page_size | number | no | Results per page |

**share_link_create parameters:**

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| document | number | yes | Document ID |
| expiration | string | no | Expiration date (YYYY-MM-DD or ISO 8601); omit for no expiry |
| file_version_archive | boolean | no | Share the archived version (`true`) or the original (`false`) |

#### Users

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `user_list` | GET | `/api/users/` | List users (`inherited_permissions` omitted unless `include_permissions` is set) |
| `user_get` | GET | `/api/users/{id}/` | Get user details |
| `user_create` | POST | `/api/users/` | Create user |
| `user_update` | PATCH | `/api/users/{id}/` | Update user |
| `user_delete` | DELETE | `/api/users/{id}/` | Delete user |
| `user_deactivate_totp` | POST | `/api/users/{id}/deactivate_totp/` | Deactivate TOTP for a user (admin) |

#### Profile

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `profile_get` | GET | `/api/profile/` | Get current user's profile (the `auth_token` field is redacted so the API token never enters a transcript) |
| `profile_update` | PATCH | `/api/profile/` | Update current user's profile |

#### Groups

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `group_list` | GET | `/api/groups/` | List groups |
| `group_get` | GET | `/api/groups/{id}/` | Get group details |
| `group_create` | POST | `/api/groups/` | Create group |
| `group_update` | PATCH | `/api/groups/{id}/` | Update group |
| `group_delete` | DELETE | `/api/groups/{id}/` | Delete group |

#### Mail Accounts

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `mail_account_list` | GET | `/api/mail_accounts/` | List mail accounts |
| `mail_account_get` | GET | `/api/mail_accounts/{id}/` | Get mail account |
| `mail_account_create` | POST | `/api/mail_accounts/` | Create mail account |
| `mail_account_update` | PATCH | `/api/mail_accounts/{id}/` | Update mail account |
| `mail_account_delete` | DELETE | `/api/mail_accounts/{id}/` | Delete mail account |
| `mail_account_test` | POST | `/api/mail_accounts/test/` | Test mail account connectivity |
| `mail_account_process` | POST | `/api/mail_accounts/{id}/process/` | Manually process account |

#### Mail Rules

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `mail_rule_list` | GET | `/api/mail_rules/` | List mail rules |
| `mail_rule_get` | GET | `/api/mail_rules/{id}/` | Get mail rule |
| `mail_rule_create` | POST | `/api/mail_rules/` | Create mail rule |
| `mail_rule_update` | PATCH | `/api/mail_rules/{id}/` | Update mail rule |
| `mail_rule_delete` | DELETE | `/api/mail_rules/{id}/` | Delete mail rule |

#### Processed Mail

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `processed_mail_list` | GET | `/api/processed_mail/` | List processed mail records |
| `processed_mail_get` | GET | `/api/processed_mail/{id}/` | Get processed mail details |
| `processed_mail_bulk_delete` | POST | `/api/processed_mail/bulk_delete/` | Bulk delete processed mail records |

#### Workflows

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `workflow_list` | GET | `/api/workflows/` | List workflows |
| `workflow_get` | GET | `/api/workflows/{id}/` | Get workflow |
| `workflow_create` | POST | `/api/workflows/` | Create workflow |
| `workflow_update` | PATCH | `/api/workflows/{id}/` | Update workflow |
| `workflow_delete` | DELETE | `/api/workflows/{id}/` | Delete workflow |

#### Workflow Triggers

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `workflow_trigger_list` | GET | `/api/workflow_triggers/` | List triggers |
| `workflow_trigger_get` | GET | `/api/workflow_triggers/{id}/` | Get trigger |
| `workflow_trigger_create` | POST | `/api/workflow_triggers/` | Create trigger |
| `workflow_trigger_update` | PATCH | `/api/workflow_triggers/{id}/` | Update trigger |
| `workflow_trigger_delete` | DELETE | `/api/workflow_triggers/{id}/` | Delete trigger |

#### Workflow Actions

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `workflow_action_list` | GET | `/api/workflow_actions/` | List actions |
| `workflow_action_get` | GET | `/api/workflow_actions/{id}/` | Get action |
| `workflow_action_create` | POST | `/api/workflow_actions/` | Create action |
| `workflow_action_update` | PATCH | `/api/workflow_actions/{id}/` | Update action |
| `workflow_action_delete` | DELETE | `/api/workflow_actions/{id}/` | Delete action |

#### Tasks

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `task_list` | GET | `/api/tasks/` | List background tasks, paginated by the server. Filters: `status`, `task_type`, `trigger_source` (arrays, sent as repeated query parameters), `is_complete`, `acknowledged`, `owner`, `name`, `result`, `date_created_after`, `date_created_before`; `ordering` on `date_created`, `date_done`, `status`, `task_type`, `duration_seconds`, `wait_time_seconds` (default `-date_created`). `result_data.traceback` is trimmed to its last 500 bytes unless `include_traceback` is set |
| `task_get` | GET | `/api/tasks/?task_id={uuid}` or `/api/tasks/{id}/` | Get one task by task UUID or integer id. The UUID lookup's envelope is unwrapped; no match is an error. The traceback is returned in full |
| `task_acknowledge` | POST | `/api/tasks/acknowledge/` | Acknowledge tasks: exactly one of `tasks` (integer IDs) or `all: true` (every visible unacknowledged task, including pending and failed ones). Returns `{"result": count}` |
| `task_run` | POST | `/api/tasks/run/` | Run a system task (superuser only). `task_type` is one of `train_classifier`, `sanity_check`, `llm_index`. Optional `wait` / `timeout` (see Waiting for tasks) |
| `task_wait` | GET | `/api/tasks/?task_id={uuid}` or `/api/tasks/{id}/`, polled | Wait for a task to finish (success, failure or revoked). `id` is a task UUID or integer id; `timeout` in seconds (default 60, at most 600). See Waiting for tasks |
| `task_stats` | GET | `/api/tasks/summary/`, `/api/tasks/status_counts/`, `/api/tasks/active/` | `mode` picks the endpoint: `summary` (per task type over `days`, 1–365, default 30), `status_counts` (`all`, `needs_attention`, `in_progress`, `completed`; takes `task_list`'s filters except `status` and `is_complete`), `active` (pending and started tasks, at most 50). Parameters outside the chosen mode are rejected |

#### Logs

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `log_list` | GET | `/api/logs/` | List available log files |
| `log_get` | GET | `/api/logs/{id}/` | Get log contents (`tail` parameter, default last 100 lines; returns `total_lines` and `lines`) |

#### Trash

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `trash_list` | GET | `/api/trash/` | List trashed documents |
| `trash_action` | POST | `/api/trash/` | `restore` (needs `documents`) or `empty` (permanent; needs `documents`, or `all: true` for the whole trash: everything for a superuser, otherwise the trashed documents the user owns or that have no owner). Leaving out `documents` is an error, not "everything" |

#### Bulk Edit Objects

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `bulk_edit_objects` | POST | `/api/bulk_edit_objects/` | `set_permissions` or `delete` on tags, correspondents, document types, storage paths |

`object_type` and `operation` are enums. Objects are selected by `objects`, or `all: true` with `filters`: `name` (contains) for every type, `is_root` for tags, `path` (contains) for storage paths, the same as the `*_list` tools (`storage_path_list` has `path`). `all: true` acts on the objects the user can change (`set_permissions`) or delete (`delete`), not merely view, so it can differ from a `*_list` count; non-superusers must own every selected object. For tags, `all: true` also takes editable child tags, and a tag delete always takes child tags. `set_permissions` takes top-level `owner`, `permissions` (`{view?: {users?, groups?}, change?: {...}}`; an empty array revokes) and `merge`; `clear: ["owner"]` removes the owner (not with `merge`, which keeps it). Leaving out `owner` keeps it. The old `parameters` object is refused: the server reads these fields at the top level, so it was silently ignored.

#### System

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `system_status` | GET | `/api/status/` | System status (admin only) |
| `remote_version` | GET | `/api/remote_version/` | Check for updates |
| `ui_settings_get` | GET | `/api/ui_settings/` | Get UI settings |
| `config_list` | GET | `/api/config/` | List app configuration |
| `config_get` | GET | `/api/config/{id}/` | Get config entry |
| `config_update` | PATCH | `/api/config/{id}/` | Update config entry |

## Waiting for Tasks

Since paperless-ngx 3.5.0 the document bulk and action endpoints queue a task and return `{"result": "OK", "task_id": …}` at once; uploads have always returned the consume task's UUID. Nothing has changed when these return, and runtime errors (missing permissions on a document, a wrong PDF password) exist only in the task record.

- `document_upload`, `document_bulk_edit`, the document editing tools and `task_run` take `wait` (default false: return the id at once, unchanged) and `timeout` (whole seconds, 1 to 600, default 60; only with `wait`). With `wait` the tool returns `{response, task, waited_seconds}`, where `response` is the original API response.
- `task_wait` waits on a task started earlier or elsewhere; an unknown task id fails at once.
- Polling starts at 1 s and grows by 1.5× to at most 5 s. Each poll sends an MCP progress notification when the client sent a `progressToken`.
- A `failure` or `revoked` task is a tool error carrying the task record. A problem with waiting itself after a successful POST (the task not appearing, repeated poll errors) is not: the result carries the original `response`, `task_id` and `wait_error`, with a note not to resend. Up to two polls in a row may fail with a network error or a 5xx before waiting gives up, and each poll is bounded by the deadline. Running out of time is not: the current task comes back with `timed_out: true`, and `task_wait` continues. Right after a POST, a task that can't be found yet gets a 5 s grace period.
- What finished covers: an upload's task is the whole consume. `document_bulk_edit`'s metadata methods and `document_bulk_delete` are done when their task is; `document_rotate`, `document_merge`, `document_edit_pdf` and `document_remove_password` schedule `consume_file` tasks, `document_reprocess` schedules `reprocess_document` tasks, and merge with `delete_originals`, or edit_pdf / remove_password with `delete_original` (and no `update_document`), also schedule `bulk_delete` once the consume tasks succeed (none appears if one fails). Skipped documents schedule nothing (rotate and merge skip non-PDFs, remove_password skips unencrypted PDFs). The bulk task record doesn't link to them, so the wait result's `follow_on` names the task types and a `task_list` filter (`date_created_after` the bulk task) to find them.
- MCP hosts often limit a tool call to about 60 s; a longer wait may be cut off by the host, and `task_wait` with the same id picks it up.
- The stdio server runs up to 20 tool calls at once (mcp-go's default is 5), so waiting calls don't stall other tools; past 20 running and 100 queued calls, mcp-go runs a call on its read loop. Shutdown interrupts a wait.

## Selecting Documents by Filter

`document_bulk_edit`, `document_selection_data`, `document_rotate`, `document_reprocess` and `document_bulk_delete` take either `documents` (explicit ids) or `all: true` with an optional `filters` object and `excluded_documents`. Upstream, `all: true` selects every root document the user can view that matches the filters (resolved when the request arrives; versions never included). paperless-ngx ignores unknown filter keys and any value it can't parse, so a typo would select every document; the MCP therefore:

- takes `document_list`'s filter names (one shared table maps them to the raw query names) and rejects unknown names;
- rejects values upstream would drop: blank text (after Python-style stripping), text with NUL, impossible dates (`2024-02-30`, year 0), tag lists with empty items or non-integers, booleans other than `true`/`false`, `null` values inside `filters`, a `query` of only wildcards (`*`, `?`) or over 4096 characters, and `query` with `more_like_id`;
- sends every value as a string (ints in decimal, booleans as `"true"`/`"false"`, tag lists as `"1,2"`, `custom_field_query` as JSON text), since the server's filters only match strings (a JSON `true` for `is_in_inbox` is ignored);
- rejects `all: true` with `documents`, and `filters` or `excluded_documents` without `all: true`. `all` must be a boolean (`1` or `"t"` are errors).

There is no count guard: `all: true` is the explicit request. Without filters it means every document. Preview with `document_list` and the same filters (`page_size: 1` gives `count`). Filters that widen by design: `more_like_id`, `tags_id_none`, `is_in_inbox: false`, `NOT` in `custom_field_query`, and `query`'s search syntax. Permissions are all-or-nothing: one selected document the user can't change (or doesn't own, where ownership is needed) fails the whole call with a 403. `bulk_download` also accepts `all` upstream but isn't exposed.

The `all` ID array that list responses carried below API v10 no longer exists, so `include_all_ids` is gone.

## Null Values

Some MCP clients send every declared parameter, filling unused ones with `null`. A top-level `null` therefore always means "not given", in every tool: middleware drops it before any handler runs. The string `"null"` is dropped too, except where it is real text and can't be padding: search text (`query`, `term`, task `result`, and the list tools' `name`/`title`/`content`/`path` filters) and required free-text parameters (a note, a new object's name). Optional fields that a write would overwrite (a password, a username, a title, OCR text) drop it, so a padding client can't set a password to "null". An empty string means "not given" for optional parameters; so do `[]` and `{}` (whitespace aside) for number and true/false parameters, and `{}` for optional object parameters (`extra_data`, `permissions`). An empty list that replaces values is real: `document_update` `tags: []` and `custom_fields: []` remove them all, and `filter_rules: []` replaces every rule. This covers `document_update` `content` (clear it with `clear: ["content"]`). A number or boolean that doesn't parse is an error rather than a silent `0`, `false` or default; booleans accept `1` and `0`, except the `all` selectors and selection filters, which take only true/false. An out-of-range number gets an error naming the range. Before this, a padded `null` became a real value in many tools (`document_update` `content: null` wiped the OCR text; `user_update` `is_superuser: null` sent `false`; list filters became `=0` or `=false`).

Clearing a field takes an explicit `clear` array naming it: `document_update` (`correspondent`, `document_type`, `storage_path`, `archive_serial_number`, `owner`), `tag_update` (`parent`), `saved_view_update` (`page_size`, `display_mode`, `display_fields`) and `bulk_edit_objects` (`owner`). A field both given a value and listed in `clear` is an error. Nulls nested inside JSON objects (such as `document_bulk_edit`'s `parameters`, workflow and mail rule bodies, custom field values) are sent as given.

## Response Shaping

Paperless-ngx API responses can be far too large for MCP clients (full OCR text per list result, long task tracebacks). The server reshapes responses to keep tool output compact:

- **Content truncation**: `document_list` truncates each document's `content` to a snippet (default 500 bytes, adjustable per call with `content_snippet_bytes`; `0` disables truncation), marking truncated entries with `content_truncated: true`. Opt out per-call with `full_content: true`, or use `document_get` for one document's full text.
- **Notes stripped**: `document_list` replaces each document's unbounded `notes` array with a `notes_count` field (omitted when zero). Opt out per-call with `include_notes: true`, or use `document_note_list`/`document_get` for one document's full notes.
- **Task tracebacks trimmed**: `task_list` keeps the last 500 bytes of each `result_data.traceback` (the end, where the exception is), prefixed with `…` and marked `traceback_truncated: true`. Opt out with `include_traceback: true`, or use `task_get` for one task's full traceback.
- **Log tailing**: `log_get` returns only the last `tail` lines (default 100) with a `total_lines` count.
- **Selection data filtering**: `document_selection_data` omits objects with `document_count: 0`.
- **Slim user lists**: `user_list` omits the large `inherited_permissions` list unless `include_permissions: true`.
- **Token redaction**: `profile_get` replaces `auth_token` with `[redacted]`.

## MCP Server Instructions

The MCP server provides inline instructions via `WithInstructions` that guide agent behavior:

- Available tool categories and when to use them
- How pagination works (use `page` and `page_size` parameters)
- How full-text search works (`query` parameter on `document_list`)
- How tag filtering works (`tags_id_all`, `tags_id_none`, `tags_id_in`)
- How custom field queries work
- Metadata update workflow: get document, review fields, update with `document_update`
- Bulk operations: `document_bulk_edit` for metadata changes; the document editing tools for file operations
- Saved views: filter rules, the `saved_view_rule_types` lookup, and that dashboard/sidebar visibility is a web UI setting
- Structured parameters: arrays and objects may be sent natively or as JSON-encoded strings
- Tasks: statuses, UUID-or-id lookup, traceback trimming, `task_stats`, and waiting (`wait`, `task_wait`)
- API version is always v10 (Paperless-ngx 3.5.0+)

## Technical Requirements

- **Language**: Go 1.27.1 (managed via mise, pinned in `.mise.toml`)
- **MCP SDK**: github.com/mark3labs/mcp-go
- **CLI**: github.com/spf13/cobra
- **HTTP**: net/http (stdlib)
- **JSON**: encoding/json (stdlib)
- **Transport**: stdio (stdin/stdout JSON) for MCP
- **Platform**: macOS, Linux, Windows (anywhere Go compiles)
- **CGO**: Disabled (CGO_ENABLED=0) for static binaries
- **Test coverage**: Minimum 70% statement coverage

### Project Structure

```
paperless-ngx-mcp/
├── main.go                 # Entry point, Cobra root command
├── server.go               # MCP server setup and tool registration
├── client.go               # Paperless-ngx HTTP client
├── tools_documents.go      # Document tool handlers
├── tools_tags.go           # Tag tool handlers
├── tools_correspondents.go # Correspondent tool handlers
├── tools_document_types.go # Document type tool handlers
├── tools_storage_paths.go  # Storage path tool handlers
├── tools_custom_fields.go  # Custom field tool handlers
├── tools_search.go         # Search and statistics tool handlers
├── tools_bulk.go           # Bulk operation tool handlers
├── tools_admin.go          # Tier 2 admin/system tool handlers
├── models.go               # API response/request structs
├── Makefile                # Build, test, release targets
├── Dockerfile              # Multi-stage build
├── .mise.toml              # mise tool versions (Go 1.27.1)
├── .goreleaser.yaml        # GoReleaser config with MCPB bundles
├── manifest.json           # MCPB bundle manifest
├── go.mod / go.sum         # Dependencies
├── .github/workflows/
│   ├── ci.yml              # CI: vet, staticcheck, test, coverage, build
│   └── release.yml         # Release: GoReleaser + MCPB bundles + Docker
├── .omp/
│   ├── AGENTS.md           # Developer guide (omp agent context)
│   ├── mcp.json            # Local development MCP config (gopls)
│   ├── rules/              # Agent rules (Paperless MCP tool usage, docs sync)
│   └── skills/test-mcp/    # Live-instance smoke test skill
├── README.md               # User documentation
└── docs/PRD.md             # This document
```

### HTTP Client Design

The HTTP client (`client.go`) encapsulates all Paperless-ngx API communication:

```go
type Client struct {
    baseURL    string
    token      string
    httpClient *http.Client
}
```

The API version is hardcoded to `10` — the `Accept: application/json; version=10` header is always sent.

**Key methods:**

- `Get(path string, params url.Values) (*http.Response, error)` — GET with query params
- `Post(path string, body interface{}) (*http.Response, error)` — POST JSON
- `Patch(path string, body interface{}) (*http.Response, error)` — PATCH JSON
- `Delete(path string) (*http.Response, error)` — DELETE
- `PostMultipart(path string, fields map[string]string, file io.Reader, filename string) (*http.Response, error)` — Multipart upload

All methods inject `Authorization` and `Accept` headers automatically.

### Tool Handler Pattern

Each tool handler follows the same pattern as tasks-mcp:

1. Extract parameters from the MCP request
2. Validate required parameters
3. Build HTTP request via the client
4. Parse the response
5. Return `*mcp.CallToolResult` with JSON content or error

### Error Handling

HTTP errors are translated to structured MCP error responses:

```json
{
    "error": true,
    "status_code": 404,
    "detail": "Not found.",
    "endpoint": "GET /api/documents/999/"
}
```

## Build and Release

### Build

```sh
make build              # build binary
make test               # run tests
make test-coverage      # tests with coverage report
make vet                # go vet
make lint               # vet + staticcheck + test
make release-snapshot   # local goreleaser build (no publish)
```

### CI Workflow

Mirrors tasks-mcp CI:
- Trigger: push to main, pull requests
- Steps: checkout, detect Go changes, vet, staticcheck, test with race detector, enforce 70% coverage, build
- Protected main branch — PRs required

### Release Workflow

Mirrors tasks-mcp release:
- Trigger: push semver tags (`v*`)
- GoReleaser: cross-platform binaries (darwin/linux/windows x amd64/arm64)
- MCPB bundles: platform-specific `.mcpb` ZIP archives uploaded to GitHub release
- Docker: multi-arch images pushed to `ghcr.io/freeformz/paperless-ngx-mcp`

### MCPB Manifest

```json
{
    "manifest_version": "0.3",
    "name": "paperless-ngx-mcp",
    "display_name": "Paperless-ngx MCP",
    "version": "0.0.0",
    "description": "MCP server for Paperless-ngx document management. Search, browse, and manage documents and metadata.",
    "author": {
        "name": "freeformz",
        "url": "https://github.com/freeformz"
    },
    "server": {
        "type": "binary",
        "entry_point": "server/paperless-ngx-mcp",
        "mcp_config": {
            "command": "${__dirname}/server/paperless-ngx-mcp",
            "args": ["mcp"],
            "env": {
                "PAPERLESS_URL": "",
                "PAPERLESS_TOKEN": ""
            }
        }
    },
    "tools": [
        {"name": "document_list", "description": "Search and list documents"},
        {"name": "document_get", "description": "Get document details"},
        {"name": "document_update", "description": "Update document metadata"},
        {"name": "tag_list", "description": "List tags"},
        {"name": "correspondent_list", "description": "List correspondents"},
        {"name": "document_type_list", "description": "List document types"}
    ],
    "compatibility": {
        "platforms": ["darwin", "win32", "linux"]
    }
}
```

## Development Workflow

- **Never push directly to main** — main is protected by branch rulesets
- Create a feature branch, make changes, push, and open a PR
- CI must pass before merging
- Merge via GitHub PR (squash, merge, or rebase — all allowed)
- Delete the feature branch after merging

## Testing Strategy

- **Unit tests**: Mock HTTP responses to test tool handlers independently
- **Integration tests**: Optional, require a running Paperless-ngx instance (skipped in CI by default)
- **Client tests**: Test HTTP client request construction, header injection, error handling
- **Shutdown tests**: `shutdown_test.go` (Unix only) runs the test binary as the `mcp` command in a subprocess, since its `TestMain` calls `main()` when `PAPERLESS_MCP_TEST_RUN_MAIN=1`, and checks that SIGINT, SIGTERM and SIGHUP each exit 0 with nothing on stderr, that they and a host disconnect mid-call all remove the temp download directory, that stdin EOF or SIGTERM during a slow render still exits 0 promptly, and that a second signal ends a stalled shutdown
- **CLI tests**: `main_test.go` checks that flag errors print usage and other errors, such as an unset `PAPERLESS_URL` or `PAPERLESS_TOKEN`, don't
- **Coverage threshold**: 70% minimum (enforced in CI)

Test helpers should provide:
- A mock HTTP server that returns canned Paperless-ngx API responses
- Builder functions for constructing test request/response pairs
- Shared fixtures for common API objects (documents, tags, etc.)

## Future Considerations

These are explicitly out of scope for the current version but may be considered later:

- **Document download/preview/thumbnail tools** — Binary file retrieval (`document_download`, `document_preview`, `document_thumbnail`, `document_bulk_download`). Requires design work on how binary content is handled in an MCP context where the agent cannot directly render files. Document list/get tools already return API URL paths for these resources.
- **Claude Code hooks** — Session start/stop hooks for surfacing document state
- **CLI subcommands** — Human-facing commands for listing documents, tags, etc.
- **Batch tool calls** — Combine multiple list operations into a single tool for efficiency
- **Document content extraction** — Return document text content for agent analysis
- **Webhook integration** — React to Paperless-ngx events in real-time

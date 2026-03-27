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
5. **Zero local state** — Purely pass-through to the Paperless-ngx server; no local database or caching
6. **CoWork compatible** — Works as an MCPB bundle for Claude Code and CoWork agent teams
7. **Same toolchain as tasks-mcp** — Go, mcp-go, Cobra, GoReleaser, same CI/CD patterns

## Non-Goals

- Local document storage or caching
- Document content modification (PDF editing, OCR, etc.) — metadata only
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

The server always uses Paperless-ngx API version 9. These are passed via the MCP server configuration (`.mcp.json` or MCPB manifest) as environment variables, which is compatible with CoWork and MCPB bundle format.

### HTTP Client

- **Authentication**: Token-based via `Authorization: Token <token>` header
- **API versioning**: `Accept: application/json; version=9` header on all requests (hardcoded to v9)
- **Pagination**: Automatic handling — list tools accept `page` and `page_size` parameters; responses include `count`, `next`, `previous`, and `results`
- **Error handling**: HTTP error responses are translated to MCP error results with status code, detail message, and endpoint context
- **Timeouts**: Configurable per-request timeout with sensible default (30s)
- **Content types**: JSON for most endpoints; multipart/form-data for document uploads

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
| custom_field_query | string | no | JSON custom field filter expression |
| created_after | string | no | Filter by created date (gte) |
| created_before | string | no | Filter by created date (lte) |
| added_after | string | no | Filter by added date (gte) |
| added_before | string | no | Filter by added date (lte) |
| owner_id | integer | no | Filter by owner |
| ordering | string | no | Sort field (prefix `-` for descending) |
| page | integer | no | Page number (default: 1) |
| page_size | integer | no | Results per page (default: 25, max: 100000) |

**Returns:** Paginated document list with count, next/previous page URLs, and document objects. Each document object includes API URLs for download (`/api/documents/{id}/download/`), preview (`/api/documents/{id}/preview/`), and thumbnail (`/api/documents/{id}/thumb/`) — these are unauthenticated URL paths relative to `PAPERLESS_URL` that require a token to fetch. When `query` is used, includes `__search_hit__` with score, highlights, and rank.

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
| created | string | no | New created date (YYYY-MM-DD) |
| correspondent | integer | no | Correspondent ID (null to clear) |
| document_type | integer | no | Document type ID (null to clear) |
| storage_path | integer | no | Storage path ID (null to clear) |
| tags | string | no | JSON array of tag IDs (replaces all tags) |
| archive_serial_number | integer | no | Archive serial number (null to clear) |
| custom_fields | string | no | JSON array of custom field assignments |

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
| tags | string | no | JSON array of tag IDs |
| archive_serial_number | integer | no | Archive serial number |

**Returns:** Task UUID for tracking consumption status.

#### Document Notes

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `document_note_list` | GET | `/api/documents/{id}/notes/` | List notes on a document |
| `document_note_add` | POST | `/api/documents/{id}/notes/` | Add a note to a document |
| `document_note_delete` | DELETE | `/api/documents/{id}/notes/?id={note_id}` | Delete a note |

#### Document Bulk Operations

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `document_bulk_edit` | POST | `/api/documents/bulk_edit/` | Bulk edit documents |
| `document_selection_data` | POST | `/api/documents/selection_data/` | Get aggregated metadata counts for a selection |

##### document_bulk_edit

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| documents | string | yes | JSON array of document IDs |
| method | string | yes | Operation: `set_correspondent`, `set_document_type`, `set_storage_path`, `add_tag`, `remove_tag`, `modify_tags`, `delete`, `reprocess`, `set_permissions`, `modify_custom_fields`, `rotate`, `delete_pages`, `split`, `merge`, `edit_pdf` |
| parameters | string | yes | JSON object with method-specific parameters |

**Returns:** Confirmation of bulk operation.

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

#### Search

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `search_autocomplete` | GET | `/api/search/autocomplete/` | Autocomplete search terms |
| `search_global` | GET | `/api/search/` | Global search across all object types |

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
| `saved_view_create` | POST | `/api/saved_views/` | Create saved view |
| `saved_view_update` | PATCH | `/api/saved_views/{id}/` | Update saved view |
| `saved_view_delete` | DELETE | `/api/saved_views/{id}/` | Delete saved view |

#### Share Links

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `share_link_list` | GET | `/api/share_links/` | List share links |
| `share_link_get` | GET | `/api/share_links/{id}/` | Get share link |
| `share_link_create` | POST | `/api/share_links/` | Create share link |
| `share_link_update` | PATCH | `/api/share_links/{id}/` | Update share link |
| `share_link_delete` | DELETE | `/api/share_links/{id}/` | Delete share link |

#### Users

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `user_list` | GET | `/api/users/` | List users |
| `user_get` | GET | `/api/users/{id}/` | Get user details |
| `user_create` | POST | `/api/users/` | Create user |
| `user_update` | PATCH | `/api/users/{id}/` | Update user |
| `user_delete` | DELETE | `/api/users/{id}/` | Delete user |
| `user_deactivate_totp` | POST | `/api/users/{id}/deactivate_totp/` | Deactivate TOTP for a user (admin) |

#### Profile

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `profile_get` | GET | `/api/profile/` | Get current user's profile |
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
| `task_list` | GET | `/api/tasks/` | List background tasks |
| `task_get` | GET | `/api/tasks/{id}/` | Get task details |
| `task_acknowledge` | POST | `/api/tasks/acknowledge/` | Acknowledge tasks |
| `task_run` | POST | `/api/tasks/run/` | Run system task (admin only) |

#### Logs

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `log_list` | GET | `/api/logs/` | List available log files |
| `log_get` | GET | `/api/logs/{id}/` | Get log contents |

#### Trash

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `trash_list` | GET | `/api/trash/` | List trashed documents |
| `trash_action` | POST | `/api/trash/` | Restore or permanently delete trashed documents |

#### Bulk Edit Objects

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `bulk_edit_objects` | POST | `/api/bulk_edit_objects/` | Bulk permissions/delete for tags, correspondents, document types, storage paths |

#### System

| Tool | Method | Endpoint | Description |
|------|--------|----------|-------------|
| `system_status` | GET | `/api/status/` | System status (admin only) |
| `remote_version` | GET | `/api/remote_version/` | Check for updates |
| `ui_settings_get` | GET | `/api/ui_settings/` | Get UI settings |
| `config_list` | GET | `/api/config/` | List app configuration |
| `config_get` | GET | `/api/config/{id}/` | Get config entry |
| `config_update` | PATCH | `/api/config/{id}/` | Update config entry |

## MCP Server Instructions

The MCP server provides inline instructions via `WithInstructions` that guide agent behavior:

- Available tool categories and when to use them
- How pagination works (use `page` and `page_size` parameters)
- How full-text search works (`query` parameter on `document_list`)
- How tag filtering works (`tags_id_all`, `tags_id_none`, `tags_id_in`)
- How custom field queries work
- Metadata update workflow: get document, review fields, update with `document_update`
- Bulk operations: use `document_bulk_edit` for batch changes
- API version is always v9

## Technical Requirements

- **Language**: Go 1.26.1 (managed via mise, pinned in `.mise.toml`)
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
├── .mise.toml              # mise tool versions (Go 1.26.1)
├── .goreleaser.yaml        # GoReleaser config with MCPB bundles
├── manifest.json           # MCPB bundle manifest
├── .mcp.json               # Local development MCP config
├── go.mod / go.sum         # Dependencies
├── .github/workflows/
│   ├── ci.yml              # CI: vet, staticcheck, test, coverage, build
│   └── release.yml         # Release: GoReleaser + MCPB bundles + Docker
├── .claude/
│   ├── settings.json       # Project settings
│   └── rules/paperless.md  # Rules for how Claude uses the MCP tools
├── CLAUDE.md               # Developer guide
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

The API version is hardcoded to `9` — the `Accept: application/json; version=9` header is always sent.

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
- **Response caching** — Cache tag/correspondent/document_type lists to reduce API calls
- **Batch tool calls** — Combine multiple list operations into a single tool for efficiency
- **Document content extraction** — Return document text content for agent analysis
- **Webhook integration** — React to Paperless-ngx events in real-time

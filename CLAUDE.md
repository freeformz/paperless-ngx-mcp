# paperless-ngx-mcp

MCP server for Paperless-ngx document management, built in Go.

## Project Structure

- `main.go` — Entry point, Cobra root command, `mcp` subcommand
- `server.go` — MCP server setup, tool registration, agent instructions
- `client.go` — Paperless-ngx HTTP client with auth, versioning, multipart upload
- `helpers.go` — Shared helpers (errResult, jsonResult, doRequest, param helpers)
- `cache.go` — In-memory TTL cache for metadata list endpoints
- `tools_documents.go` — Document tool handlers (CRUD, notes, metadata, suggestions, upload, email)
- `tools_download.go` — Document download and cleanup tool handlers
- `download.go` — Downloader (per-instance temp dir, concurrency, file tracking)
- `tools_tags.go` — Tag tool handlers
- `tools_correspondents.go` — Correspondent tool handlers
- `tools_document_types.go` — Document type tool handlers
- `tools_storage_paths.go` — Storage path tool handlers
- `tools_custom_fields.go` — Custom field tool handlers
- `tools_search.go` — Search and statistics tool handlers
- `tools_bulk.go` — Bulk operation tool handlers
- `tools_saved_views.go` — Saved view tool handlers
- `tools_share_links.go` — Share link tool handlers
- `tools_users.go` — User, group, and profile tool handlers
- `tools_mail.go` — Mail account, mail rule, and processed mail tool handlers
- `tools_workflows.go` — Workflow, trigger, and action tool handlers
- `tools_system.go` — System status, config, tasks, logs, and trash tool handlers
- `test_helpers_test.go` — Shared test infrastructure (mock server, helpers)
- `.claude/rules/paperless.md` — Rules for how Claude uses the Paperless-ngx MCP tools
- `.goreleaser.yaml` — GoReleaser config (builds, archives, MCPB bundles)
- `Makefile` — Build, test, and release tasks
- `.github/workflows/` — CI (test on push/PR) and Release (on semver tags)

## Documentation

- Any changes to features or how tools work must be reflected in `docs/PRD.md`

## Build & Test

```sh
make build              # build binary
make test               # run tests
make test-coverage      # tests with coverage report
make vet                # go vet
make lint               # vet + staticcheck + test
make release-snapshot   # local goreleaser build (no publish)
```

## Development Workflow

- **Never push directly to main** — main is protected by branch rulesets
- Create a feature branch, make changes, push, and open a PR
- CI must pass before merging
- Merge via GitHub PR (squash, merge, or rebase — all allowed)
- Delete the feature branch after merging

## Releases

- Uses GoReleaser with MCPB bundle support
- Triggered by pushing a semver tag: `git tag v0.1.0 && git push origin v0.1.0`
- Tags must follow Go module versioning: `v{MAJOR}.{MINOR}.{PATCH}`
- CI runs on push to main and PRs; release workflow runs on `v*` tags
- Version is injected into the binary via ldflags (`main.version`)

## Configuration

The MCP server is configured via environment variables:

| Variable | Required | Description |
|----------|----------|-------------|
| `PAPERLESS_URL` | yes | Base URL of the Paperless-ngx instance |
| `PAPERLESS_TOKEN` | yes | API authentication token |

## Architecture

- **HTTP Client**: Token auth + API v9 versioning on all requests
- **MCP transport**: stdio via mark3labs/mcp-go
- **Caching**: In-memory TTL cache for metadata lists (tags, correspondents, document types, storage paths, custom fields)
- **Stateless**: No local database — all state lives in Paperless-ngx

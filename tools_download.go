package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerDownloadTools(srv *server.MCPServer, client *Client, dl *Downloader) {
	srv.AddTool(
		mcp.NewTool("document_download",
			mcp.WithDescription("Download one or more document files. By default saves to local temp storage and returns file paths (use cleanup_downloads to remove). Set content=true to return base64-encoded file content inline instead."),
			mcp.WithString("ids", mcp.Description("JSON array of document IDs to download"), mcp.Required()),
			mcp.WithString("variant", mcp.Description("File variant: archived (default, OCR'd PDF/A), original (as uploaded), or thumbnail")),
			mcp.WithBoolean("content", mcp.Description("Return base64-encoded file content inline instead of saving to disk")),
		),
		handleDocumentDownload(client, dl),
	)

	srv.AddTool(
		mcp.NewTool("cleanup_downloads",
			mcp.WithDescription("Clean up downloaded document files. With no arguments, removes all downloaded files. Pass specific file paths to remove only those."),
			mcp.WithString("files", mcp.Description("JSON array of file paths to remove (must be inside download directory). Omit to remove all.")),
		),
		handleCleanupDownloads(dl),
	)
}

// downloadResult represents the outcome of downloading a single document.
type downloadResult struct {
	ID          int    `json:"id"`
	Content     string `json:"content,omitempty"`      // base64-encoded file content (default mode)
	ContentType string `json:"content_type,omitempty"` // MIME type
	Filename    string `json:"filename,omitempty"`     // original filename from server
	Path        string `json:"path,omitempty"`         // local file path (save_to_disk mode)
	Error       string `json:"error,omitempty"`
}

// fetchedDocument holds raw data from an HTTP download before encoding/saving.
type fetchedDocument struct {
	body        []byte
	contentType string
	filename    string
	ext         string
}

func handleDocumentDownload(client *Client, dl *Downloader) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		idsStr, errRes := getRequiredString(request, "ids")
		if errRes != nil {
			return errRes, nil
		}

		var ids []int
		if err := json.Unmarshal([]byte(idsStr), &ids); err != nil {
			return errResult(fmt.Sprintf("invalid ids JSON: %s", err)), nil
		}
		if len(ids) == 0 {
			return errResult("ids must contain at least one document ID"), nil
		}

		variant := request.GetString("variant", "archived")
		if variant == "" {
			variant = "archived"
		}
		switch variant {
		case "archived", "original", "thumbnail":
		default:
			return errResult(fmt.Sprintf("invalid variant %q: must be archived, original, or thumbnail", variant)), nil
		}

		returnContent := request.GetBool("content", false)

		results := make([]downloadResult, len(ids))

		type downloadJob struct {
			idx   int
			docID int
		}

		jobs := make(chan downloadJob)
		var wg sync.WaitGroup

		workerCount := min(dl.Concurrency(), len(ids))
		for range workerCount {
			wg.Go(func() {
				for job := range jobs {
					if ctx.Err() != nil {
						results[job.idx] = downloadResult{ID: job.docID, Error: ctx.Err().Error()}
						continue
					}
					doc, err := fetchDocument(ctx, client, job.docID, variant)
					if err != nil {
						results[job.idx] = downloadResult{ID: job.docID, Error: err.Error()}
						continue
					}
					if returnContent {
						results[job.idx] = downloadResult{
							ID:          job.docID,
							Content:     base64.StdEncoding.EncodeToString(doc.body),
							ContentType: doc.contentType,
							Filename:    doc.filename,
						}
					} else {
						path, err := saveDocument(dl, doc)
						if err != nil {
							results[job.idx] = downloadResult{ID: job.docID, Error: err.Error()}
						} else {
							results[job.idx] = downloadResult{
								ID:          job.docID,
								Path:        path,
								ContentType: doc.contentType,
								Filename:    doc.filename,
							}
						}
					}
				}
			})
		}

		for i, id := range ids {
			select {
			case jobs <- downloadJob{idx: i, docID: id}:
			case <-ctx.Done():
				for j := i; j < len(ids); j++ {
					results[j] = downloadResult{ID: ids[j], Error: ctx.Err().Error()}
				}
				goto done
			}
		}
	done:
		close(jobs)
		wg.Wait()

		resp := map[string]any{
			"results": results,
		}
		if !returnContent {
			resp["download_dir"] = dl.Dir()
		}
		return jsonResult(resp)
	}
}

// fetchDocument performs the HTTP download and returns raw document data.
func fetchDocument(ctx context.Context, client *Client, id int, variant string) (*fetchedDocument, error) {
	var path string
	params := url.Values{}

	switch variant {
	case "original":
		path = fmt.Sprintf("/api/documents/%d/download/", id)
		params.Set("original", "true")
	case "thumbnail":
		path = fmt.Sprintf("/api/documents/%d/thumb/", id)
	default: // archived
		path = fmt.Sprintf("/api/documents/%d/download/", id)
	}

	resp, err := client.GetRaw(ctx, path, params)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		detail := extractErrorDetail(resp)
		return nil, fmt.Errorf("HTTP %d for document %d: %s", resp.StatusCode, id, detail)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	ct := resp.Header.Get("Content-Type")
	if mediaType, _, parseErr := mime.ParseMediaType(ct); parseErr == nil {
		ct = mediaType
	}

	return &fetchedDocument{
		body:        body,
		contentType: ct,
		filename:    filenameFromResponse(resp),
		ext:         extensionFromResponse(resp),
	}, nil
}

// saveDocument writes fetched document data to disk in the downloader's temp directory.
// Creates the directory if it was removed since startup.
func saveDocument(dl *Downloader, doc *fetchedDocument) (string, error) {
	if err := os.MkdirAll(dl.Dir(), 0o700); err != nil {
		return "", fmt.Errorf("ensure download dir: %w", err)
	}

	filename, err := randomFileName(doc.ext)
	if err != nil {
		return "", fmt.Errorf("generate filename: %w", err)
	}

	dest := filepath.Join(dl.Dir(), filename)
	if err := os.WriteFile(dest, doc.body, 0o600); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	dl.TrackFile(dest)
	return dest, nil
}

// extractErrorDetail reads a bounded amount of the response body and tries to
// extract a "detail" field from JSON. Falls back to the raw status text.
func extractErrorDetail(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil || len(body) == 0 {
		return resp.Status
	}
	var detail struct {
		Detail string `json:"detail"`
	}
	if json.Unmarshal(body, &detail) == nil && detail.Detail != "" {
		return detail.Detail
	}
	return resp.Status
}

func handleCleanupDownloads(dl *Downloader) server.ToolHandlerFunc {
	return func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filesStr := request.GetString("files", "")

		if filesStr == "" {
			removed, err := dl.CleanupAll()
			if err != nil {
				return errResult(fmt.Sprintf("cleanup failed: %s", err)), nil
			}
			resp := map[string]any{
				"removed":       removed,
				"removed_count": len(removed),
			}
			return jsonResult(resp)
		}

		var files []string
		if err := json.Unmarshal([]byte(filesStr), &files); err != nil {
			return errResult(fmt.Sprintf("invalid files JSON: %s", err)), nil
		}
		if len(files) == 0 {
			return errResult("files array must not be empty"), nil
		}

		removed, failed, err := dl.CleanupFiles(files)
		if err != nil {
			return errResult(fmt.Sprintf("cleanup failed: %s", err)), nil
		}

		resp := map[string]any{
			"removed":       removed,
			"removed_count": len(removed),
		}
		if len(failed) > 0 {
			resp["failed"] = failed
			resp["failed_count"] = len(failed)
		}
		return jsonResult(resp)
	}
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerPageImageTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("document_page_image",
			mcp.WithDescription("Render a page of a document as an image the model can see. Use this to read documents whose OCR content is missing or garbled (handwriting, bad scans). Supports cropping a region at high resolution to resolve fine detail."),
			withNumber("id", mcp.Description("Document ID"), mcp.Required()),
			withNumber("page", mcp.Description("Page number, 1-indexed (default: 1)")),
			withNumber("max_width", mcp.Description(fmt.Sprintf("Maximum rendered width in pixels, %d-%d (default: 1500)", minPageImageWidth, maxPageImageWidth))),
			mcp.WithString("region", mcp.Description("Optional crop region as JSON [x0,y0,x1,y1] with values as fractions of page width/height (0-1). The region is re-rendered at higher resolution, so use it to zoom into fine detail like handwriting or small print.")),
			mcp.WithBoolean("grayscale", mcp.Description("Render in grayscale, ~3x smaller with no legibility loss on scans (default: true)")),
			mcp.WithString("format", mcp.Description("Output image format: jpeg (default) or png")),
		),
		handleDocumentPageImage(client),
	)
}

func handleDocumentPageImage(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}

		page := int(request.GetFloat("page", 1))
		if page < 1 {
			return errResult(fmt.Sprintf("page must be >= 1, got %d", page)), nil
		}

		maxWidth := int(request.GetFloat("max_width", 1500))
		maxWidth = min(max(maxWidth, minPageImageWidth), maxPageImageWidth)

		var region *[4]float64
		if regionStr := request.GetString("region", ""); regionStr != "" {
			var r [4]float64
			if err := json.Unmarshal([]byte(regionStr), &r); err != nil {
				return errResult(fmt.Sprintf("invalid region JSON: %s (expected [x0,y0,x1,y1])", err)), nil
			}
			if err := validateRegion(r); err != nil {
				return errResult(err.Error()), nil
			}
			region = &r
		}

		grayscale := request.GetBool("grayscale", true)

		format := request.GetString("format", "jpeg")
		switch format {
		case "jpeg", "png":
		default:
			return errResult(fmt.Sprintf("invalid format %q: must be jpeg or png", format)), nil
		}

		body, meta, err := fetchDocument(ctx, client, id, "archived")
		if err != nil {
			return errResult(err.Error()), nil
		}
		defer body.Close()

		limited := io.LimitReader(body, maxInlineSize+1)
		data, err := io.ReadAll(limited)
		if err != nil {
			return errResult(fmt.Sprintf("read document: %s", err)), nil
		}
		if int64(len(data)) > maxInlineSize {
			return errResult(fmt.Sprintf("document exceeds maximum size (%d MiB)", maxInlineSize/(1024*1024))), nil
		}

		var rendered *renderedImage
		switch {
		case meta.contentType == "application/pdf":
			rendered, err = renderPDFPage(renderRequest{
				pdf:       data,
				page:      page,
				maxWidth:  maxWidth,
				region:    region,
				grayscale: grayscale,
				format:    format,
			})
			if err != nil {
				return errResult(fmt.Sprintf("render document %d: %s", id, err)), nil
			}
		case imageMimeTypes[meta.contentType]:
			// The document file is itself an image: treat it as a single page.
			if page != 1 {
				return errResult(fmt.Sprintf("page %d out of range: document %d is a single image", page, id)), nil
			}
			img, decErr := decodeImage(data, meta.contentType)
			if decErr != nil {
				return errResult(fmt.Sprintf("decode document %d image: %s", id, decErr)), nil
			}
			if region != nil {
				img, decErr = cropToRegion(img, *region)
				if decErr != nil {
					return errResult(decErr.Error()), nil
				}
			}
			if img.Bounds().Dx() > maxWidth {
				img = scaleToWidth(img, maxWidth)
			}
			rendered, err = encodeImage(img, format, grayscale)
			if err != nil {
				return errResult(fmt.Sprintf("encode document %d image: %s", id, err)), nil
			}
			rendered.pageCount = 1
		default:
			return errResult(fmt.Sprintf("document %d is %s: only PDF and image documents can be rendered", id, meta.contentType)), nil
		}

		summary := fmt.Sprintf("Document %d, page %d of %d, %dx%d px", id, page, rendered.pageCount, rendered.width, rendered.height)
		if region != nil {
			summary += fmt.Sprintf(", region [%g,%g,%g,%g]", region[0], region[1], region[2], region[3])
		}
		return mcp.NewToolResultImage(summary, rendered.base64Data(), rendered.mimeType), nil
	}
}

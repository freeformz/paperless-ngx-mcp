package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

const (
	// maxImageEdge is the longest edge for any image returned to the model.
	// Larger images get downsampled by the client anyway.
	maxImageEdge = 1568

	// jpegQuality is the encoding quality for JPEG output.
	jpegQuality = 80

	// maxEncodedImageSize is the target ceiling for encoded image bytes returned
	// inline. Images above it are downscaled and re-encoded rather than rejected.
	maxEncodedImageSize = 1536 * 1024

	// minPageImageWidth and maxPageImageWidth bound the max_width parameter.
	minPageImageWidth = 200
	maxPageImageWidth = 2000

	// maxRenderWidth bounds the intermediate full-page render used for region
	// crops, so a tiny region cannot request an unbounded rasterization.
	maxRenderWidth = 6000

	// pdfiumInstanceTimeout is how long to wait for a pdfium worker.
	pdfiumInstanceTimeout = 30 * time.Second
)

// pdfiumPool is initialized lazily on first use: compiling the embedded
// PDFium WebAssembly module takes noticeable time and most sessions never
// render a page.
var pdfiumPoolOnce = sync.OnceValues(func() (pdfium.Pool, error) {
	return webassembly.Init(webassembly.Config{
		MinIdle:  0,
		MaxIdle:  1,
		MaxTotal: 1,
	})
})

// renderRequest describes one page-image render.
type renderRequest struct {
	pdf       []byte
	page      int // 1-indexed
	maxWidth  int
	region    *[4]float64 // normalized [x0,y0,x1,y1] page fractions, nil for full page
	grayscale bool
	format    string // "jpeg" or "png"
}

// renderedImage is the encoded output of a render or normalization.
type renderedImage struct {
	data      []byte
	mimeType  string
	width     int
	height    int
	pageCount int
}

// base64Data returns the image bytes base64-encoded for MCP image content.
func (r *renderedImage) base64Data() string {
	return base64.StdEncoding.EncodeToString(r.data)
}

// renderPDFPage rasterizes one page of a PDF, optionally cropping to a
// normalized region. For region crops the page is re-rendered at a higher
// resolution computed from the region size, so the crop comes back at high
// effective DPI instead of being an upscaled blur.
func renderPDFPage(req renderRequest) (*renderedImage, error) {
	pool, err := pdfiumPoolOnce()
	if err != nil {
		return nil, fmt.Errorf("initialize PDF renderer: %w", err)
	}
	instance, err := pool.GetInstance(pdfiumInstanceTimeout)
	if err != nil {
		return nil, fmt.Errorf("acquire PDF renderer: %w", err)
	}
	defer instance.Close()

	doc, err := instance.OpenDocument(&requests.OpenDocument{File: &req.pdf})
	if err != nil {
		return nil, fmt.Errorf("open PDF: %w", err)
	}
	defer instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})

	countRes, err := instance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return nil, fmt.Errorf("get page count: %w", err)
	}
	pageCount := countRes.PageCount
	if req.page < 1 || req.page > pageCount {
		return nil, fmt.Errorf("page %d out of range: document has %d page(s)", req.page, pageCount)
	}

	sizeRes, err := instance.GetPageSize(&requests.GetPageSize{
		Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: req.page - 1}},
	})
	if err != nil {
		return nil, fmt.Errorf("get page size: %w", err)
	}
	if sizeRes.Width <= 0 || sizeRes.Height <= 0 {
		return nil, fmt.Errorf("invalid page dimensions %gx%g", sizeRes.Width, sizeRes.Height)
	}
	aspect := sizeRes.Height / sizeRes.Width

	// For a region crop, render the page large enough that the cropped area
	// alone spans ~maxWidth native pixels.
	renderWidth := req.maxWidth
	if req.region != nil {
		regionWidth := req.region[2] - req.region[0]
		renderWidth = min(int(float64(req.maxWidth)/regionWidth), maxRenderWidth)
	}
	renderHeight := max(int(float64(renderWidth)*aspect), 1)

	var flags enums.FPDF_RENDER_FLAG
	if req.grayscale {
		flags |= enums.FPDF_RENDER_FLAG_GRAYSCALE
	}

	renderRes, err := instance.RenderPageInPixels(&requests.RenderPageInPixels{
		Page:        requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document, Index: req.page - 1}},
		Width:       renderWidth,
		Height:      renderHeight,
		RenderFlags: flags,
	})
	if err != nil {
		return nil, fmt.Errorf("render page %d: %w", req.page, err)
	}
	defer renderRes.Cleanup()

	var img image.Image = renderRes.Result.Image
	if req.region != nil {
		img, err = cropToRegion(img, *req.region)
		if err != nil {
			return nil, err
		}
	}

	out, err := encodeImage(img, req.format, req.grayscale)
	if err != nil {
		return nil, err
	}
	out.pageCount = pageCount
	return out, nil
}

// parseRegion parses a crop region given as a JSON array "[x0,y0,x1,y1]".
func parseRegion(s string) ([4]float64, error) {
	var r [4]float64
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return r, fmt.Errorf("invalid region %q: expected a JSON array [x0,y0,x1,y1] with values as fractions of page width/height (0-1)", s)
	}
	if err := validateRegion(r); err != nil {
		return r, err
	}
	return r, nil
}

// validateRegion checks a normalized [x0,y0,x1,y1] region.
func validateRegion(r [4]float64) error {
	if r[0] < 0 || r[1] < 0 || r[2] > 1 || r[3] > 1 || r[0] >= r[2] || r[1] >= r[3] {
		var hint string
		if r[2] > 1 || r[3] > 1 {
			hint = " — values look like pixel coordinates, but the region must be fractions of page width/height, e.g. [0,0,0.5,0.5] for the top-left quarter"
		}
		return fmt.Errorf("invalid region [%g,%g,%g,%g]: values must satisfy 0 <= x0 < x1 <= 1 and 0 <= y0 < y1 <= 1%s", r[0], r[1], r[2], r[3], hint)
	}
	return nil
}

// cropToRegion cuts a normalized region out of a rendered image.
func cropToRegion(img image.Image, region [4]float64) (image.Image, error) {
	b := img.Bounds()
	w, h := float64(b.Dx()), float64(b.Dy())
	rect := image.Rect(
		b.Min.X+int(region[0]*w),
		b.Min.Y+int(region[1]*h),
		b.Min.X+int(region[2]*w),
		b.Min.Y+int(region[3]*h),
	)
	if rect.Dx() < 1 || rect.Dy() < 1 {
		return nil, fmt.Errorf("region too small: crops to %dx%d pixels", rect.Dx(), rect.Dy())
	}
	out := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(out, out.Bounds(), img, rect.Min, draw.Src)
	return out, nil
}

// encodeImage applies the size discipline (longest edge cap, encoded byte
// ceiling with downscale-and-retry) and encodes to JPEG or PNG.
func encodeImage(img image.Image, format string, grayscale bool) (*renderedImage, error) {
	if edge := longestEdge(img); edge > maxImageEdge {
		img = scaleToEdge(img, maxImageEdge)
	}

	for {
		data, mimeType, err := encodeOnce(img, format, grayscale)
		if err != nil {
			return nil, err
		}
		if len(data) <= maxEncodedImageSize || longestEdge(img) <= minPageImageWidth {
			b := img.Bounds()
			return &renderedImage{data: data, mimeType: mimeType, width: b.Dx(), height: b.Dy()}, nil
		}
		img = scaleToEdge(img, longestEdge(img)*3/4)
	}
}

func encodeOnce(img image.Image, format string, grayscale bool) ([]byte, string, error) {
	if grayscale {
		img = toGray(img)
	}
	var buf bytes.Buffer
	switch format {
	case "png":
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", fmt.Errorf("encode png: %w", err)
		}
		return buf.Bytes(), "image/png", nil
	default:
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
			return nil, "", fmt.Errorf("encode jpeg: %w", err)
		}
		return buf.Bytes(), "image/jpeg", nil
	}
}

func longestEdge(img image.Image) int {
	b := img.Bounds()
	return max(b.Dx(), b.Dy())
}

// scaleToWidth resizes an image so its width equals w, preserving aspect.
func scaleToWidth(img image.Image, w int) image.Image {
	b := img.Bounds()
	h := max(b.Dy()*w/b.Dx(), 1)
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(out, out.Bounds(), img, b, draw.Src, nil)
	return out
}

// scaleToEdge resizes an image so its longest edge equals edge, preserving aspect.
func scaleToEdge(img image.Image, edge int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w >= h {
		h = max(h*edge/w, 1)
		w = edge
	} else {
		w = max(w*edge/h, 1)
		h = edge
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(out, out.Bounds(), img, b, draw.Src, nil)
	return out
}

// toGray converts an image to 8-bit grayscale, which encodes roughly 3x
// smaller as JPEG with no legibility loss on black-ink-on-white scans.
func toGray(img image.Image) image.Image {
	if _, ok := img.(*image.Gray); ok {
		return img
	}
	b := img.Bounds()
	out := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out
}

// imageMimeTypes are the MIME types MCP hosts render as image content.
var imageMimeTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
	"image/gif":  true,
}

// decodeImage decodes PNG, JPEG, GIF, or WebP bytes.
func decodeImage(data []byte, mimeType string) (image.Image, error) {
	switch mimeType {
	case "image/png":
		return png.Decode(bytes.NewReader(data))
	case "image/jpeg":
		return jpeg.Decode(bytes.NewReader(data))
	case "image/gif":
		return gif.Decode(bytes.NewReader(data))
	case "image/webp":
		return webp.Decode(bytes.NewReader(data))
	default:
		return nil, fmt.Errorf("unsupported image type %q", mimeType)
	}
}

// normalizeInlineImage prepares raw image bytes for inline MCP image content.
// Small images pass through untouched; oversized ones are decoded, downscaled
// to the size discipline, and re-encoded as JPEG.
func normalizeInlineImage(data []byte, mimeType string) (*renderedImage, error) {
	cfg, _, cfgErr := image.DecodeConfig(bytes.NewReader(data))
	if mimeType == "image/webp" {
		if c, err := webp.DecodeConfig(bytes.NewReader(data)); err == nil {
			cfg, cfgErr = c, nil
		}
	}
	if cfgErr == nil &&
		len(data) <= maxEncodedImageSize &&
		max(cfg.Width, cfg.Height) <= maxImageEdge {
		return &renderedImage{data: data, mimeType: mimeType, width: cfg.Width, height: cfg.Height}, nil
	}

	img, err := decodeImage(data, mimeType)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", mimeType, err)
	}
	return encodeImage(img, "jpeg", false)
}

package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/klippa-app/go-pdfium/requests"
	"github.com/mark3labs/mcp-go/mcp"
)

// makeTestPDF builds a PDF with the given number of blank US-letter pages
// using pdfium itself, guaranteeing the fixture is parseable by the renderer.
func makeTestPDF(t *testing.T, pages int) []byte {
	t.Helper()
	pool, err := pdfiumPoolOnce()
	if err != nil {
		t.Fatalf("init pdfium: %s", err)
	}
	instance, err := pool.GetInstance(pdfiumInstanceTimeout)
	if err != nil {
		t.Fatalf("get pdfium instance: %s", err)
	}
	defer instance.Close()

	doc, err := instance.FPDF_CreateNewDocument(&requests.FPDF_CreateNewDocument{})
	if err != nil {
		t.Fatalf("create document: %s", err)
	}
	defer instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})

	for i := range pages {
		if _, err := instance.FPDFPage_New(&requests.FPDFPage_New{
			Document:  doc.Document,
			PageIndex: i,
			Width:     612,
			Height:    792,
		}); err != nil {
			t.Fatalf("create page %d: %s", i, err)
		}
	}

	var buf bytes.Buffer
	if _, err := instance.FPDF_SaveAsCopy(&requests.FPDF_SaveAsCopy{
		Document:   doc.Document,
		FileWriter: &buf,
	}); err != nil {
		t.Fatalf("save document: %s", err)
	}
	return buf.Bytes()
}

// makeTestPNG encodes a solid-color PNG of the given dimensions.
func makeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %s", err)
	}
	return buf.Bytes()
}

// pdfDocumentServer serves pdfData as document id's archived download.
func pdfDocumentServer(t *testing.T, id string, contentType string, data []byte) *Client {
	t.Helper()
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/"+id+"/download/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Write(data)
	})
	return testClientAndServer(t, rh)
}

// resultImage extracts the ImageContent block from a page-image result.
func resultImage(t *testing.T, result *mcp.CallToolResult) mcp.ImageContent {
	t.Helper()
	if len(result.Content) != 2 {
		t.Fatalf("expected 2 content blocks (text + image), got %d", len(result.Content))
	}
	ic, ok := result.Content[1].(mcp.ImageContent)
	if !ok {
		t.Fatalf("expected ImageContent, got %T", result.Content[1])
	}
	return ic
}

// decodeResultImage decodes the base64 image data of an ImageContent block.
func decodeResultImage(t *testing.T, ic mcp.ImageContent) image.Image {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(ic.Data)
	if err != nil {
		t.Fatalf("decode base64: %s", err)
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode image: %s", err)
	}
	wantFormat := strings.TrimPrefix(ic.MIMEType, "image/")
	if format != wantFormat {
		t.Errorf("image format = %s, want %s", format, wantFormat)
	}
	return img
}

func TestDocumentPageImageDefault(t *testing.T) {
	client := pdfDocumentServer(t, "1454", "application/pdf", makeTestPDF(t, 2))

	result := callTool(t, handleDocumentPageImage(client), map[string]any{"id": 1454})
	assertNotError(t, result)

	summary := resultText(t, result)
	if !strings.Contains(summary, "page 1 of 2") {
		t.Errorf("summary %q missing page info", summary)
	}

	ic := resultImage(t, result)
	if ic.MIMEType != "image/jpeg" {
		t.Errorf("mime = %s, want image/jpeg", ic.MIMEType)
	}
	img := decodeResultImage(t, ic)
	b := img.Bounds()
	if edge := max(b.Dx(), b.Dy()); edge > maxImageEdge {
		t.Errorf("longest edge %d exceeds cap %d", edge, maxImageEdge)
	}
	if b.Dx() < minPageImageWidth {
		t.Errorf("width %d below minimum", b.Dx())
	}
}

func TestDocumentPageImageSecondPage(t *testing.T) {
	client := pdfDocumentServer(t, "7", "application/pdf", makeTestPDF(t, 3))

	result := callTool(t, handleDocumentPageImage(client), map[string]any{"id": 7, "page": 2})
	assertNotError(t, result)
	if summary := resultText(t, result); !strings.Contains(summary, "page 2 of 3") {
		t.Errorf("summary %q missing page info", summary)
	}
}

func TestDocumentPageImagePageOutOfRange(t *testing.T) {
	client := pdfDocumentServer(t, "7", "application/pdf", makeTestPDF(t, 2))

	result := callTool(t, handleDocumentPageImage(client), map[string]any{"id": 7, "page": 3})
	assertIsError(t, result)
	if msg := resultText(t, result); !strings.Contains(msg, "2 page(s)") {
		t.Errorf("error %q should name the actual page count", msg)
	}
}

func TestDocumentPageImageRegion(t *testing.T) {
	client := pdfDocumentServer(t, "1454", "application/pdf", makeTestPDF(t, 1))

	result := callTool(t, handleDocumentPageImage(client), map[string]any{
		"id":     1454,
		"region": "[0.25,0.25,0.75,0.75]",
	})
	assertNotError(t, result)

	if summary := resultText(t, result); !strings.Contains(summary, "region [0.25,0.25,0.75,0.75]") {
		t.Errorf("summary %q missing region info", summary)
	}

	img := decodeResultImage(t, resultImage(t, result))
	b := img.Bounds()
	// The 50%-wide region must come back at high effective resolution — near
	// the requested width, not a quarter of a full-page render.
	if b.Dx() < 1000 {
		t.Errorf("region render width %d too small: crop was not re-rendered at higher DPI", b.Dx())
	}
	// A square region of a portrait page renders portrait-shaped due to the
	// page aspect, so height > width.
	if b.Dy() <= b.Dx() {
		t.Errorf("expected portrait crop, got %dx%d", b.Dx(), b.Dy())
	}
}

func TestDocumentPageImageInvalidRegion(t *testing.T) {
	client := NewClient("http://unused", "unused")
	for _, region := range []string{"not json", "0.25,0.25,0.75,0.75", "[0.5,0,0.2,1]", "[-0.1,0,1,1]", "[0,0,1,1.5]", "[0.1,0.2,0.3]"} {
		result := callTool(t, handleDocumentPageImage(client), map[string]any{
			"id":     1,
			"region": region,
		})
		if !result.IsError {
			t.Errorf("region %q: expected error", region)
		}
	}
}

func TestDocumentPageImagePixelRegionHint(t *testing.T) {
	client := NewClient("http://unused", "unused")
	for _, region := range []string{"[0,0,1000,700]", "[2,0,0.5,0.5]"} {
		result := callTool(t, handleDocumentPageImage(client), map[string]any{
			"id":     1,
			"region": region,
		})
		assertIsError(t, result)
		if msg := resultText(t, result); !strings.Contains(msg, "pixel coordinates") {
			t.Errorf("region %q: error %q should hint that pixel coordinates are not accepted", region, msg)
		}
	}
}

func TestDocumentPageImageInvalidFormat(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentPageImage(client), map[string]any{"id": 1, "format": "webp"})
	assertIsError(t, result)
}

func TestDocumentPageImageMissingID(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentPageImage(client), map[string]any{})
	assertIsError(t, result)
}

func TestDocumentPageImagePNGFormat(t *testing.T) {
	client := pdfDocumentServer(t, "9", "application/pdf", makeTestPDF(t, 1))

	result := callTool(t, handleDocumentPageImage(client), map[string]any{"id": 9, "format": "png"})
	assertNotError(t, result)
	ic := resultImage(t, result)
	if ic.MIMEType != "image/png" {
		t.Errorf("mime = %s, want image/png", ic.MIMEType)
	}
	decodeResultImage(t, ic)
}

func TestDocumentPageImageImageDocument(t *testing.T) {
	// A document whose archived file is itself an image is served directly.
	client := pdfDocumentServer(t, "42", "image/png", makeTestPNG(t, 400, 300))

	result := callTool(t, handleDocumentPageImage(client), map[string]any{"id": 42})
	assertNotError(t, result)

	if summary := resultText(t, result); !strings.Contains(summary, "page 1 of 1") {
		t.Errorf("summary %q missing page info", summary)
	}
	img := decodeResultImage(t, resultImage(t, result))
	b := img.Bounds()
	if b.Dx() != 400 || b.Dy() != 300 {
		t.Errorf("dimensions = %dx%d, want 400x300", b.Dx(), b.Dy())
	}
}

func TestDocumentPageImageImageDocumentRespectsMaxWidth(t *testing.T) {
	client := pdfDocumentServer(t, "43", "image/png", makeTestPNG(t, 400, 300))

	result := callTool(t, handleDocumentPageImage(client), map[string]any{"id": 43, "max_width": 300})
	assertNotError(t, result)

	img := decodeResultImage(t, resultImage(t, result))
	b := img.Bounds()
	if b.Dx() != 300 || b.Dy() != 225 {
		t.Errorf("dimensions = %dx%d, want 300x225 (scaled to max_width)", b.Dx(), b.Dy())
	}
}

func TestDocumentPageImageImageDocumentPageOutOfRange(t *testing.T) {
	client := pdfDocumentServer(t, "42", "image/png", makeTestPNG(t, 40, 30))

	result := callTool(t, handleDocumentPageImage(client), map[string]any{"id": 42, "page": 2})
	assertIsError(t, result)
}

func TestDocumentPageImageUnsupportedType(t *testing.T) {
	client := pdfDocumentServer(t, "5", "text/plain", []byte("not renderable"))

	result := callTool(t, handleDocumentPageImage(client), map[string]any{"id": 5})
	assertIsError(t, result)
	if msg := resultText(t, result); !strings.Contains(msg, "text/plain") {
		t.Errorf("error %q should name the content type", msg)
	}
}

func TestDocumentPageImageGrayscaleDefault(t *testing.T) {
	client := pdfDocumentServer(t, "3", "application/pdf", makeTestPDF(t, 1))

	result := callTool(t, handleDocumentPageImage(client), map[string]any{"id": 3})
	assertNotError(t, result)

	ic := resultImage(t, result)
	data, err := base64.StdEncoding.DecodeString(ic.Data)
	if err != nil {
		t.Fatalf("decode base64: %s", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode jpeg: %s", err)
	}
	if _, ok := img.(*image.Gray); !ok {
		t.Errorf("expected grayscale JPEG, decoded as %T", img)
	}
}

//go:build unix

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// runMainEnv makes the test binary run main() instead of the tests, so shutdown
// tests can drive the real `mcp` command as a subprocess over stdio.
const runMainEnv = "PAPERLESS_MCP_TEST_RUN_MAIN"

// processTimeout bounds every wait on the subprocess. Passing runs take
// milliseconds; the timeout only turns a hang into a failure.
const processTimeout = 30 * time.Second

func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// mcpProcess is the `mcp` command running as a subprocess, with its temp dir
// redirected to tmpDir.
type mcpProcess struct {
	cmd    *exec.Cmd
	exited chan struct{} // closed once the process has exited and been reaped
	tmpDir string
	stdin  *os.File             // our end of the process's stdin
	stdout *os.File             // our end of the process's stdout
	msgs   chan json.RawMessage // JSON-RPC messages read from stdout
	stderr bytes.Buffer         // read only after the process has exited
	nextID int
}

// startMCP starts the `mcp` command against paperlessURL and completes the MCP
// initialize handshake, so the server is serving (signal handling installed,
// temp download dir created) when it returns.
func startMCP(t *testing.T, paperlessURL string) *mcpProcess {
	t.Helper()
	p := &mcpProcess{tmpDir: t.TempDir(), msgs: make(chan json.RawMessage, 16), nextID: 1}

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %s", err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %s", err)
	}
	p.stdin, p.stdout = stdinW, stdoutR

	p.cmd = exec.Command(os.Args[0], "mcp")
	p.cmd.Stdin, p.cmd.Stdout, p.cmd.Stderr = stdinR, stdoutW, &p.stderr
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PAPERLESS_") && !strings.HasPrefix(kv, "TMPDIR=") {
			p.cmd.Env = append(p.cmd.Env, kv)
		}
	}
	p.cmd.Env = append(p.cmd.Env, runMainEnv+"=1", "TMPDIR="+p.tmpDir,
		"PAPERLESS_URL="+paperlessURL, "PAPERLESS_TOKEN=test-token")
	if err := p.cmd.Start(); err != nil {
		t.Fatalf("start mcp: %s", err)
	}
	stdinR.Close()
	stdoutW.Close()
	p.exited = make(chan struct{})
	go func() {
		p.cmd.Wait()
		close(p.exited)
	}()
	t.Cleanup(func() {
		p.kill()
		p.stdin.Close()
		p.stdout.Close()
	})

	go func() {
		defer close(p.msgs)
		r := bufio.NewReader(stdoutR)
		for {
			line, err := r.ReadBytes('\n')
			if err != nil {
				return
			}
			p.msgs <- line
		}
	}()

	id := p.request(t, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "shutdown-test", "version": "0"},
	})
	p.awaitResponse(t, id)
	p.send(t, map[string]any{"method": "notifications/initialized"})
	return p
}

// send writes one JSON-RPC message to the process's stdin.
func (p *mcpProcess) send(t *testing.T, msg map[string]any) {
	t.Helper()
	msg["jsonrpc"] = "2.0"
	line, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal %v: %s", msg, err)
	}
	if _, err := p.stdin.Write(append(line, '\n')); err != nil {
		p.fatalf(t, "write to mcp stdin: %s", err)
	}
}

// request sends a JSON-RPC request without waiting for its response and
// returns its id.
func (p *mcpProcess) request(t *testing.T, method string, params any) int {
	t.Helper()
	id := p.nextID
	p.nextID++
	p.send(t, map[string]any{"id": id, "method": method, "params": params})
	return id
}

// awaitResponse reads messages until the response to request id arrives.
func (p *mcpProcess) awaitResponse(t *testing.T, id int) {
	t.Helper()
	timeout := time.After(processTimeout)
	for {
		select {
		case line, ok := <-p.msgs:
			if !ok {
				p.fatalf(t, "mcp stdout closed before the response to request %d", id)
			}
			var msg struct {
				ID json.RawMessage `json:"id"`
			}
			if json.Unmarshal(line, &msg) == nil && string(msg.ID) == strconv.Itoa(id) {
				return
			}
		case <-timeout:
			p.fatalf(t, "no response to request %d within %s", id, processTimeout)
		}
	}
}

// wait waits for the process to exit and returns how it ended.
func (p *mcpProcess) wait(t *testing.T) *os.ProcessState {
	t.Helper()
	select {
	case <-p.exited:
	case <-time.After(processTimeout):
		p.kill()
		t.Fatalf("mcp did not exit within %s\nstderr:\n%s", processTimeout, p.stderr.String())
	}
	return p.cmd.ProcessState
}

// kill kills the process if it is still running and waits until it has exited.
func (p *mcpProcess) kill() {
	select {
	case <-p.exited:
	default:
		p.cmd.Process.Kill()
		<-p.exited
	}
}

// fatalf kills the process if it is still running and fails the test with its
// stderr attached.
func (p *mcpProcess) fatalf(t *testing.T, format string, args ...any) {
	t.Helper()
	p.kill()
	t.Fatalf(format+"\nstderr:\n%s", append(args, p.stderr.String())...)
}

// stopReading leaves the process's stdout unread without closing it, like a
// host that stops reading: once the pipe buffer is full, the process's writes
// block.
func (p *mcpProcess) stopReading(t *testing.T) {
	t.Helper()
	if err := p.stdout.SetReadDeadline(time.Now()); err != nil {
		p.fatalf(t, "stop reading mcp stdout: %s", err)
	}
	for range p.msgs { // the reader goroutine closes msgs when it gives up
	}
}

// tempDownloadDirs lists the per-instance temp download dirs the process has
// created in its temp dir.
func (p *mcpProcess) tempDownloadDirs(t *testing.T) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(p.tmpDir, "paperless-ngx-mcp-*"))
	if err != nil {
		t.Fatalf("glob temp download dirs: %s", err)
	}
	return dirs
}

func TestMCPShutsDownGracefullyOnSignal(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			// Nothing in this test reaches the Paperless API.
			p := startMCP(t, "http://127.0.0.1:9")
			if dirs := p.tempDownloadDirs(t); len(dirs) != 1 {
				p.fatalf(t, "temp download dirs while serving = %v, want exactly one", dirs)
			}

			if err := p.cmd.Process.Signal(sig); err != nil {
				p.fatalf(t, "send %s: %s", sig, err)
			}
			state := p.wait(t)

			// A requested shutdown succeeds, and there is nothing to report.
			if !state.Success() {
				t.Errorf("mcp ended with %s after %s, want exit status 0\nstderr:\n%s", state, sig, p.stderr.String())
			} else if p.stderr.Len() != 0 {
				t.Errorf("mcp wrote to stderr after %s, want nothing:\n%s", sig, p.stderr.String())
			}
			if dirs := p.tempDownloadDirs(t); len(dirs) != 0 {
				t.Errorf("temp download dir left behind after %s: %v", sig, dirs)
			}
		})
	}
}

func TestMCPRemovesTempDirWhenHostExitsMidCall(t *testing.T) {
	requested := make(chan struct{})
	notifyRequested := sync.OnceFunc(func() { close(requested) })
	release := make(chan struct{})
	paperless := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		notifyRequested()
		// Hold the call in flight until mcp cancels the request.
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(paperless.Close)
	t.Cleanup(func() { close(release) })

	p := startMCP(t, paperless.URL)
	p.request(t, "tools/call", map[string]any{
		"name":      "document_download",
		"arguments": map[string]any{"ids": "[1]"},
	})
	select {
	case <-requested:
	case <-time.After(processTimeout):
		p.fatalf(t, "document_download never reached the Paperless API")
	}
	if dirs := p.tempDownloadDirs(t); len(dirs) != 1 {
		p.fatalf(t, "temp download dirs while serving = %v, want exactly one", dirs)
	}

	// The host goes away mid-call. Its end of stdout closes first, so the
	// response to the cancelled call is written to a pipe with no reader; then
	// stdin reaches EOF, which cancels the call.
	p.stdout.Close()
	p.stdin.Close()
	state := p.wait(t)

	if !state.Success() {
		t.Errorf("mcp ended with %s, want exit status 0\nstderr:\n%s", state, p.stderr.String())
	}
	if dirs := p.tempDownloadDirs(t); len(dirs) != 0 {
		t.Errorf("temp download dir left behind: %v", dirs)
	}
}

// heavyPDF builds a one-page PDF whose content stream is rects filled
// rectangles, which PDFium takes about a second per 200,000 to render on an
// M-series Mac and can't be interrupted while doing so.
func heavyPDF(rects int) []byte {
	var content bytes.Buffer
	for i := range rects {
		fmt.Fprintf(&content, "0.%03d g %d %d %d %d re f\n", i%1000, i*37%612, i*53%792, 1+i%40, 1+i*7%40)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", content.Len(), content.Bytes()),
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, obj := range objs {
		offsets[i] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return pdf.Bytes()
}

// servedOnce serves body as a PDF download and closes the returned channel once
// the first response has been written.
func servedOnce(t *testing.T, body []byte) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	served := make(chan struct{})
	notifyServed := sync.OnceFunc(func() { close(served) })
	paperless := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write(body)
		notifyServed()
	}))
	t.Cleanup(paperless.Close)
	return paperless, served
}

func TestMCPExitsPromptlyDuringRender(t *testing.T) {
	pdf := heavyPDF(400_000)
	for _, tc := range []struct {
		name     string
		shutdown func(p *mcpProcess) error
	}{
		{"stdin EOF", func(p *mcpProcess) error { return p.stdin.Close() }},
		{"SIGTERM", func(p *mcpProcess) error { return p.cmd.Process.Signal(syscall.SIGTERM) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paperless, served := servedOnce(t, pdf)
			p := startMCP(t, paperless.URL)
			p.request(t, "tools/call", map[string]any{
				"name":      "document_page_image",
				"arguments": map[string]any{"id": 3, "max_width": 2000},
			})
			select {
			case <-served:
			case <-time.After(processTimeout):
				p.fatalf(t, "document_page_image never fetched the document")
			}
			// Let the handler finish reading the document, so shutdown starts
			// while PDFium initializes or renders, neither of which can be
			// interrupted, rather than during the fetch, which always could be.
			time.Sleep(200 * time.Millisecond)

			start := time.Now()
			if err := tc.shutdown(p); err != nil {
				p.fatalf(t, "start shutdown: %s", err)
			}
			state := p.wait(t)

			// MCP hosts SIGKILL a server that is still running 2s after they
			// close its stdin. Waiting for the render would take seconds.
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Errorf("mcp exited %s after shutdown started, want under 2s", elapsed)
			}
			if !state.Success() {
				t.Errorf("mcp ended with %s, want exit status 0\nstderr:\n%s", state, p.stderr.String())
			}
			if dirs := p.tempDownloadDirs(t); len(dirs) != 0 {
				t.Errorf("temp download dir left behind: %v", dirs)
			}
		})
	}
}

func TestMCPSecondSignalEndsStalledShutdown(t *testing.T) {
	paperless, served := servedOnce(t, bytes.Repeat([]byte("x"), 1<<20))
	p := startMCP(t, paperless.URL)
	// The host stops reading, so writing the ~1.4 MB response below blocks once
	// the pipe is full, and shutdown can't finish.
	p.stopReading(t)
	p.request(t, "tools/call", map[string]any{
		"name":      "document_download",
		"arguments": map[string]any{"ids": "[1]", "content": true},
	})
	select {
	case <-served:
	case <-time.After(processTimeout):
		p.fatalf(t, "document_download never fetched the document")
	}
	// Let the handler finish reading the document and start writing the response.
	time.Sleep(200 * time.Millisecond)

	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		p.fatalf(t, "send SIGTERM: %s", err)
	}
	select {
	case <-p.exited:
		t.Fatalf("mcp exited (%s) on the first signal; this test needs a stalled shutdown\nstderr:\n%s", p.cmd.ProcessState, p.stderr.String())
	case <-time.After(500 * time.Millisecond):
	}

	if err := p.cmd.Process.Signal(syscall.SIGINT); err != nil {
		p.fatalf(t, "send SIGINT: %s", err)
	}
	state := p.wait(t)
	if status, ok := state.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != syscall.SIGINT {
		t.Errorf("mcp ended with %s after a second signal, want killed by SIGINT", state)
	}
}

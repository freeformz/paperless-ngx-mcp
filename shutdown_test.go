//go:build unix

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
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
	t.Cleanup(func() {
		if p.cmd.ProcessState == nil {
			p.cmd.Process.Kill()
			p.cmd.Wait()
		}
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
	done := make(chan struct{})
	go func() {
		p.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(processTimeout):
		p.cmd.Process.Kill()
		<-done
		t.Fatalf("mcp did not exit within %s\nstderr:\n%s", processTimeout, p.stderr.String())
	}
	return p.cmd.ProcessState
}

// fatalf kills the process if it is still running and fails the test with its
// stderr attached.
func (p *mcpProcess) fatalf(t *testing.T, format string, args ...any) {
	t.Helper()
	if p.cmd.ProcessState == nil {
		p.cmd.Process.Kill()
		p.cmd.Wait()
	}
	t.Fatalf(format+"\nstderr:\n%s", append(args, p.stderr.String())...)
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

func TestMCPRemovesTempDirOnSignal(t *testing.T) {
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

			if !state.Exited() {
				t.Errorf("mcp was killed (%s) instead of shutting down\nstderr:\n%s", state, p.stderr.String())
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

	if !state.Exited() {
		t.Errorf("mcp was killed (%s) instead of shutting down\nstderr:\n%s", state, p.stderr.String())
	}
	if dirs := p.tempDownloadDirs(t); len(dirs) != 0 {
		t.Errorf("temp download dir left behind: %v", dirs)
	}
}

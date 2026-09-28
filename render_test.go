package main

import (
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"
)

// captureLog redirects the standard logger, where abandonOnDone reports
// panics, for the rest of the test and returns what gets logged.
func captureLog(t *testing.T) <-chan string {
	t.Helper()
	logged := make(chan string, 16)
	prev := log.Writer()
	log.SetOutput(writerFunc(func(p []byte) (int, error) {
		select {
		case logged <- string(p):
		default:
		}
		return len(p), nil
	}))
	t.Cleanup(func() { log.SetOutput(prev) })
	return logged
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestRenderPDFPageStopsWaitingForRendererWhenCancelled(t *testing.T) {
	pdf := makeTestPDF(t, 1)
	pool, err := pdfiumPoolOnce()
	if err != nil {
		t.Fatalf("init pdfium: %s", err)
	}
	// Hold the pool's only instance, as a render in progress would.
	held, err := pool.GetInstance(pdfiumInstanceTimeout)
	if err != nil {
		t.Fatalf("get pdfium instance: %s", err)
	}
	defer held.Close()

	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err = renderPDFPage(ctx, renderRequest{pdf: pdf, page: 1, maxWidth: 500, grayscale: true, format: "jpeg"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("renderPDFPage error = %v, want %v", err, context.Canceled)
	}
	// Ignoring the cancellation would wait out pdfiumInstanceTimeout (30s).
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("renderPDFPage returned %s after the call, want soon after the cancellation at 100ms", elapsed)
	}
}

func TestAbandonOnDoneReturnsWhenCancelled(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(50*time.Millisecond, cancel)

	done := make(chan error, 1)
	go func() {
		_, err := abandonOnDone(ctx, func() (int, error) {
			<-release // work that ignores ctx, like a PDFium call
			return 1, nil
		})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("abandonOnDone error = %v, want %v", err, context.Canceled)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("abandonOnDone still waiting for fn 5s after its context was cancelled")
	}
}

func TestAbandonOnDoneRepanicsInCaller(t *testing.T) {
	captureLog(t)
	defer func() {
		if p := recover(); p != "boom" {
			t.Errorf("recovered %v, want fn's panic", p)
		}
	}()
	abandonOnDone(t.Context(), func() (int, error) { panic("boom") })
	t.Error("abandonOnDone returned instead of re-raising fn's panic")
}

func TestAbandonOnDoneLogsPanicAfterAbandonment(t *testing.T) {
	logged := captureLog(t)
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(50*time.Millisecond, cancel)
	_, err := abandonOnDone(ctx, func() (int, error) {
		<-release
		panic("boom")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("abandonOnDone error = %v, want %v", err, context.Canceled)
	}

	// The work panics after its caller has stopped waiting. That must not
	// crash the process, and the panic must still be reported, with the stack
	// of the code that panicked.
	close(release)
	select {
	case msg := <-logged:
		if !strings.Contains(msg, "boom") || !strings.Contains(msg, "TestAbandonOnDoneLogsPanicAfterAbandonment") {
			t.Errorf("logged %q, want the panic value and the stack where it happened", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("panic in abandoned work was not logged within 5s")
	}
}

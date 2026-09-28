package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

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
	defer func() {
		if p := recover(); p != "boom" {
			t.Errorf("recovered %v, want fn's panic", p)
		}
	}()
	abandonOnDone(t.Context(), func() (int, error) { panic("boom") })
	t.Error("abandonOnDone returned instead of re-raising fn's panic")
}

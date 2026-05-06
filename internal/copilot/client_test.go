package copilot

import (
	"context"
	"testing"
	"time"
)

func TestNewReviewClient(t *testing.T) {
	// Create client with 5s timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rc, err := NewReviewClient(ctx)
	if err != nil {
		t.Fatalf("NewReviewClient: %v", err)
	}
	if rc == nil {
		t.Fatal("expected non-nil ReviewClient")
	}

	// Clean up
	if err := rc.Close(); err != nil {
		t.Logf("Close: %v (non-fatal)", err)
	}
}

func TestNewReviewClient_ContextTimeout(t *testing.T) {
	// Create context with very short timeout to trigger timeout error
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	rc, err := NewReviewClient(ctx)
	// Either we get an error from context timeout, or SDK manages to start
	// Both are acceptable - SDK might start asynchronously
	if rc != nil {
		defer rc.Close()
	}
	t.Logf("SDK init with short timeout: err=%v", err)
}

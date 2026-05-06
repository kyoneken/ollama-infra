package copilot

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestReview_SendsDiffAsPrompt(t *testing.T) {
	// This test verifies Review sends diff and collects response
	// Full integration test (requires Copilot SDK + CLI running)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rc, err := NewReviewClient(ctx)
	if err != nil {
		t.Skipf("SDK init failed: %v (skipping integration test)", err)
	}
	defer rc.Close()

	diff := `--- a/main.go
+++ b/main.go
@@ -1,3 +1,5 @@
+// TODO: improve this
 package main
 
 func main() {
`

	// Pass empty skillDirs for now (no skill directories configured)
	review, err := rc.Review(ctx, diff, "gpt-5", []string{})
	if err != nil {
		t.Skipf("Review failed: %v (skipping — Ollama may be unavailable)", err)
	}

	if strings.TrimSpace(review) == "" {
		t.Fatal("expected non-empty review response")
	}

	// Basic sanity check: review should be somewhat reasonable
	if len(review) < 10 {
		t.Fatalf("review too short: %q", review)
	}
}

func TestReview_EmptyDiff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rc, err := NewReviewClient(ctx)
	if err != nil {
		t.Skipf("SDK init failed: %v (skipping)", err)
	}
	defer rc.Close()

	// Empty diff should still result in some response (or clear error)
	review, err := rc.Review(ctx, "", "gpt-5", []string{})

	// Either succeeds with response, or fails with error — both acceptable
	if err == nil && strings.TrimSpace(review) == "" {
		t.Fatal("empty diff: expected either response or error, got neither")
	}
}

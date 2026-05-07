package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kyoneken/ollama-infra/internal/diff"
)

// systemPrompt is copied verbatim from entrypoint.sh — do not modify.
const systemPrompt = `You are a strict code reviewer. Check for ALL of the following:
1. TYPOS: misspelled identifiers, strings, comments (e.g. Mulitply->Multiply, CountVowles->CountVowels)
2. LOGIC: off-by-one, missing null/zero checks, wrong operators (- instead of +), unchecked errors
3. COMMENT: docstring/comment says one thing but code does another

Diff lines are annotated with file line numbers like this:
  "[  12]+	return a - b"  => line 12 was ADDED (+ means new line)
  "[  10] 	func Add(...)"  => line 10 is context
  "      -	return a + b"  => deleted line (no line number)
Use the integer inside [] as LINE. Do not copy the brackets.

For each issue output exactly one line in this format:
FILE|LINE|SEVERITY|ISSUE|FIX|REASON_JA
- LINE: integer from [] annotation (e.g. 12, not "[  12]")
- SEVERITY: ERROR, WARNING, or INFO
- FIX: the corrected code snippet only (no line numbers)
- REASON_JA: one Japanese sentence explaining why this must be fixed
Output ONLY these pipe-separated lines, nothing else.`

const (
	maxDiffBytes = 4000
)

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[reviewer] "+format+"\n", args...)
}

// runCopilotReview executes the copilot review command with the given diff and model.
// Returns the review response or a fatal error if Copilot CLI is unavailable or fails.
func runCopilotReview(diffText string, model string) (string, error) {
	const copilotTimeout = 30 * time.Second

	logf("Running Copilot CLI review (timeout 30s)...")

	ctx, cancel := context.WithTimeout(context.Background(), copilotTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "copilot", "review", "--diff", diffText, "--model", model)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("copilot CLI failed: %w (stderr: %s)", err, stderr.String())
	}

	reviewText := stdout.String()
	if strings.TrimSpace(reviewText) == "" {
		return "", fmt.Errorf("copilot CLI returned empty response")
	}

	logf("Copilot CLI review successful")
	return reviewText, nil
}

func main() {
	model := getenv("COPILOT_MODEL", "qwen2.5-coder:1.5b")
	outputPath := getenv("REVIEW_OUTPUT", "/tmp/review.txt")

	diffText, err := readDiff()
	if err != nil {
		log.Fatalf("read diff: %v", err)
	}

	annotated := diff.Annotate(diffText)
	truncated := diff.Truncate(annotated, maxDiffBytes)
	logf("Diff: %d bytes (annotated), truncated to %d bytes", len(annotated), len(truncated))

	// Run Copilot CLI review — no fallback, CI fails if this fails
	reviewText, err := runCopilotReview(truncated, model)
	if err != nil {
		log.Fatalf("review failed — CI halted: %v", err)
	}

	if err := os.WriteFile(outputPath, []byte(reviewText+"\n"), 0644); err != nil {
		log.Fatalf("write output: %v", err)
	}
	logf("Review written to %s", outputPath)

	fmt.Println()
	fmt.Println("========== CODE REVIEW ==========")
	fmt.Println(reviewText)
	fmt.Println("=================================")
}

// readDiff returns the diff text. Priority: /workspace/pr.diff file (non-empty), then PR_DIFF env var.
func readDiff() (string, error) {
	const diffFile = "/workspace/pr.diff"
	if _, err := os.Stat(diffFile); err == nil {
		logf("Using %s...", diffFile)
		b, err := os.ReadFile(diffFile)
		if err != nil {
			return "", err
		}
		if len(b) > 0 {
			return string(b), nil
		}
		// File exists but is empty — fall through to PR_DIFF.
	}
	if v := os.Getenv("PR_DIFF"); v != "" {
		logf("Using PR_DIFF env var...")
		return v, nil
	}
	return "", fmt.Errorf("no diff found: provide %s or set PR_DIFF env var", diffFile)
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

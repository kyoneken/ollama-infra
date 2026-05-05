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
	"github.com/kyoneken/ollama-infra/internal/ollama"
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
	ollamaURL     = "http://localhost:11434"
	maxDiffBytes  = 4000
	ollamaTimeout = 60 * time.Second
	reviewTimeout = 480 * time.Second
)

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[reviewer] "+format+"\n", args...)
}

// runCopilotReview executes the copilot review command with the given diff and model.
// Returns the review response or an error if copilot is unavailable or the command fails.
func runCopilotReview(diff string, model string) (string, error) {
	const copilotTimeout = 30 * time.Second

	logf("Trying Copilot CLI (timeout 30s)...")

	ctx, cancel := context.WithTimeout(context.Background(), copilotTimeout)
	defer cancel()

	// Build copilot review command with diff and model arguments
	cmd := exec.CommandContext(ctx, "copilot", "review", "--diff", diff, "--model", model)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// Log copilot failure at info level (it's optional)
		logf("Copilot CLI unavailable or failed: %v (stderr: %s) — falling back to Ollama", err, stderr.String())
		return "", err
	}

	reviewText := stdout.String()
	if strings.TrimSpace(reviewText) == "" {
		logf("Copilot CLI returned empty response — falling back to Ollama")
		return "", fmt.Errorf("empty copilot response")
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

	prompt := systemPrompt + "\n\nDIFF:\n" + truncated + "\n\nREVIEW:"

	var reviewText string

	// Try Copilot CLI first
	if copilotResponse, err := runCopilotReview(truncated, model); err == nil {
		reviewText = copilotResponse
		logf("Using Copilot CLI for review")
	} else {
		// Fall back to Ollama client
		logf("Falling back to Ollama for review...")

		client := ollama.NewClient(ollamaURL)

		logf("Starting ollama serve...")
		if err := client.Start(); err != nil {
			log.Fatalf("failed to start ollama: %v", err)
		}
		defer client.Stop()

		logf("Waiting for Ollama to be ready (timeout 60s)...")
		ctx := context.Background()
		if err := client.WaitReady(ctx, ollamaTimeout); err != nil {
			log.Fatalf("ollama not ready: %v", err)
		}
		logf("Ollama is ready.")

		logf("Verifying model %s is present...", model)
		if err := client.EnsureModel(ctx, model); err != nil {
			logf("Warning: EnsureModel failed (%v) — continuing with pre-baked model", err)
		}

		logf("Running code review (stream:true, num_predict:500, timeout 480s)...")
		reviewCtx, cancel := context.WithTimeout(ctx, reviewTimeout)
		defer cancel()

		reviewText, err = client.Generate(reviewCtx, model, prompt, ollama.GenerateOptions{
			NumCtx:      2048,
			NumPredict:  500,
			Temperature: 0.1,
		})
		if err != nil {
			logf("Generate error: %v — using fallback message", err)
			reviewText = ""
		}
		logf("Using Ollama for review")
	}

	if strings.TrimSpace(reviewText) == "" {
		reviewText = "No review output generated (model may be too slow for CPU inference)."
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

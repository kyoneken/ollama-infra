package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/kyoneken/ollama-infra/internal/copilot"
	"github.com/kyoneken/ollama-infra/internal/diff"
	"github.com/kyoneken/ollama-infra/internal/ollama"
)

// systemPrompt is copied verbatim from entrypoint.sh — do not modify.
// NOTE: When using Copilot SDK with SkillDirectories, the system prompt is minimal.
// Detailed review logic is delegated to skills loaded from SkillDirectories.
// This reduces maintenance overhead and enables skills-based extensibility.
const systemPrompt = `Review this code diff and report any issues found.
Format: FILE|LINE|SEVERITY|ISSUE|FIX|REASON_JA
Output only these pipe-separated lines, nothing else.`

const (
	ollamaURL     = "http://localhost:11434"
	maxDiffBytes  = 4000
	ollamaTimeout = 60 * time.Second
	reviewTimeout = 480 * time.Second
)

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[reviewer] "+format+"\n", args...)
}

// reviewWithSDK executes the copilot SDK review with the given diff and model.
// Returns the review response or an error if SDK is unavailable or the request fails.
// skillDirs specifies directories to load Copilot skills from.
func reviewWithSDK(diff string, model string, skillDirs []string) (string, error) {
	const sdkTimeout = 30 * time.Second

	logf("Trying Copilot SDK (timeout 30s, skills enabled)...")

	// Verify Ollama is running before attempting SDK initialization
	logf("Verifying Ollama endpoint is ready...")
	client := ollama.NewClient(ollamaURL)
	ctx := context.Background()
	if err := client.WaitReady(ctx, 5*time.Second); err != nil {
		logf("Warning: Ollama not immediately ready: %v (attempting SDK anyway...)", err)
	} else {
		logf("✓ Ollama verified ready")
	}

	ctx, cancel := context.WithTimeout(context.Background(), sdkTimeout)
	defer cancel()

	reviewClient, err := copilot.NewReviewClient(ctx)
	if err != nil {
		logf("Copilot SDK initialization failed: %v — falling back to Ollama", err)
		return "", err
	}
	defer reviewClient.Close()

	reviewText, err := reviewClient.Review(ctx, diff, model, skillDirs)
	if err != nil {
		logf("Copilot SDK review failed: %v — falling back to Ollama", err)
		return "", err
	}

	if strings.TrimSpace(reviewText) == "" {
		logf("Copilot SDK returned empty response — falling back to Ollama")
		return "", fmt.Errorf("empty SDK response")
	}

	logf("Copilot SDK review successful (skills-based review)")
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

	// Try Copilot SDK first (skills-based review)
	// SkillDirectories can be configured via env var or passed as []string{}
	skillDirs := []string{} // Empty for now; can be set from env or config
	if sdkResponse, err := reviewWithSDK(truncated, model, skillDirs); err == nil {
		reviewText = sdkResponse
		logf("Using Copilot SDK for review")
	} else {
		// Fall back to Ollama client (traditional system-prompt based review)

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

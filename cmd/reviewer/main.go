package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/kyoneken/ollama-infra/internal/diff"
	"github.com/kyoneken/ollama-infra/internal/ollama"
	"github.com/kyoneken/ollama-infra/internal/proxy"
)

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

// copilot CLI footer patterns to strip from output.
var copilotFooter = regexp.MustCompile(`^(Changes\s+[+-]\d+|Duration\s+|Tokens\s+[↑↓])`)

const (
	maxDiffBytes  = 4000
	ollamaURL     = "http://localhost:11434"
	ollamaTimeout = 60 * time.Second
	reviewTimeout = 480 * time.Second
)

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[reviewer] "+format+"\n", args...)
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

	client := ollama.NewClient(ollamaURL)

	logf("Starting ollama serve...")
	if err := client.Start(); err != nil {
		log.Fatalf("start ollama: %v", err)
	}
	defer client.Stop()

	logf("Waiting for Ollama to be ready (timeout %s)...", ollamaTimeout)
	ctx := context.Background()
	if err := client.WaitReady(ctx, ollamaTimeout); err != nil {
		log.Fatalf("ollama not ready: %v", err)
	}
	logf("Ollama is ready.")

	logf("Verifying model %s is present...", model)
	if err := client.EnsureModel(ctx, model); err != nil {
		log.Fatalf("ensure model %s: %v", model, err)
	}

	// Start proxy that strips tool definitions so the model returns plain text.
	prx := proxy.New(ollamaURL)
	port, err := prx.Start()
	if err != nil {
		log.Fatalf("start proxy: %v", err)
	}
	defer prx.Close()
	proxyURL := fmt.Sprintf("http://localhost:%d/v1", port)
	logf("Tool-stripping proxy listening on %s → %s", proxyURL, ollamaURL)

	prompt := systemPrompt + "\n\n" + truncated

	logf("Running copilot CLI review (model: %s, timeout: %s)...", model, reviewTimeout)
	reviewCtx, cancel := context.WithTimeout(ctx, reviewTimeout)
	defer cancel()

	reviewText, err := runCopilot(reviewCtx, prompt, model, proxyURL)
	if err != nil {
		log.Fatalf("review failed — CI halted: %v", err)
	}

	reviewText = strings.TrimSpace(reviewText)

	if err := os.WriteFile(outputPath, []byte(reviewText+"\n"), 0644); err != nil {
		log.Fatalf("write output: %v", err)
	}
	logf("Review written to %s", outputPath)

	fmt.Println()
	fmt.Println("========== CODE REVIEW ==========")
	fmt.Println(reviewText)
	fmt.Println("=================================")
}

// runCopilot executes the copilot CLI in non-interactive mode and returns
// the plain-text review output with copilot's footer lines stripped.
func runCopilot(ctx context.Context, prompt, model, providerBaseURL string) (string, error) {
	cmd := exec.CommandContext(ctx, "copilot",
		"--no-color",
		"--allow-all",
		"--output-format", "text",
		"-p", prompt,
	)
	cmd.Env = append(os.Environ(),
		"COPILOT_PROVIDER_BASE_URL="+providerBaseURL,
		"COPILOT_MODEL="+model,
		"COPILOT_OFFLINE=true",
		"COPILOT_ALLOW_ALL=true",
	)

	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("copilot exited %d: %s", ee.ExitCode(), bytes.TrimSpace(ee.Stderr))
		}
		return "", fmt.Errorf("run copilot: %w", err)
	}

	return stripCopilotFooter(string(out)), nil
}

// stripCopilotFooter removes the summary lines copilot appends after the response
// (e.g. "Changes +0 -0", "Duration 2m 6s", "Tokens ↑ 4.1k • ↓ 25").
func stripCopilotFooter(s string) string {
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		line := scanner.Text()
		if !copilotFooter.MatchString(line) {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
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

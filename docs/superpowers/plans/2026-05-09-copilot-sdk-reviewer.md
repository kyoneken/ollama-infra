# Copilot SDK Reviewer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** GoのCopilot SDKを使って `exec.Command("copilot", ...)` を置き換え、`Reviewer` インターフェースで SDK/CLI を切り替え可能にする。

**Architecture:** `internal/reviewer` パッケージに `Reviewer` インターフェースを定義し、`SDKReviewer`（copilot-sdk/go 使用）と `CLIReviewer`（既存 exec.Command 方式）の2実装を持つ。`REVIEWER_BACKEND=sdk|cli` で切り替え。`cmd/reviewer/main.go` は Ollama ライフサイクルのみ管理し Reviewer 実装の詳細を持たない。

**Tech Stack:** Go 1.24, github.com/github/copilot-sdk/go, copilot CLI v1.0.43+, Ollama

---

## File Map

| File | 操作 | 責務 |
|------|------|------|
| `internal/reviewer/reviewer.go` | 新規作成 | `Reviewer` インターフェース + `systemPrompt` 定数 + `New()` ファクトリ |
| `internal/reviewer/cli.go` | 新規作成 | `CLIReviewer`: proxy起動 + exec.Command 実装（main.go から移動） |
| `internal/reviewer/cli_test.go` | 新規作成 | `stripCopilotFooter` のユニットテスト |
| `internal/reviewer/sdk.go` | 新規作成 | `SDKReviewer`: copilot-sdk/go を使った実装 |
| `internal/reviewer/sdk_test.go` | 新規作成 | SDKReviewer の統合テスト（`-short` でスキップ） |
| `cmd/reviewer/main.go` | 変更 | Ollama 管理のみ、`reviewer.New()` を呼び出すだけ |
| `go.mod` / `go.sum` | 変更 | `github.com/github/copilot-sdk/go` 追加 |

---

### Task 1: ワークツリーのセットアップ

**Files:**
- Create: `.worktrees/feat-copilot-sdk/` (worktree)

- [ ] **Step 1: ワークツリーを作成する**

```bash
cd /Volumes/T7/.ghq/github.com/kyoneken/ollama-infra
git worktree add .worktrees/feat-copilot-sdk -b feat/copilot-sdk
cd .worktrees/feat-copilot-sdk
go mod download
```

- [ ] **Step 2: 既存テストがパスすることを確認する**

```bash
go test ./...
```

Expected output（全パス）:
```
ok  	github.com/kyoneken/ollama-infra/internal/diff
ok  	github.com/kyoneken/ollama-infra/internal/ollama
ok  	github.com/kyoneken/ollama-infra/internal/proxy
```

---

### Task 2: SDK 依存関係の追加と API 確認

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: SDK パッケージをインストールする**

```bash
cd .worktrees/feat-copilot-sdk
go get github.com/github/copilot-sdk/go
```

- [ ] **Step 2: SDK の型を確認する**

```bash
go doc github.com/github/copilot-sdk/go SessionConfig | head -40
go doc github.com/github/copilot-sdk/go MessageOptions
```

`SessionConfig.SystemMessage` と `SessionConfig.AvailableTools` フィールドの型を確認すること。  
結果に応じて Task 5 のコードを調整する。

- [ ] **Step 3: ビルドが通ることを確認する**

```bash
go build ./...
```

---

### Task 3: Reviewer インターフェースとファクトリを作成する

**Files:**
- Create: `internal/reviewer/reviewer.go`

- [ ] **Step 1: インターフェースファイルを作成する**

`internal/reviewer/reviewer.go` を以下の内容で作成する:

```go
// Package reviewer provides LLM-based code review implementations.
// Use New() to select the backend via REVIEWER_BACKEND env var ("sdk" or "cli").
package reviewer

import (
	"context"
	"fmt"
)

// Reviewer performs LLM-based code review on a diff.
type Reviewer interface {
	// Review sends the annotated diff to the LLM and returns pipe-separated review lines.
	Review(ctx context.Context, diff string) (string, error)
	// Close releases resources (SDK client, proxy server, etc.).
	Close() error
}

// SystemPrompt is the code review instruction sent to the LLM.
const SystemPrompt = `You are a strict code reviewer. Check for ALL of the following:
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

// New returns a Reviewer based on the backend name.
// backend: "sdk" (default) or "cli"
// model: Ollama model name (e.g. "qwen2.5-coder:1.5b")
// ollamaURL: Ollama base URL used by CLIReviewer proxy (e.g. "http://localhost:11434")
func New(backend, model, ollamaURL string) (Reviewer, error) {
	switch backend {
	case "sdk":
		return NewSDKReviewer(model)
	case "cli":
		return NewCLIReviewer(model, ollamaURL)
	default:
		return nil, fmt.Errorf("unknown reviewer backend %q (use \"sdk\" or \"cli\")", backend)
	}
}
```

- [ ] **Step 2: ビルドが通ることを確認する**

```bash
go build ./internal/reviewer/
```

Expected: コンパイルエラーなし（NewSDKReviewer/NewCLIReviewer は未定義だが、パッケージ内で解決予定）

---

### Task 4: CLIReviewer を作成する（既存ロジックを移動）

**Files:**
- Create: `internal/reviewer/cli.go`
- Create: `internal/reviewer/cli_test.go`

- [ ] **Step 1: テストファイルを作成する**

`internal/reviewer/cli_test.go` を作成する:

```go
package reviewer

import (
	"testing"
)

func TestStripCopilotFooter(t *testing.T) {
	input := "main.go|5|ERROR|wrong op|return a + b|演算子が誤り\n" +
		"Changes  +0 -0\n" +
		"Duration  2m 6s\n" +
		"Tokens  ↑ 4.1k • ↓ 25 • 0 (cached)\n"

	got := stripCopilotFooter(input)
	want := "main.go|5|ERROR|wrong op|return a + b|演算子が誤り"

	if got != want {
		t.Errorf("stripCopilotFooter:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestStripCopilotFooter_NoFooter(t *testing.T) {
	input := "main.go|5|ERROR|wrong op|return a + b|演算子が誤り"
	got := stripCopilotFooter(input)
	if got != input {
		t.Errorf("stripCopilotFooter with no footer: got %q, want %q", got, input)
	}
}
```

- [ ] **Step 2: テストが失敗することを確認する**

```bash
go test ./internal/reviewer/ -run TestStrip -v
```

Expected: `undefined: stripCopilotFooter`

- [ ] **Step 3: CLIReviewer 実装ファイルを作成する**

`internal/reviewer/cli.go` を作成する:

```go
package reviewer

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/kyoneken/ollama-infra/internal/proxy"
)

// copilot CLI footer patterns to strip from output.
var copilotFooter = regexp.MustCompile(`^(Changes\s+[+-]\d+|Duration\s+|Tokens\s+[↑↓])`)

// CLIReviewer runs the copilot CLI via exec.Command with a tool-stripping proxy.
type CLIReviewer struct {
	proxy    *proxy.Server
	proxyURL string
	model    string
}

// NewCLIReviewer starts a tool-stripping proxy pointing to ollamaURL and
// returns a CLIReviewer ready to use.
func NewCLIReviewer(model, ollamaURL string) (*CLIReviewer, error) {
	prx := proxy.New(ollamaURL)
	port, err := prx.Start()
	if err != nil {
		return nil, fmt.Errorf("start proxy: %w", err)
	}
	proxyURL := fmt.Sprintf("http://localhost:%d/v1", port)
	return &CLIReviewer{proxy: prx, proxyURL: proxyURL, model: model}, nil
}

// Review runs copilot CLI with the system prompt prepended to diff.
func (r *CLIReviewer) Review(ctx context.Context, diff string) (string, error) {
	prompt := SystemPrompt + "\n\n" + diff
	cmd := exec.CommandContext(ctx, "copilot",
		"--no-color",
		"--allow-all",
		"--output-format", "text",
		"-p", prompt,
	)
	cmd.Env = append(os.Environ(),
		"COPILOT_PROVIDER_BASE_URL="+r.proxyURL,
		"COPILOT_MODEL="+r.model,
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

// Close shuts down the proxy server.
func (r *CLIReviewer) Close() error {
	r.proxy.Close()
	return nil
}

// stripCopilotFooter removes summary lines copilot appends after the response.
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
```

- [ ] **Step 4: テストがパスすることを確認する**

```bash
go test ./internal/reviewer/ -run TestStrip -v
```

Expected:
```
--- PASS: TestStripCopilotFooter (0.00s)
--- PASS: TestStripCopilotFooter_NoFooter (0.00s)
PASS
```

- [ ] **Step 5: コミットする**

```bash
git add internal/reviewer/
git commit -m "feat: add CLIReviewer to internal/reviewer package"
```

---

### Task 5: SDKReviewer を作成する

**Files:**
- Create: `internal/reviewer/sdk.go`
- Create: `internal/reviewer/sdk_test.go`

- [ ] **Step 1: テストファイルを作成する（統合テスト、-short でスキップ）**

`internal/reviewer/sdk_test.go` を作成する:

```go
package reviewer

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestSDKReviewerIntegration requires copilot CLI + Ollama running.
// Run with: go test ./internal/reviewer/ -run TestSDKReviewer -v -timeout 5m
// Skip in short mode: go test -short ./...
func TestSDKReviewerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SDK integration test in short mode")
	}

	r, err := NewSDKReviewer("qwen2.5-coder:1.5b")
	if err != nil {
		t.Fatalf("NewSDKReviewer: %v", err)
	}
	defer r.Close()

	diff := `--- a/math.go
+++ b/math.go
[   5]+	return a - b`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	result, err := r.Review(ctx, diff)
	if err != nil {
		t.Fatalf("Review: %v", err)
	}

	if result == "" {
		t.Fatal("expected non-empty review result")
	}

	// Should produce pipe-separated output, not tool-call JSON.
	if strings.Contains(result, `"name":`) || strings.Contains(result, `"arguments":`) {
		t.Errorf("review looks like tool-call JSON, not plain text:\n%s", result)
	}

	t.Logf("Review output:\n%s", result)
}
```

- [ ] **Step 2: テストが `skip` になることを確認する**

```bash
go test ./internal/reviewer/ -run TestSDKReviewer -short -v
```

Expected: `--- SKIP: TestSDKReviewerIntegration`

- [ ] **Step 3: SDKReviewer 実装ファイルを作成する**

`internal/reviewer/sdk.go` を作成する（SDK の実際の型は Task 2 で確認した結果に合わせて調整すること）:

```go
package reviewer

import (
	"context"
	"fmt"
	"strings"

	copilot "github.com/github/copilot-sdk/go"
)

// SDKReviewer uses the Copilot SDK to run code review via the copilot CLI
// in server mode. BYOK env vars (COPILOT_PROVIDER_BASE_URL, COPILOT_OFFLINE,
// COPILOT_ALLOW_ALL) are inherited from the parent process.
type SDKReviewer struct {
	client  *copilot.Client
	session *copilot.Session
}

// NewSDKReviewer creates a Copilot SDK client, starts the CLI in server mode,
// and opens a session with our system prompt and no tools.
func NewSDKReviewer(model string) (*SDKReviewer, error) {
	client := copilot.NewClient(nil)
	if err := client.Start(); err != nil {
		return nil, fmt.Errorf("copilot SDK start: %w", err)
	}

	session, err := client.CreateSession(&copilot.SessionConfig{
		OnPermissionRequest: copilot.PermissionHandler.ApproveAll,
		Model:               model,
		SystemMessage:       &copilot.SystemMessage{Content: SystemPrompt},
		AvailableTools:      []string{}, // disable all tools
	})
	if err != nil {
		client.Stop() //nolint:errcheck
		return nil, fmt.Errorf("create session: %w", err)
	}

	return &SDKReviewer{client: client, session: session}, nil
}

// Review sends the diff to the Copilot SDK and returns the review text.
// The timeout in the context is forwarded to SendAndWait (milliseconds).
func (r *SDKReviewer) Review(ctx context.Context, diff string) (string, error) {
	deadline, ok := ctx.Deadline()
	var timeoutMs int64
	if ok {
		timeoutMs = deadline.UnixMilli() - nowUnixMilli()
		if timeoutMs <= 0 {
			return "", context.DeadlineExceeded
		}
	}

	resp, err := r.session.SendAndWait(copilot.MessageOptions{Prompt: diff}, timeoutMs)
	if err != nil {
		return "", fmt.Errorf("SDK SendAndWait: %w", err)
	}
	if resp == nil || resp.Data.Content == nil {
		return "", fmt.Errorf("SDK returned nil response")
	}

	return strings.TrimSpace(*resp.Data.Content), nil
}

// Close stops the Copilot SDK client (and the managed CLI server process).
func (r *SDKReviewer) Close() error {
	return r.client.Stop()
}
```

`nowUnixMilli()` は同ファイル末尾に追加:

```go
func nowUnixMilli() int64 {
	return timeNow().UnixMilli()
}
```

また、テスト可能にするための `var timeNow = time.Now` も追加:

```go
import "time"

var timeNow = time.Now
```

- [ ] **Step 4: ビルドが通ることを確認する**

```bash
go build ./internal/reviewer/
```

Expected: エラーなし（SDKの実際の型と合わない場合は型エラーが出るので修正すること）

- [ ] **Step 5: 短縮テストがパスすることを確認する**

```bash
go test -short ./internal/reviewer/ -v
```

Expected:
```
--- PASS: TestStripCopilotFooter
--- PASS: TestStripCopilotFooter_NoFooter
--- SKIP: TestSDKReviewerIntegration
PASS
```

- [ ] **Step 6: コミットする**

```bash
git add internal/reviewer/sdk.go internal/reviewer/sdk_test.go
git commit -m "feat: add SDKReviewer using github.com/github/copilot-sdk/go"
```

---

### Task 6: main.go をリファクタリングする

**Files:**
- Modify: `cmd/reviewer/main.go`

- [ ] **Step 1: main.go を書き直す**

`cmd/reviewer/main.go` を以下に置き換える:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/kyoneken/ollama-infra/internal/diff"
	"github.com/kyoneken/ollama-infra/internal/ollama"
	"github.com/kyoneken/ollama-infra/internal/reviewer"
)

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
	backend := getenv("REVIEWER_BACKEND", "sdk")
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

	ctx := context.Background()
	logf("Waiting for Ollama to be ready (timeout %s)...", ollamaTimeout)
	if err := client.WaitReady(ctx, ollamaTimeout); err != nil {
		log.Fatalf("ollama not ready: %v", err)
	}
	logf("Ollama is ready.")

	logf("Verifying model %s is present...", model)
	if err := client.EnsureModel(ctx, model); err != nil {
		log.Fatalf("ensure model %s: %v", model, err)
	}

	logf("Creating reviewer (backend: %s)...", backend)
	r, err := reviewer.New(backend, model, ollamaURL)
	if err != nil {
		log.Fatalf("create reviewer: %v", err)
	}
	defer r.Close()

	logf("Running review (model: %s, timeout: %s)...", model, reviewTimeout)
	reviewCtx, cancel := context.WithTimeout(ctx, reviewTimeout)
	defer cancel()

	reviewText, err := r.Review(reviewCtx, truncated)
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

// readDiff returns the diff text. Priority: /workspace/pr.diff file, then PR_DIFF env var.
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
```

- [ ] **Step 2: ビルドが通ることを確認する**

```bash
go build ./cmd/reviewer/
```

Expected: エラーなし

- [ ] **Step 3: 全テストがパスすることを確認する**

```bash
go test -short ./...
```

Expected:
```
ok  	github.com/kyoneken/ollama-infra/cmd/reviewer
ok  	github.com/kyoneken/ollama-infra/internal/diff
ok  	github.com/kyoneken/ollama-infra/internal/ollama
ok  	github.com/kyoneken/ollama-infra/internal/proxy
ok  	github.com/kyoneken/ollama-infra/internal/reviewer
```

- [ ] **Step 4: コミットする**

```bash
git add cmd/reviewer/main.go
git commit -m "refactor: simplify main.go using reviewer.New() interface"
```

---

### Task 7: ローカル Docker E2E テスト（SDK バックエンド）

**Files:**
- なし（Dockerfile は変更なし）

- [ ] **Step 1: Docker イメージをビルドする**

```bash
docker build --platform linux/arm64 -t ollama-sdk-test:latest .
```

Expected: ビルド成功（`Successfully built ...`）

- [ ] **Step 2: SDK バックエンドで E2E テストを実行する**

```bash
docker run --rm \
  -e PR_DIFF='--- a/math.go
+++ b/math.go
@@ -1,7 +1,7 @@
 package math

-func Add(a, b int) int {
-	return a + b
+func Add(a, b int) int {
+	return a - b
 }' \
  -e REVIEWER_BACKEND=sdk \
  ollama-sdk-test:latest
```

Expected: `math.go|...|ERROR|...` 形式の出力（ツール呼び出し JSON でないこと）

**SDKでツール呼び出しが発生した場合の対応：**

出力に `{"name":` や `"arguments":` が含まれる場合、SDKの `systemMessage` + `availableTools:[]` が不十分。その場合は `SDKReviewer.NewSDKReviewer()` でプロキシも起動するよう修正:

```go
// sdk.go に追加
func NewSDKReviewerWithProxy(model, ollamaURL string) (*SDKReviewer, error) {
    prx := proxy.New(ollamaURL)
    port, err := prx.Start()
    if err != nil {
        return nil, fmt.Errorf("start proxy: %w", err)
    }
    proxyURL := fmt.Sprintf("http://localhost:%d/v1", port)
    // COPILOT_PROVIDER_BASE_URL をプロキシURLに上書き
    os.Setenv("COPILOT_PROVIDER_BASE_URL", proxyURL)
    r, err := NewSDKReviewer(model)
    if err != nil {
        prx.Close()
        return nil, err
    }
    r.proxy = prx  // SDKReviewer に proxy フィールドを追加して管理
    return r, nil
}
```

- [ ] **Step 3: CLI バックエンドでも E2E テストを実行する（リグレッション確認）**

```bash
docker run --rm \
  -e PR_DIFF='--- a/math.go
+++ b/math.go
@@ -5,5 +5,5 @@
-	return a + b
+	return a - b' \
  -e REVIEWER_BACKEND=cli \
  ollama-sdk-test:latest
```

Expected: `math.go|...|ERROR|...` 形式の出力（既存動作と同じ）

---

### Task 8: PR 作成・CI 確認

**Files:**
- なし

- [ ] **Step 1: 全変更をプッシュして PR を作成する**

```bash
git push -u origin feat/copilot-sdk
gh pr create \
  --title "feat: Copilot SDK reviewer (sdk→cli→ollama)" \
  --body "## 変更内容

GoのCopilot SDKを使ってOllama経由のコードレビューを実現する技術検証。

### アーキテクチャ
- \`internal/reviewer\` パッケージに \`Reviewer\` インターフェースを追加
- \`SDKReviewer\`: \`github.com/github/copilot-sdk/go\` を使った実装（新規）
- \`CLIReviewer\`: 既存の \`exec.Command\` ベース実装を移動
- \`REVIEWER_BACKEND=sdk|cli\` で切り替え可能
- \`cmd/reviewer/main.go\` から実装詳細を排除（200行→80行）

### 技術検証ポイント
- SDK の \`systemMessage\` + \`availableTools:[]\` でプロキシ不要かを検証
- BYOK env vars の CLI プロセスへの継承を確認" \
  --repo kyoneken/ollama-infra
```

- [ ] **Step 2: CI の Build and Push が成功することを確認する**

```bash
gh run list --repo kyoneken/ollama-infra --limit 3
```

Expected: `feat/copilot-sdk` ブランチの CI が `success`

- [ ] **Step 3: デモリポジトリで AI Code Review を実行する**

デモリポジトリの `ai-review.yml` の `uses:` を一時的に `kyoneken/ollama-infra@feat/copilot-sdk` に変更し、PR を同期して CI をトリガー。出力にレビュー結果が含まれることを確認する。

---

## 技術検証チェックリスト

- [ ] SDK `systemMessage` が copilot のデフォルト system prompt を上書きしているか
- [ ] SDK `availableTools: []` でツール定義が除去されているか（→プロキシ不要）
- [ ] BYOK 環境変数 (`COPILOT_PROVIDER_BASE_URL`) が SDK 経由 CLI に継承されているか
- [ ] レスポンスが `FILE|LINE|SEVERITY|...` 形式（ツール呼び出し JSON でない）か

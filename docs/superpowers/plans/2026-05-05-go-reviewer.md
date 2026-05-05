# Go Reviewer 実装計画

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `entrypoint.sh`（165行のBashスクリプト）をテスト可能なGoプログラムに置き換え、Dockerfileをマルチステージビルドに更新する。

**Architecture:** `internal/diff` パッケージが diff 注釈と切り詰めを担い、`internal/ollama` パッケージが Ollama HTTP API のラッパーとなり、`cmd/reviewer/main.go` がエントリーポイントとしてフローを制御する。標準ライブラリのみ使用（外部依存なし）。

**Tech Stack:** Go 1.22, `net/http`, `encoding/json`, `bufio`, `os/exec`, `net/http/httptest`（テスト用）

---

## ファイル構成

```
新規作成:
  cmd/reviewer/main.go
  internal/diff/annotate.go
  internal/diff/annotate_test.go
  internal/ollama/client.go
  internal/ollama/client_test.go
  go.mod

更新:
  Dockerfile           マルチステージビルドに変更
  .github/workflows/docker-publish.yml   パストリガー更新

削除:
  entrypoint.sh
```

---

## Task 1: Go モジュール初期化

**Files:**
- Create: `go.mod`

- [ ] **Step 1: `go.mod` を作成する**

```bash
cd /Volumes/T7/.ghq/github.com/kyoneken/ollama-infra
go mod init github.com/kyoneken/ollama-infra
```

Expected: `go.mod` が作成され `module github.com/kyoneken/ollama-infra` が含まれる。

- [ ] **Step 2: ディレクトリを作成する**

```bash
mkdir -p cmd/reviewer internal/diff internal/ollama
```

Expected: ディレクトリが作成される。

- [ ] **Step 3: コミットする**

```bash
git add go.mod
git commit -m "chore: initialize Go module (github.com/kyoneken/ollama-infra)"
```

---

## Task 2: diff 注釈パッケージ（TDD）

**Files:**
- Create: `internal/diff/annotate.go`
- Create: `internal/diff/annotate_test.go`

- [ ] **Step 1: テストを書く（先に）**

`internal/diff/annotate_test.go` を作成：

```go
package diff_test

import (
	"strings"
	"testing"

	"github.com/kyoneken/ollama-infra/internal/diff"
)

// 追加行のみの diff
func TestAnnotate_AddedLines(t *testing.T) {
	input := "@@ -0,0 +1,3 @@\n+func Add(a, b int) int {\n+\treturn a + b\n+}\n"
	got := diff.Annotate(input)
	want := "@@ -0,0 +1,3 @@\n[   1]+func Add(a, b int) int {\n[   2]+\treturn a + b\n[   3]+}\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

// コンテキスト行と削除行の混在
func TestAnnotate_ContextAndDeletedLines(t *testing.T) {
	input := "@@ -10,4 +10,4 @@\n func foo() {\n-\treturn 1\n+\treturn 2\n }\n"
	got := diff.Annotate(input)
	want := "@@ -10,4 +10,4 @@\n[  10] func foo() {\n      -\treturn 1\n[  11]+\treturn 2\n[  12] }\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

// +++ 行はそのまま通過する
func TestAnnotate_PlusPlusPlus(t *testing.T) {
	input := "+++ b/main.go\n@@ -1,2 +1,2 @@\n package main\n"
	got := diff.Annotate(input)
	if !strings.Contains(got, "+++ b/main.go") {
		t.Errorf("+++ line should pass through unchanged; got:\n%s", got)
	}
}

// @@ ヘッダーが複数あるとき行番号がリセットされる
func TestAnnotate_MultiHunk(t *testing.T) {
	input := "@@ -1,2 +1,2 @@\n+line1\n@@ -10,2 +10,2 @@\n+line10\n"
	got := diff.Annotate(input)
	if !strings.Contains(got, "[   1]+line1") {
		t.Errorf("first hunk should start at line 1; got:\n%s", got)
	}
	if !strings.Contains(got, "[  10]+line10") {
		t.Errorf("second hunk should start at line 10; got:\n%s", got)
	}
}

// 切り詰めなし
func TestTruncate_NoTruncation(t *testing.T) {
	s := "hello"
	got := diff.Truncate(s, 100)
	if got != s {
		t.Errorf("expected no truncation, got: %q", got)
	}
}

// maxChars で正確に切り詰め、通知行が付く
func TestTruncate_Truncates(t *testing.T) {
	s := "abcdefghij"
	got := diff.Truncate(s, 5)
	if !strings.HasPrefix(got, "abcde") {
		t.Errorf("expected prefix 'abcde', got: %q", got)
	}
	if !strings.Contains(got, "truncated at 5") {
		t.Errorf("expected truncation notice, got: %q", got)
	}
}

// ちょうど maxChars のとき切り詰めなし
func TestTruncate_ExactLength(t *testing.T) {
	s := "abcde"
	got := diff.Truncate(s, 5)
	if got != s {
		t.Errorf("expected exact length to not truncate, got: %q", got)
	}
}
```

- [ ] **Step 2: テストが失敗することを確認する**

```bash
go test ./internal/diff/...
```

Expected: `cannot find package` または `undefined: diff.Annotate` で失敗。

- [ ] **Step 3: 実装を書く**

`internal/diff/annotate.go` を作成：

```go
package diff

import (
	"bufio"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// hunkHeaderRe は @@ ヘッダーの + 側の開始行番号を抽出する。
// 例: "@@ -1,4 +5,8 @@" → ["  +5", "5"]
var hunkHeaderRe = regexp.MustCompile(`\+(\d+)`)

// Annotate は unified diff テキストに新ファイル行番号を注釈する。
//
//   - 追加行 (+):  "[   N]+content"
//   - コンテキスト行 ( ): "[   N] content"
//   - 削除行 (-):  "      -content"
//   - @@ / +++ などのメタ行: そのまま出力
func Annotate(diffText string) string {
	var sb strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(diffText))
	newline := 0

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "@@"):
			// @@ ヘッダーから + 側の開始行番号を取得する（最後のマッチを使用）
			matches := hunkHeaderRe.FindAllStringSubmatch(line, -1)
			if len(matches) > 0 {
				n, _ := strconv.Atoi(matches[len(matches)-1][1])
				newline = n - 1 // 次の行でインクリメントされる
			}
			sb.WriteString(line + "\n")
		case strings.HasPrefix(line, "+++"):
			sb.WriteString(line + "\n")
		case strings.HasPrefix(line, "+"):
			newline++
			sb.WriteString(fmt.Sprintf("[%4d]+%s\n", newline, line[1:]))
		case strings.HasPrefix(line, " "):
			newline++
			sb.WriteString(fmt.Sprintf("[%4d] %s\n", newline, line[1:]))
		case strings.HasPrefix(line, "-"):
			sb.WriteString(fmt.Sprintf("      -%s\n", line[1:]))
		default:
			sb.WriteString(line + "\n")
		}
	}
	return sb.String()
}

// Truncate は diffText を最大 maxChars バイトに切り詰める。
// 超過した場合は末尾に通知行を追加する。
func Truncate(diffText string, maxChars int) string {
	if len(diffText) <= maxChars {
		return diffText
	}
	return diffText[:maxChars] + fmt.Sprintf("\n[... diff truncated at %d chars ...]", maxChars)
}
```

- [ ] **Step 4: テストが通ることを確認する**

```bash
go test ./internal/diff/... -v
```

Expected: 全テスト PASS。

- [ ] **Step 5: コミットする**

```bash
git add internal/diff/
git commit -m "feat: add diff annotation package (replaces awk in entrypoint.sh)"
```

---

## Task 3: Ollama クライアントパッケージ（TDD）

**Files:**
- Create: `internal/ollama/client.go`
- Create: `internal/ollama/client_test.go`

- [ ] **Step 1: テストを書く（先に）**

`internal/ollama/client_test.go` を作成：

```go
package ollama_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kyoneken/ollama-infra/internal/ollama"
)

// Ollama がすぐに 200 を返す場合、WaitReady は成功する
func TestWaitReady_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Ollama is running")
	}))
	defer srv.Close()

	c := ollama.NewClient(srv.URL)
	err := c.WaitReady(context.Background(), 5*time.Second)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// Ollama が ready にならない場合、timeout 後にエラーを返す
func TestWaitReady_Timeout(t *testing.T) {
	// 503 を返し続けるサーバー（ready ではない状態をシミュレート）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := ollama.NewClient(srv.URL)
	// context でタイムアウトを制御（200ms 後に ctx.Done() が発火）
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := c.WaitReady(ctx, 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

// EnsureModel は /api/pull にリクエストを送る
func TestEnsureModel_CallsPullEndpoint(t *testing.T) {
	var gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody) //nolint
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"success"}`)
	}))
	defer srv.Close()

	c := ollama.NewClient(srv.URL)
	_ = c.EnsureModel(context.Background(), "qwen2.5-coder:1.5b")

	if gotPath != "/api/pull" {
		t.Errorf("expected path /api/pull, got: %s", gotPath)
	}
	if gotBody["name"] != "qwen2.5-coder:1.5b" {
		t.Errorf("expected name=qwen2.5-coder:1.5b in body, got: %v", gotBody)
	}
}

// Generate は NDJSON ストリームから .response を結合して返す
func TestGenerate_StreamingResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunks := []map[string]any{
			{"response": "main.go", "done": false},
			{"response": "|9|ERROR|wrong operator|return a + b|演算子が間違っています。", "done": false},
			{"response": "", "done": true},
		}
		for _, chunk := range chunks {
			b, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "%s\n", b)
		}
	}))
	defer srv.Close()

	c := ollama.NewClient(srv.URL)
	result, err := c.Generate(context.Background(), "testmodel", "test prompt", ollama.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "main.go|9|ERROR|wrong operator|return a + b|演算子が間違っています。"
	if result != want {
		t.Errorf("got: %q\nwant: %q", result, want)
	}
}

// Generate は done:true のチャンク後で終了する
func TestGenerate_StopsAtDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunks := []map[string]any{
			{"response": "first", "done": false},
			{"response": "", "done": true},
			{"response": "should not appear", "done": false}, // done 後
		}
		for _, chunk := range chunks {
			b, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "%s\n", b)
		}
	}))
	defer srv.Close()

	c := ollama.NewClient(srv.URL)
	result, err := c.Generate(context.Background(), "testmodel", "prompt", ollama.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "first" {
		t.Errorf("expected 'first', got: %q", result)
	}
}

// GenerateOptions のデフォルト値が正しく適用される
func TestGenerate_DefaultOptions(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody) //nolint
		b, _ := json.Marshal(map[string]any{"response": "", "done": true})
		fmt.Fprintf(w, "%s\n", b)
	}))
	defer srv.Close()

	c := ollama.NewClient(srv.URL)
	_, _ = c.Generate(context.Background(), "mymodel", "prompt", ollama.GenerateOptions{})

	opts, ok := gotBody["options"].(map[string]any)
	if !ok {
		t.Fatal("expected options field in request body")
	}
	if opts["num_ctx"] != float64(2048) {
		t.Errorf("expected num_ctx=2048, got: %v", opts["num_ctx"])
	}
	if opts["num_predict"] != float64(500) {
		t.Errorf("expected num_predict=500, got: %v", opts["num_predict"])
	}
	if opts["temperature"] != float64(0.1) {
		t.Errorf("expected temperature=0.1, got: %v", opts["temperature"])
	}
}
```

- [ ] **Step 2: テストが失敗することを確認する**

```bash
go test ./internal/ollama/...
```

Expected: `undefined: ollama.NewClient` などで失敗。

- [ ] **Step 3: 実装を書く**

`internal/ollama/client.go` を作成：

```go
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Client は Ollama HTTP API の薄いラッパー。
// ollama CLI は呼び出さず、すべて REST API 経由で操作する。
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// GenerateOptions はレビュー生成リクエストのパラメータ。
type GenerateOptions struct {
	NumCtx      int     // デフォルト: 2048
	NumPredict  int     // デフォルト: 500
	Temperature float64 // デフォルト: 0.1
}

// NewClient は指定した baseURL（例: "http://localhost:11434"）のクライアントを作成する。
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{},
	}
}

// Start は "ollama serve" をバックグラウンドプロセスとして起動する。
// 起動後すぐ返る（準備完了を待たない）。準備待ちは WaitReady で行う。
func (c *Client) Start() error {
	cmd := exec.Command("ollama", "serve")
	return cmd.Start()
}

// WaitReady は Ollama が HTTP リクエストに応答するまで待機する。
// timeout を超えるか ctx がキャンセルされた場合はエラーを返す。
// 1 秒ごとにリトライする。
func (c *Client) WaitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/", nil)
		if err != nil {
			return err
		}
		resp, err := c.httpClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 400 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("ollama did not become ready within %s", timeout)
}

// EnsureModel は POST /api/pull でモデルの存在を確認する。
// イメージにプリベイク済みの場合は数秒で返る。
func (c *Client) EnsureModel(ctx context.Context, model string) error {
	payload := map[string]any{"name": model, "stream": false}
	return c.postDrain(ctx, "/api/pull", payload)
}

// Generate は POST /api/generate にストリーミングリクエストを送り、
// 全チャンクの .response フィールドを結合した文字列を返す。
func (c *Client) Generate(ctx context.Context, model, prompt string, opts GenerateOptions) (string, error) {
	if opts.NumCtx == 0 {
		opts.NumCtx = 2048
	}
	if opts.NumPredict == 0 {
		opts.NumPredict = 500
	}
	if opts.Temperature == 0 {
		opts.Temperature = 0.1
	}

	payload := map[string]any{
		"model":  model,
		"prompt": prompt,
		"stream": true,
		"options": map[string]any{
			"num_ctx":     opts.NumCtx,
			"num_predict": opts.NumPredict,
			"temperature": opts.Temperature,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST /api/generate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama returned %d: %s", resp.StatusCode, b)
	}

	var sb strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var chunk struct {
			Response string `json:"response"`
			Done     bool   `json:"done"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue // 不正な行はスキップ
		}
		sb.WriteString(chunk.Response)
		if chunk.Done {
			break
		}
	}
	return sb.String(), scanner.Err()
}

// postDrain は JSON ボディを POST してレスポンスボディを破棄する内部ヘルパー。
func (c *Client) postDrain(ctx context.Context, path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, path)
	}
	return nil
}
```

- [ ] **Step 4: テストが通ることを確認する**

```bash
go test ./internal/ollama/... -v
```

Expected: 全テスト PASS。

- [ ] **Step 5: コミットする**

```bash
git add internal/ollama/
git commit -m "feat: add ollama HTTP client package (replaces curl/jq in entrypoint.sh)"
```

---

## Task 4: メインエントリーポイント

**Files:**
- Create: `cmd/reviewer/main.go`

- [ ] **Step 1: `main.go` を作成する**

`cmd/reviewer/main.go` を作成：

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/kyoneken/ollama-infra/internal/diff"
	"github.com/kyoneken/ollama-infra/internal/ollama"
)

// systemPrompt は entrypoint.sh の SYSTEM_PROMPT をそのまま移植（変更なし）。
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
	maxDiffChars  = 4000
	ollamaTimeout = 60 * time.Second
	reviewTimeout = 480 * time.Second
)

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[reviewer] "+format+"\n", args...)
}

func main() {
	model := getenv("COPILOT_MODEL", "qwen2.5-coder:1.5b")
	outputPath := getenv("REVIEW_OUTPUT", "/tmp/review.txt")

	client := ollama.NewClient(ollamaURL)

	logf("Starting ollama serve...")
	if err := client.Start(); err != nil {
		log.Fatalf("failed to start ollama: %v", err)
	}

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

	diffText, err := readDiff()
	if err != nil {
		log.Fatalf("read diff: %v", err)
	}

	annotated := diff.Annotate(diffText)
	truncated := diff.Truncate(annotated, maxDiffChars)
	logf("Diff: %d chars (annotated), truncated to %d chars", len(annotated), len(truncated))

	prompt := systemPrompt + "\n\nDIFF:\n" + truncated + "\n\nREVIEW:"

	logf("Running code review (stream:true, num_predict:500, timeout 480s)...")
	reviewCtx, cancel := context.WithTimeout(ctx, reviewTimeout)
	defer cancel()

	reviewText, err := client.Generate(reviewCtx, model, prompt, ollama.GenerateOptions{
		NumCtx:      2048,
		NumPredict:  500,
		Temperature: 0.1,
	})
	if err != nil {
		logf("Generate error: %v — using fallback message", err)
		reviewText = ""
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

// readDiff は /workspace/pr.diff ファイルを優先し、なければ PR_DIFF 環境変数を使う。
func readDiff() (string, error) {
	const diffFile = "/workspace/pr.diff"
	if _, err := os.Stat(diffFile); err == nil {
		logf("Using %s...", diffFile)
		b, err := os.ReadFile(diffFile)
		return string(b), err
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

- [ ] **Step 2: ビルドして確認する**

```bash
go build ./cmd/reviewer/
```

Expected: エラーなしでビルド成功。`reviewer` バイナリが生成される。

- [ ] **Step 3: `go mod tidy` を実行する**

```bash
go mod tidy
```

Expected: `go.sum` が作成される（外部依存なしのため最小限）。

- [ ] **Step 4: 全テストが通ることを確認する**

```bash
go test ./...
```

Expected: 全テスト PASS。

- [ ] **Step 5: コミットする**

```bash
rm -f reviewer  # ビルド成果物を削除
git add cmd/ go.sum
git commit -m "feat: add main reviewer entrypoint (Go replacement for entrypoint.sh)"
```

---

## Task 5: Dockerfile をマルチステージビルドに更新

**Files:**
- Modify: `Dockerfile`

- [ ] **Step 1: Dockerfile を更新する**

`Dockerfile` の内容を以下に置き換える：

```dockerfile
# Stage 1: Go バイナリのビルド
FROM golang:1.22-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 GOOS=linux go build -o /reviewer ./cmd/reviewer

# Stage 2: ランタイム（Ollama ベースイメージを維持）
FROM ollama/ollama:latest

ENV DEBIAN_FRONTEND=noninteractive

# entrypoint.sh で使用していた gh / jq / zstd は Go バイナリで不要になるため削除
# curl は ollama/ollama:latest ベースイメージに含まれている
RUN apt-get update && apt-get install -y \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# モデルのpre-bake（CI 実行時にダウンロード不要にする）
RUN ollama serve & \
    OLLAMA_PID=$! && \
    echo "Waiting for Ollama..." && \
    for i in $(seq 1 60); do \
      sleep 1 && curl -sf http://localhost:11434/ > /dev/null 2>&1 && echo "Ready after ${i}s" && break; \
    done && \
    ollama pull qwen2.5-coder:1.5b && \
    echo "Model size: $(du -sh /root/.ollama/models)" && \
    kill "$OLLAMA_PID" && wait "$OLLAMA_PID" 2>/dev/null || true

ENV COPILOT_MODEL=qwen2.5-coder:1.5b
ENV COPILOT_OFFLINE=true
ENV OLLAMA_HOST=0.0.0.0

WORKDIR /workspace

# entrypoint.sh の代わりに Go バイナリを使用
COPY --from=builder /reviewer /reviewer
ENTRYPOINT ["/reviewer"]
```

- [ ] **Step 2: ローカルビルドで動作確認する（オプション、Docker が使える環境のみ）**

```bash
docker build -t ollama-review-test . 2>&1 | tail -20
```

Expected: `Successfully built ...` — ただしモデルのダウンロードで数分かかる。

- [ ] **Step 3: コミットする**

```bash
git add Dockerfile
git commit -m "feat: convert Dockerfile to multi-stage build (Go builder + ollama runtime)"
```

---

## Task 6: docker-publish.yml のパストリガーを更新

**Files:**
- Modify: `.github/workflows/docker-publish.yml`

- [ ] **Step 1: パストリガーを更新する**

`.github/workflows/docker-publish.yml` の `paths:` セクションを変更：

変更前：
```yaml
    paths:
      - 'Dockerfile'
      - 'entrypoint.sh'
```

変更後：
```yaml
    paths:
      - 'Dockerfile'
      - 'cmd/**'
      - 'internal/**'
```

- [ ] **Step 2: コミットする**

```bash
git add .github/workflows/docker-publish.yml
git commit -m "ci: update docker-publish trigger paths (entrypoint.sh -> cmd/**, internal/**)"
```

---

## Task 7: entrypoint.sh を削除

**Files:**
- Delete: `entrypoint.sh`

- [ ] **Step 1: entrypoint.sh を削除する**

```bash
git rm entrypoint.sh
```

Expected: `rm 'entrypoint.sh'`

- [ ] **Step 2: コミットする**

```bash
git commit -m "remove: delete entrypoint.sh (replaced by cmd/reviewer/main.go)"
```

---

## Task 8: プッシュして CI を確認

- [ ] **Step 1: main ブランチにプッシュする**

```bash
git push origin main
```

- [ ] **Step 2: docker-publish.yml の CI が起動することを確認する**

GitHub Actions の `Build and Push Docker Image to GHCR` ワークフローが起動していることを確認する。
（Dockerfile と cmd/ の両方を変更したため、トリガー条件に該当する）

- [ ] **Step 3: Docker ビルドが成功したら demo リポジトリで動作確認する**

`kyoneken/ollama-infra-demo` のデモ PR に対して AI レビューを再実行し、インラインコメントが正常に付くことを確認する。

```bash
# ollama-infra-demo リポジトリで PR に push して CI を起動
# または GitHub Actions UI から手動で "Run workflow"
```

Expected:
- `[reviewer] Starting ollama serve...` ログが出る
- `[reviewer] Ollama is ready.` ログが出る
- インラインコメントが line 9, 25, 36, 49 などに付く

---

## チェックリスト（スペック対応確認）

| スペック要件 | 対応タスク |
|------------|-----------|
| `entrypoint.sh` を削除 | Task 7 |
| `cmd/reviewer/main.go` を作成 | Task 4 |
| `internal/ollama/client.go` を作成 | Task 3 |
| `internal/diff/annotate.go` を作成 | Task 2 |
| Dockerfile マルチステージビルド | Task 5 |
| gh / jq / zstd を Dockerfile から削除 | Task 5 |
| docker-publish.yml パストリガー更新 | Task 6 |
| `action.yml` は変更しない | ✅ 変更なし |
| システムプロンプトの内容は変更しない | Task 4 で `const systemPrompt` にそのまま移植 |

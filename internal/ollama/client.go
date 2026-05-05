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
	serveCmd   *exec.Cmd
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
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}
}

// Start は "ollama serve" をバックグラウンドプロセスとして起動する。
// 起動後すぐ返る（準備完了を待たない）。準備待ちは WaitReady で行う。
func (c *Client) Start() error {
	cmd := exec.Command("ollama", "serve")
	c.serveCmd = cmd
	return cmd.Start()
}

// Stop terminates the ollama serve process if it was started by Start().
func (c *Client) Stop() {
	if c.serveCmd != nil && c.serveCmd.Process != nil {
		c.serveCmd.Process.Kill() //nolint:errcheck
	}
}

// WaitReady は Ollama が HTTP リクエストに応答するまで待機する。
// timeout を超えるか ctx がキャンセルされた場合はエラーを返す。
// 1 秒ごとにリトライする。
func (c *Client) WaitReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("ollama did not become ready: %w", ctx.Err())
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/", nil)
		if err == nil {
			resp, err := c.httpClient.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode < 400 {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("ollama did not become ready: %w", ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

// EnsureModel は POST /api/pull でモデルをダウンロード（または確認）する。
// Dockerイメージにプリベイク済みの場合は即座に返る。
// 初回実行時はモデルのダウンロードに数分かかる場合がある。
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
	if opts.Temperature <= 0 {
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
	scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1 MB per line — prevents truncation of long model responses
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
	if err := scanner.Err(); err != nil {
		return sb.String(), fmt.Errorf("scanner error: %w", err)
	}
	return sb.String(), nil
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
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, path, bytes.TrimSpace(b))
	}
	return nil
}

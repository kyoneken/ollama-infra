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

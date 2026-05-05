# Go Reviewer リファクタリング設計書

## 目的

`entrypoint.sh`（165行のBashスクリプト）を、テスト可能で読みやすいGoプログラムに置き換える。

**解決したい問題：**
- シェルスクリプトのエスケープ地獄（jqへの文字列渡しなど）
- awk/curl/jqパイプラインのデバッグの困難さ
- ユニットテストが書けない

**スコープ外：**
- ベースイメージの変更（`ollama/ollama:latest` を維持）
- Copilot SDK の利用（大きなシステムプロンプト問題あり）
- `action.yml` の変更

---

## アーキテクチャ

### ファイル構成

```
ollama-infra/
├── cmd/
│   └── reviewer/
│       └── main.go              # エントリーポイント
├── internal/
│   ├── ollama/
│   │   ├── client.go            # Ollama HTTP クライアント
│   │   └── client_test.go
│   └── diff/
│       ├── annotate.go          # diff 行番号注釈
│       └── annotate_test.go
├── go.mod                       # module: github.com/kyoneken/ollama-infra
├── go.sum
├── Dockerfile                   # マルチステージビルドに更新
└── entrypoint.sh                # 削除
```

### コンポーネント責務

| コンポーネント | 責務 |
|--------------|------|
| `cmd/reviewer/main.go` | 環境変数の読み取り、フロー制御、エラー出力 |
| `internal/ollama/client.go` | Ollama サーバー起動・ヘルスチェック・モデル確認・レビュー生成 |
| `internal/diff/annotate.go` | diff への行番号注釈 (`[  N]+content`)、文字数切り詰め |

---

## 設計詳細

### `internal/diff/annotate.go`

現在の `entrypoint.sh` の `annotate_diff()` (awk) を Go で再実装。

**インターフェース：**
```go
// Annotate は diff テキストに新ファイル行番号を注釈して返す
// diff 形式: `[  N]+content` (追加行), `[  N] content` (コンテキスト行), `      -content` (削除行)
func Annotate(diff string) string

// Truncate は diff テキストを maxChars 文字に切り詰める（超過時は通知行を追加）
func Truncate(diff string, maxChars int) string
```

### `internal/ollama/client.go`

Ollama REST API (`http://localhost:11434`) の薄いラッパー。`ollama` CLIの呼び出しは一切行わない（すべてHTTP API経由）。

**インターフェース：**
```go
type Client struct { baseURL string; httpClient *http.Client }

func NewClient(baseURL string) *Client

// Start は ollama serve を子プロセスとして起動する
func (c *Client) Start() error

// WaitReady は Ollama が応答するまで最大 timeout 待機する
func (c *Client) WaitReady(ctx context.Context, timeout time.Duration) error

// EnsureModel は /api/pull を呼び出してモデルが存在することを確認する（pre-bake済みなら即返る）
func (c *Client) EnsureModel(ctx context.Context, model string) error

// Generate はストリーミングでレビューテキストを生成して返す
// options: num_ctx, num_predict, temperature など
func (c *Client) Generate(ctx context.Context, model, prompt string, opts GenerateOptions) (string, error)
```

**`Generate` の実装方針：**
- `POST /api/generate` に `stream: true` で送信
- レスポンスの NDJSON を1行ずつ読み込み `.response` フィールドを結合
- タイムアウトは ctx で制御（呼び出し元が設定）

**`GenerateOptions`：**
```go
type GenerateOptions struct {
    NumCtx     int     // デフォルト: 2048
    NumPredict int     // デフォルト: 500
    Temperature float64 // デフォルト: 0.1
}
```

### `cmd/reviewer/main.go`

環境変数：

| 変数 | 説明 | デフォルト |
|------|------|-----------|
| `REVIEW_OUTPUT` | 結果の書き込み先ファイルパス | `/tmp/review.txt` |
| `COPILOT_MODEL` | 使用するモデル名 | `qwen2.5-coder:1.5b` |

diff 入力の優先順位：
1. `/workspace/pr.diff` ファイル
2. `PR_DIFF` 環境変数

**フロー：**
1. Ollama 起動 (`client.Start`)
2. 準備待ち (`client.WaitReady`, 60s タイムアウト)
3. モデル確認 (`client.EnsureModel`)
4. diff 読み込み → `diff.Annotate` → `diff.Truncate(4000)`
5. システムプロンプト + diff を結合してレビュー生成 (`client.Generate`, 480s タイムアウト)
6. 結果を `REVIEW_OUTPUT` に書き込み

**システムプロンプト：** 現行 `entrypoint.sh` の `SYSTEM_PROMPT` をそのまま移植（変更なし）

### Dockerfile（マルチステージビルド）

```dockerfile
# Stage 1: Go バイナリのビルド
FROM golang:1.22-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /reviewer ./cmd/reviewer

# Stage 2: ランタイム（ベースイメージは維持）
FROM ollama/ollama:latest

# 不要パッケージのインストールを省略（gh, jq, zstd は削除）
# curl は Ollama ベースイメージに含まれている
RUN apt-get update && apt-get install -y \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# モデルのpre-bake（変更なし）
RUN ollama serve & \
    OLLAMA_PID=$! && \
    for i in $(seq 1 60); do \
      sleep 1 && curl -sf http://localhost:11434/ > /dev/null 2>&1 && break; \
    done && \
    ollama pull qwen2.5-coder:1.5b && \
    kill "$OLLAMA_PID" && wait "$OLLAMA_PID" 2>/dev/null || true

ENV COPILOT_MODEL=qwen2.5-coder:1.5b
ENV COPILOT_OFFLINE=true
ENV OLLAMA_HOST=0.0.0.0

WORKDIR /workspace

# entrypoint.sh の代わりに Go バイナリを使用
COPY --from=builder /reviewer /reviewer
ENTRYPOINT ["/reviewer"]
```

---

## テスト方針

### `internal/diff/annotate_test.go`
- 正常なunified diff入力 → 期待する注釈付き出力
- `@@` ヘッダーの行番号パース（`+15,8` 形式）
- 削除行に行番号が付かないこと
- `Truncate` が maxChars で正確に切り詰めること

### `internal/ollama/client_test.go`
- `httptest.Server` でモック
- ストリーミングNDJSON → `Generate` が正しく結合すること
- `WaitReady` がタイムアウトすると error を返すこと
- `EnsureModel` が `/api/pull` を呼ぶこと

---

## 削除されるもの

- `entrypoint.sh`（完全削除）
- Dockerfile の `jq`, `zstd`, `gh` パッケージインストール

## 変更されないもの

- `action.yml`
- `.copilot/` ディレクトリ
- システムプロンプトの内容
- `docker-publish.yml` ワークフロー（トリガー条件: `Dockerfile` or `entrypoint.sh` の代わりに `Dockerfile` or `cmd/**` or `internal/**` に更新が必要）

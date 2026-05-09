# Design: Copilot SDK Reviewer (SDK→CLI→Ollama)

**Date:** 2026-05-09  
**Status:** Approved  
**Goal:** 技術検証 — GoからCopilot SDKを使ってOllama経由のコードレビューを実現する

---

## 背景と動機

現在の実装は `exec.Command("copilot", "-p", "...")` でCopilot CLIをサブプロセスとして直接呼び出している。
Copilot SDKを使うことで：

1. SDKの知見を高める（技術検証）
2. コードのアーキテクチャを改善（責務分離）
3. 将来的にSDK機能（セッション永続化、カスタムエージェント等）を活かせる基盤を作る

---

## アーキテクチャ

```
reviewer binary (Go)
  ├── Ollama serve (pre-baked qwen2.5-coder:1.5b)
  └── Reviewer interface
        ├── SDKReviewer (REVIEWER_BACKEND=sdk, default)
        │     └── copilot.Client → copilot CLI (server mode, 自動起動)
        │           └── BYOK env → [proxy不要を検証] → Ollama
        └── CLIReviewer (REVIEWER_BACKEND=cli)
              └── exec.Command("copilot", ...) + Proxy → Ollama
```

**SDK実装の仮説：** `systemMessage` でcopilotデフォルト system prompt を上書き、`availableTools: []` でツール定義を除去することで、プロキシなしにOllamaが正しくレビューテキストを返すことを検証する。

---

## パッケージ構造

```
internal/
  reviewer/
    reviewer.go    ← Reviewer インターフェース + systemPrompt 定数
    sdk.go         ← SDKReviewer 実装（新規）
    cli.go         ← CLIReviewer 実装（main.goから移動）
  proxy/           ← 変更なし（CLIReviewer で使用）
  diff/            ← 変更なし
  ollama/          ← 変更なし
cmd/reviewer/
  main.go          ← Ollamaライフサイクル管理 + Reviewer生成のみ
```

---

## インターフェース定義

```go
// internal/reviewer/reviewer.go

type Reviewer interface {
    Review(ctx context.Context, diff string) (string, error)
    Close() error
}

// New selects a Reviewer based on REVIEWER_BACKEND env var.
// backend: "sdk" (default) or "cli"
// proxyPort: used only by CLIReviewer (0 = start new proxy)
func New(backend, model, ollamaURL string) (Reviewer, error)
```

---

## SDKReviewer

```go
// internal/reviewer/sdk.go

type SDKReviewer struct {
    client  *copilot.Client
    session *copilot.Session
}

func NewSDKReviewer(model string) (*SDKReviewer, error) {
    client := copilot.NewClient(nil) // BYOK env vars 継承
    if err := client.Start(); err != nil { ... }

    session, err := client.CreateSession(&copilot.SessionConfig{
        OnPermissionRequest: copilot.PermissionHandler.ApproveAll,
        Model:              model,
        SystemMessage:      &copilot.SystemMessage{Content: systemPrompt},
        AvailableTools:     []string{}, // ツール定義を除去
    })

    return &SDKReviewer{client, session}, nil
}

func (r *SDKReviewer) Review(ctx context.Context, diff string) (string, error) {
    resp, err := r.session.SendAndWait(copilot.MessageOptions{Prompt: diff}, 480000)
    return *resp.Data.Content, err
}

func (r *SDKReviewer) Close() error { return r.client.Stop() }
```

**BYOK連携：** `COPILOT_PROVIDER_BASE_URL`、`COPILOT_OFFLINE`、`COPILOT_ALLOW_ALL` はGoプロセスから自動継承。

---

## CLIReviewer

既存の `cmd/reviewer/main.go` の `runCopilot()` / `stripCopilotFooter()` ロジックを移動。
Proxyの起動・管理もCLIReviewer内に閉じる。

```go
// internal/reviewer/cli.go

type CLIReviewer struct {
    proxy *proxy.Server
    model string
}

func NewCLIReviewer(model, ollamaURL string) (*CLIReviewer, error) {
    // proxy起動
    // (既存ロジックをそのまま移動)
}
```

---

## main.goの責務（簡素化後）

1. PR diff の読み込み
2. Ollama serve 起動・待機
3. モデル存在確認
4. `reviewer.New(backend, model, ollamaURL)` でReviewer生成
5. `r.Review(ctx, diff)` 呼び出し
6. 結果をファイルに書き出し

約200行 → 約80行に削減。

---

## 環境変数

| 変数 | デフォルト | 説明 |
|------|-----------|------|
| `REVIEWER_BACKEND` | `sdk` | `sdk` または `cli` |
| `COPILOT_MODEL` | `qwen2.5-coder:1.5b` | 使用モデル |
| `COPILOT_PROVIDER_BASE_URL` | `http://localhost:11434/v1` | OllamaのBYOKエンドポイント |
| `COPILOT_OFFLINE` | `true` | Copilotオフラインモード |
| `COPILOT_ALLOW_ALL` | `true` | 全権限許可 |

---

## 技術検証ポイント

1. **プロキシ不要の検証：** SDKの `systemMessage` + `availableTools:[]` でOllamaがツール呼び出しではなくテキストを返すか
2. **BYOK継承：** 環境変数がSDK経由のCLIプロセスに正しく伝わるか
3. **応答フォーマット：** `FILE|LINE|SEVERITY|...` 形式が維持されるか

プロキシなしで失敗した場合は、`COPILOT_PROVIDER_BASE_URL` をプロキシURLに設定するオプションをCLIReviewerに保持し、SDKReviewerにも同様のオプションを追加する。

---

## テスト戦略

- `internal/reviewer/sdk_test.go`: SDKReviewerのユニットテスト（Ollamaモック）
- `internal/reviewer/cli_test.go`: CLIReviewerのユニットテスト
- ローカルDockerでのE2Eテスト（既存手順）

---

## 依存関係の追加

```
go get github.com/github/copilot-sdk/go
```

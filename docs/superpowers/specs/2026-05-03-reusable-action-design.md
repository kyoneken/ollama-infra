# Design: ollama-infra を Reusable Composite Action として公開する

## 問題と目的

`ollama-infra` は現在、同一リポジトリ内でのみ動作する AI コードレビュー基盤である。これを他のリポジトリから `uses: kyoneken/ollama-infra@v1` のように呼び出せる **GitHub Actions Composite Action** として公開し、誰でも API キー不要でオフライン LLM コードレビューを導入できるようにする。

## アーキテクチャ概要

```
ollama-infra (public)
├── action.yml                        ← Composite Action エントリポイント
├── Dockerfile                        ← qwen2.5-coder:1.5b pre-bake（変更なし）
├── entrypoint.sh                     ← レビュー実行スクリプト（変更なし）
├── .github/workflows/
│   ├── docker-publish.yml            ← main push 時に GHCR へイメージを push
│   └── ai-review.yml                 ← 自リポジトリ用（action 経由に変更）
└── demo/ (git submodule)             ← kyoneken/ollama-infra-demo (Private)
    ├── main.go
    └── .github/workflows/ai-review.yml
```

## コンポーネント設計

### 1. `action.yml` — Composite Action

- **用途**: 他リポジトリから `uses: kyoneken/ollama-infra@v1` で呼び出されるエントリポイント
- **inputs**:
  - `github-token` (required): PR コメント投稿用
  - `model` (optional, default: `""`): モデル名の上書き。空の場合は pre-installed モデルを使用
- **動作**:
  1. PR の diff を生成（`git diff` + インフラファイル除外）
  2. `ghcr.io/kyoneken/ollama-review:latest` を pull して実行
  3. レビュー結果を PR コメントとして投稿
- **モデルデフォルト動作**: `COPILOT_MODEL` が空の場合、`entrypoint.sh` の `BASE_MODEL="${COPILOT_MODEL:-qwen2.5-coder:1.5b}"` により pre-installed の `qwen2.5-coder:1.5b` が使われる

### 2. `.github/workflows/docker-publish.yml` — GHCR 公開

- **トリガー**: `main` ブランチへの push（`Dockerfile`, `entrypoint.sh`, `.copilot/**` 変更時）および `workflow_dispatch`
- **タグ戦略**:
  - `ghcr.io/kyoneken/ollama-review:latest`
  - `ghcr.io/kyoneken/ollama-review:<git-sha>`
- **認証**: `GITHUB_TOKEN` のみ（追加シークレット不要）
- **イメージ公開設定**: Public パッケージとして公開（他リポジトリからの pull を許可）
- **キャッシュ**: BuildKit キャッシュを活用し、Dockerfile 未変更時はモデル再ダウンロードをスキップ

### 3. `demo/` — 検証用リポジトリ（サブモジュール）

- **リポジトリ**: `kyoneken/ollama-infra-demo`（Private）
- **言語**: Go
- **内容**: 意図的なバグを含む簡単な Go CLI アプリ（計算機など）
- **ワークフロー**: `uses: kyoneken/ollama-infra@main` で AI レビューを実行
- **目的**: action の動作確認と E2E テスト

## データフロー

```
他リポジトリの PR
    │
    ▼
action.yml (Composite Action)
    │ git diff → pr.diff
    │
    ▼
docker run ghcr.io/kyoneken/ollama-review:latest
    │ entrypoint.sh が実行される
    │ ollama serve → qwen2.5-coder:1.5b（pre-baked）
    │ Ollama REST API でレビュー生成
    │
    ▼
review_output.txt
    │
    ▼
github-script → PR コメント投稿
```

## セキュリティ・権限

- `GITHUB_TOKEN` のみで動作（追加シークレット不要）
- イメージは GHCR に Public 公開（pull に認証不要）
- action.yml の permissions は `contents: read`, `pull-requests: write` のみ

## 移行方針

1. `main` ブランチを `git fetch && git checkout main && git pull` で最新化
2. `localize/japanese-prompts` の変更は新機能実装の中で再適用または別 PR で対応
3. `docker-publish.yml` を追加後、手動 `workflow_dispatch` で初回イメージをビルド
4. `action.yml` 追加後、`demo` リポジトリを作成してサブモジュール登録

## 未解決事項

- 現在の `ai-review.yml` は Dockerfile をその場でビルドしている。GHCR の image を使う方式に移行するか、両方維持するか検討余地あり（本設計では GHCR 版に統一する）
- `localize/japanese-prompts` のエントリポイント日本語化は別 PR として扱う

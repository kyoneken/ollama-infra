# ollama-infra

GitHub Actions でプルリクエストをローカル LLM で自動コードレビューする再利用可能なアクションです。外部の AI API キー不要で完全オフライン動作します。

## 他のリポジトリからの利用方法

`.github/workflows/ai-review.yml` を作成するだけで使えます：

```yaml
name: AI Code Review

on:
  pull_request:
    types: [opened, synchronize, reopened]

permissions:
  contents: read
  pull-requests: write
  issues: write

jobs:
  ai-review:
    runs-on: ubuntu-latest
    timeout-minutes: 45
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: kyoneken/ollama-infra@main
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          # model: leave empty to use pre-installed qwen2.5-coder:1.5b
```

追加シークレット不要。`GITHUB_TOKEN` は Actions が自動提供します。

## Action Inputs

| Input | 必須 | デフォルト | 説明 |
|---|---|---|---|
| `github-token` | ✅ | — | PR コメント投稿用トークン |
| `model` | ❌ | `""` | Ollama モデル名の上書き（省略時: pre-installed の `qwen2.5-coder:1.5b` を使用） |
| `base-ref` | ❌ | `""` | diff 生成の基準ブランチ（省略時: `github.base_ref` を使用） |

## 仕組み

```
PR オープン / 更新
      │
      ▼
GitHub Actions (ubuntu-latest)
      │
      ▼
kyoneken/ollama-infra@main (Composite Action)
      │  ghcr.io/kyoneken/ollama-review:latest を pull
      ▼
ollama serve（モデルはイメージに pre-bake 済み、ダウンロード不要）
      │
      ▼
Ollama REST API (/api/generate)
      │  差分を最大 4000 文字に切り詰め・行番号アノテーションを付与してプロンプト生成
      │  stream:true でトークンを逐次受信
      ▼
レビュー結果 ──► インラインレビューコメントとして各行に投稿
```

## モデルとパフォーマンス

| 項目 | 値 |
|---|---|
| モデル | `qwen2.5-coder:1.5b`（Docker イメージに pre-bake 済み） |
| モデルサイズ | ~1 GB |
| コンテキスト長 | 2048 トークン |
| 推論速度（2 vCPU） | ~2–3 分（1.5b モデル） |
| 差分上限 | 4000 文字（~1000 トークン） |
| CI タイムアウト | 480 秒（curl）/ 45 分（ジョブ全体） |

## レビュー内容

LLM は差分を解析し、以下の 3 カテゴリで問題を検出します：

| カテゴリ | 内容 | 例 |
|---|---|---|
| `TYPOS` | スペルミス・誤字 | 変数名の typo、文字列リテラルの誤り |
| `LOGIC` | ロジックバグ | 境界値エラー、null チェック漏れ、条件式の誤り |
| `COMMENT` | コメントとコードの不一致 | 実装と食い違うドキュメント、古くなったコメント |

### 出力形式

モデルは各指摘を以下のパイプ区切り形式で返します：

```
FILE|LINE|SEVERITY|ISSUE|FIX|REASON_JA
```

- `FILE` — 対象ファイルパス
- `LINE` — 問題のある行番号
- `SEVERITY` — `ERROR` / `WARNING` / `INFO`
- `ISSUE` — 問題の概要（英語）
- `FIX` — 修正案（英語）
- `REASON_JA` — 日本語による理由説明

### インラインコメント

各指摘はプルリクエストの差分ビューに直接インラインコメントとして投稿されます。差分に行番号アノテーションを付与することで、正確な行への紐付けを実現しています。

## Docker イメージ

`ghcr.io/kyoneken/ollama-review:latest`

Dockerfile が更新されると GitHub Actions が自動でビルドして GHCR にプッシュします。

## ローカルでの動作確認

```bash
# イメージを pull
docker pull ghcr.io/kyoneken/ollama-review:latest

# 差分を生成
git diff main...HEAD > pr.diff

# レビュー実行
docker run --rm \
  -v "$(pwd):/workspace" \
  -e REVIEW_OUTPUT=/workspace/review.txt \
  ghcr.io/kyoneken/ollama-review:latest

cat review.txt
```

## キャッシュ戦略

BuildKit の GitHub Actions キャッシュ (`type=gha`) を使用しています。`Dockerfile` または `entrypoint.sh` の変更時のみ再ビルドされます。

## ファイル構成

```
.
├── action.yml              # Composite Action 定義（他リポジトリからの利用エントリポイント）
├── Dockerfile              # Ollama + qwen2.5-coder:1.5b を pre-bake した CI コンテナ
├── entrypoint.sh           # レビュー実行スクリプト（Ollama API 直接呼び出し）
├── demo/ (submodule)       # 動作確認用リポジトリ（kyoneken/ollama-infra-demo）
└── .github/
    └── workflows/
        ├── ai-review.yml       # 自リポジトリの PR レビュー（Composite Action 使用）
        └── docker-publish.yml  # GHCR へのイメージ公開
```

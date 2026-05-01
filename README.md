# ollama-infra

GitHub ActionsでプルリクエストをローカルLLMで自動コードレビューするインフラです。外部のAI APIキー不要で完全オフライン動作します。

## 概要

```
PR オープン / 更新
      │
      ▼
GitHub Actions (ubuntu-latest)
      │
      ▼
Docker ビルド（Ubuntu 22.04 + Ollama + GitHub CLI + Node.js）
      │  ※ Dockerfileが変わらない限りキャッシュヒット
      ▼
ollama serve（モデルはイメージにpre-bake済み、ダウンロード不要）
      │
      ▼
Ollama REST API (/api/generate)
      │  ・差分を最大600文字に切り詰めてプロンプト生成
      │  ・stream:true でトークンを逐次受信
      ▼
レビュー結果  ──►  PRコメントとして投稿（github-script）
```

## 仕組み

1. **トリガー**: PRの `opened` / `synchronize` / `reopened` イベントで `ai-review` ワークフローが起動
2. **差分生成**: `git diff origin/<base>...HEAD` でソースコードの差分のみを取得（Dockerfile等インフラファイルは除外）
3. **Dockerビルド**: BuildKitキャッシュを活用し、`Dockerfile` が変わらない限り数秒でビルド完了
4. **モデル起動**: `qwen2.5-coder:1.5b` はイメージにpre-bake済みのため、`ollama pull` が約0.5秒で完了
5. **レビュー実行**: Ollama REST API を直接呼び出してコードレビューを生成（`stream:true`、最大480秒）
6. **コメント投稿**: `github-script` がレビュー結果をPRコメントとして投稿

## 検出できる問題

- **タイポ**: 識別子・変数名・関数名のスペルミス（例: `resutl` → `result`、`is_palondrome` → `is_palindrome`）
- **コメント不整合**: docstringと実装の乖離（例: docstringが「0を返す」と書いているのに実際は`None`を返す）
- **ロジックエラー**: off-by-oneバグ、nullチェック漏れ、境界条件の誤りなど

## 出力フォーマット

```
ファイルパス|行番号|重要度|問題|修正案
```

重要度: `ERROR`（バグ）/ `WARNING`（潜在的問題）/ `INFO`（品質改善）

## モデルとパフォーマンス

| 項目 | 値 |
|---|---|
| モデル | `qwen2.5-coder:1.5b` |
| モデルサイズ | ~1 GB |
| コンテキスト長 | 512 トークン |
| 推論速度（2 vCPU） | ~1 トークン/秒 |
| 差分上限 | 600 文字（~150トークン） |
| CIタイムアウト | 480秒（curl）/ 45分（ジョブ全体） |

> **注**: `qwen2.5-coder:7b` 等の大きなモデルはGPUランナーがあれば利用可能。CPUのみの環境では1.5b推奨。

## 設定

追加シークレット不要。`GITHUB_TOKEN` はActions が自動提供します。

| 変数 | 用途 |
|---|---|
| `GITHUB_TOKEN` | PRコメント投稿（Actions自動提供） |
| `COPILOT_MODEL` | モデル名の上書き（省略時: `qwen2.5-coder:1.5b`） |

## ローカルでの動作確認

```bash
# イメージをビルド（初回はモデルのダウンロードがあるため数分かかります）
docker build -t ollama-review .

# 差分を生成
git diff main...HEAD > pr.diff

# レビュー実行（結果は review.txt に出力）
docker run --rm \
  -v "$(pwd):/workspace" \
  -e REVIEW_OUTPUT=/workspace/review.txt \
  ollama-review

cat review.txt
```

環境変数で差分を渡すこともできます：

```bash
docker run --rm \
  -e PR_DIFF="$(git diff main...HEAD)" \
  -e REVIEW_OUTPUT=/workspace/review.txt \
  -v "$(pwd):/workspace" \
  ollama-review
```

GPUがある場合（推論が大幅に高速化）：

```bash
docker run --rm --gpus all \
  -v "$(pwd):/workspace" \
  -e REVIEW_OUTPUT=/workspace/review.txt \
  ollama-review
```

## キャッシュ戦略

BuildKitキャッシュのキーは `Dockerfile` のハッシュのみで管理しています。

```
キー: Linux-buildx-v2-<Dockerfileのsha256>
```

`entrypoint.sh` や `.github/workflows/` を変更してもキャッシュは無効化されないため、  
モデルのpre-bakeレイヤーが常に再利用されます。

## ファイル構成

```
.
├── Dockerfile              # Ollama + qwen2.5-coder:1.5b をpre-bakeしたCIコンテナ
├── entrypoint.sh           # レビュー実行スクリプト（Ollama API直接呼び出し）
├── sample/
│   └── calculator.py       # 動作検証用テストコード（意図的なバグを含む）
└── .github/
    └── workflows/
        └── ai-review.yml   # PRトリガーのワークフロー定義
```

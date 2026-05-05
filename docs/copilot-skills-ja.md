# `.copilot/` スキル・エージェント 日本語解説

このリポジトリの `.copilot/` ディレクトリには、GitHub Copilot CLI 用のカスタムエージェントとスキルが定義されています。  
英語で書かれているため、ここに日本語でまとめます。

---

## エージェント

### `code-reviewer`（`.copilot/agents/code-reviewer.md` / `.yml`）

**役割：** PRの差分を受け取り、タイポ・ロジックバグ・コメント不整合の3点を横断的にレビューするエージェント。

**使うスキル：**
- `typo-checker`
- `logic-reviewer`
- `comment-consistency`

**指示内容（要約）：**
1. 差分内のすべてのファイルを行ごとに確認する
2. 識別子・文字列リテラル・コメント内のタイポを検出する（例：`recieve` → `receive`）
3. ロジックエラーを特定する（オフバイワン、null チェック漏れ、誤った演算子など）
4. コメント/ドキュメントとコード実装の食い違いを見つける
5. スタイルの好みや主観的な意見は報告しない

**出力フォーマット（1件あたり）：**
```
FILE: <ファイルパス>
LINE: <行番号>
SEVERITY: <typo | logic | comment>
DESCRIPTION: <問題の簡潔な説明>
SUGGESTION: <具体的な修正案>
```
最後に `SUMMARY: X件の問題がY個のファイルで見つかりました。` を出力する。

---

## スキル

### `typo-checker`（`.copilot/skills/typo-checker.md`）

**役割：** コード内のスペルミスを検出する専門スキル。

**チェック対象：**
| 種別 | 例 |
|------|-----|
| 変数・関数名 | `getUserNmae`, `calcualteTotal`, `isVaild` |
| 文字列リテラル | ユーザー向けメッセージ、ログ、エラーメッセージ |
| コメント・docstring | インラインコメント、ブロックコメント |
| 引数名 | スペルミスのある関数パラメータ |
| 定数名 | `MAX_RETRIESS`, `DEFUALT_TIMEOUT` |

**フラグを立てないもの：**
- コードベース全体で一貫して使われる略語（`ctx`, `cfg`, `req`, `resp` など）
- 確信が持てないドメイン固有の用語
- 自動生成コードやベンダーファイル

**出力フォーマット（1件あたり）：**
```
File:    <ファイルパス>
Line:    <行番号>
Type:    <identifier | string | comment | parameter>
Found:   "<誤ったスペル>"
Suggest: "<正しいスペル>"
Context: <問題の行またはスニペット>
```

---

### `logic-reviewer`（`.copilot/skills/logic-reviewer.md`）

**役割：** 実行時に誤動作・クラッシュ・セキュリティ問題を引き起こす可能性のあるロジックバグを検出するスキル。

**チェックするバグカテゴリ：**

| カテゴリ | 内容 |
|----------|------|
| **オフバイワン** | ループ境界の `<` vs `<=`、配列インデックスの過不足 |
| **null/境界安全** | ポインタ参照前の null チェック漏れ、空スライスの未確認アクセス |
| **制御フロー** | `return` 漏れ、到達不能コード、無限ループリスク |
| **演算子ミス** | `=` vs `==`、`&&` vs `\|\|`、ビット演算子と論理演算子の混同 |
| **エラーハンドリング** | エラーの無視・サイレント破棄、エラー時のパニック |

**出力フォーマット（1件あたり）：**
```
File:     <ファイルパス>
Line:     <行番号>
Category: <off-by-one | null-check | control-flow | operator | error-handling>
Severity: <high | medium | low>
Issue:    <バグの説明>
Code:     <問題のある行>
Suggest:  <修正方法>
```

---

### `comment-consistency`（`.copilot/skills/comment-consistency.md`）

**役割：** コメント・docstring・ドキュメントとコードの実際の動作が一致しているかを監査するスキル。

**チェック対象：**

| 種別 | 内容 |
|------|------|
| **古いコメント** | コードが変わったのにコメントが古いまま |
| **ドキュメント不整合** | 引数名の不一致、戻り値説明の誤り |
| **TODO/FIXME の放置** | 実装済みなのに残っているTODO、解決済みのFIXME |
| **名前と動作のズレ** | `isValid` なのに状態を変更する、`getUser` なのにユーザーを作成する |
| **古いサンプル** | 削除されたAPIや古い構文を使ったコード例 |

**出力フォーマット（1件あたり）：**
```
File:         <ファイルパス>
Line:         <コメントや doc の行番号>
Type:         <outdated-comment | doc-mismatch | todo-drift | misleading-name | stale-example>
Comment says: "<コメント/docが主張していること>"
Code does:    "<コードが実際にやっていること>"
Suggest:      <コメントの修正・削除、または識別子のリネーム>
```

---

## ディレクトリ構成まとめ

```
.copilot/
├── agents/
│   ├── code-reviewer.md   # エージェント定義（Markdown形式）
│   └── code-reviewer.yml  # エージェント定義（YAML形式）
└── skills/
    ├── typo-checker.md        # タイポ検出スキル
    ├── logic-reviewer.md      # ロジックバグ検出スキル
    └── comment-consistency.md # コメント整合性チェックスキル
```

> **補足：** これらのスキルは GitHub Copilot CLI のカスタムエージェント機能で使用します。  
> CI での実際のレビュー実行には `entrypoint.sh` と `action.yml` が使われており、Ollama REST API を直接呼び出します（Copilot CLI は経由しません）。

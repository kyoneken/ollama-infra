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

// --- 行は削除行扱いになる（awk と同じ挙動）
func TestAnnotate_DashDashDash(t *testing.T) {
	input := "--- a/main.go\n+++ b/main.go\n@@ -1,1 +1,1 @@\n-old\n+new\n"
	got := diff.Annotate(input)
	if !strings.Contains(got, "      --- a/main.go") {
		t.Errorf("--- line should be rendered with deletion indent; got:\n%s", got)
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

// maxBytes で正確に切り詰め、通知行が付く
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

// ちょうど maxBytes のとき切り詰めなし
func TestTruncate_ExactLength(t *testing.T) {
	s := "abcde"
	got := diff.Truncate(s, 5)
	if got != s {
		t.Errorf("expected exact length to not truncate, got: %q", got)
	}
}

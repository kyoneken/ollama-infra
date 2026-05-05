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

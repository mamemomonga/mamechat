package httpserver

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	ghtml "github.com/yuin/goldmark/renderer/html"
)

// markdownRenderer はサーバ概要・アプリケーション情報の Markdown を HTML に整形する。
// これらは管理者・ソースコードが管理する信頼済みコンテンツのため、生HTML（WithUnsafe）を
// 許可し、タグの制限は行わない（要件どおり）。
var markdownRenderer = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(ghtml.WithUnsafe()),
)

// renderMarkdown は Markdown 文字列を HTML 文字列へ変換する。失敗時は空文字を返す。
func renderMarkdown(src string) string {
	if src == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := markdownRenderer.Convert([]byte(src), &buf); err != nil {
		return ""
	}
	return buf.String()
}

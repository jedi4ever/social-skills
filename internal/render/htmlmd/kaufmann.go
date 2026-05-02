package htmlmd

import (
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
)

// KaufmannConverter wraps github.com/JohannesKaufmann/html-to-markdown
// (v2). The library is the de facto standard for HTML→Markdown in Go
// — actively maintained, plugin system, much better edge-case
// coverage than the hand-rolled BuiltinConverter (tables,
// strikethrough, definition lists, complex code blocks, etc.).
//
// We use the library's `ConvertString` helper which configures the
// commonmark plugin with sensible defaults. No additional plugin
// wiring needed for the article-extraction use case — the default
// commonmark plugin already covers what BuiltinConverter handles plus
// the missing edge cases.
//
// Errors from the underlying library are converted to an empty string
// to match BuiltinConverter's "best-effort, never panic" contract;
// extraction callers test for empty output and fall back to summary
// text.
type KaufmannConverter struct{}

func (k *KaufmannConverter) Convert(htmlFragment string) string {
	out, err := htmltomarkdown.ConvertString(htmlFragment)
	if err != nil {
		return ""
	}
	return out
}

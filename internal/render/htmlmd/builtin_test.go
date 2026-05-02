package htmlmd

import (
	"strings"
	"testing"
)

// BuiltinConverter has been the default for a long time; these tests
// pin its current output shape so a future tweak to the hand-roll is
// caught immediately. They also document its quirks
// (chrome-stripping, markdown-char escaping) which differ from
// KaufmannConverter's more permissive policy.

func TestBuiltinHeadingsAndParagraphs(t *testing.T) {
	c := &BuiltinConverter{}
	got := c.Convert(`<h1>Title</h1><p>Hello <strong>world</strong>.</p>`)
	if !strings.Contains(got, "# Title") {
		t.Errorf("missing h1: %q", got)
	}
	if !strings.Contains(got, "**world**") {
		t.Errorf("missing bold: %q", got)
	}
}

func TestBuiltinLists(t *testing.T) {
	c := &BuiltinConverter{}
	got := c.Convert(`<ul><li>one</li><li>two</li></ul>`)
	if !strings.Contains(got, "- one") || !strings.Contains(got, "- two") {
		t.Errorf("ul not rendered: %q", got)
	}

	got = c.Convert(`<ol><li>first</li><li>second</li></ol>`)
	if !strings.Contains(got, "1. first") || !strings.Contains(got, "2. second") {
		t.Errorf("ol not rendered: %q", got)
	}
}

func TestBuiltinLinksAndImages(t *testing.T) {
	c := &BuiltinConverter{}
	got := c.Convert(`<p>See <a href="https://example.com">site</a>.</p>`)
	if !strings.Contains(got, "[site](https://example.com)") {
		t.Errorf("link: %q", got)
	}

	got = c.Convert(`<img src="https://example.com/x.png" alt="alt text">`)
	if !strings.Contains(got, "![alt text](https://example.com/x.png)") {
		t.Errorf("image: %q", got)
	}
}

func TestBuiltinBlockquoteAndCode(t *testing.T) {
	c := &BuiltinConverter{}
	got := c.Convert(`<blockquote><p>Quoted</p></blockquote>`)
	if !strings.Contains(got, "> Quoted") {
		t.Errorf("blockquote: %q", got)
	}

	got = c.Convert(`<pre><code>line1
line2</code></pre>`)
	if !strings.Contains(got, "```") || !strings.Contains(got, "line1") {
		t.Errorf("pre: %q", got)
	}
}

// TestBuiltinSkipsScriptAndNav documents the hand-roll's defensive
// stripping of layout chrome. KaufmannConverter is more permissive
// here and would let nav text through — that's why article
// extractors run PickArticleHTML before conversion to isolate the
// article body before either converter sees it.
func TestBuiltinSkipsScriptAndNav(t *testing.T) {
	c := &BuiltinConverter{}
	got := c.Convert(`<p>keep</p><script>drop me</script><nav>menu</nav>`)
	if strings.Contains(got, "drop") || strings.Contains(got, "menu") {
		t.Errorf("did not skip: %q", got)
	}
}

// TestBuiltinEscapesMarkdownChars documents the hand-roll's escaping
// of `*` and `_` in plain text. Kaufmann does context-aware escaping
// so the output of `*stars*` differs (it only escapes the leading
// `*` since the trailing one couldn't form an emphasis on its own).
func TestBuiltinEscapesMarkdownChars(t *testing.T) {
	c := &BuiltinConverter{}
	got := c.Convert(`<p>use *stars* and _underscores_</p>`)
	if !strings.Contains(got, `\*stars\*`) || !strings.Contains(got, `\_underscores\_`) {
		t.Errorf("escapes: %q", got)
	}
}

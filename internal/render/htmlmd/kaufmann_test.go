package htmlmd

import (
	"strings"
	"sync"
	"testing"
)

// KaufmannConverter is the new default. We don't pin its exact
// output (the upstream library may tighten formatting between
// versions); we just confirm core constructs round-trip.

func TestKaufmannBasics(t *testing.T) {
	c := &KaufmannConverter{}
	cases := []struct {
		name string
		html string
		want []string // substrings that must appear
	}{
		{"heading", `<h1>Title</h1>`, []string{"# Title"}},
		{"paragraph", `<p>Hello <strong>world</strong>.</p>`, []string{"Hello", "**world**"}},
		{"unordered list", `<ul><li>one</li><li>two</li></ul>`, []string{"- one", "- two"}},
		{"ordered list", `<ol><li>first</li><li>second</li></ol>`, []string{"1. first", "2. second"}},
		{"link", `<p>See <a href="https://example.com">site</a>.</p>`, []string{"[site](https://example.com)"}},
		{"image", `<img src="https://example.com/x.png" alt="alt text">`, []string{"![alt text](https://example.com/x.png)"}},
		{"blockquote", `<blockquote><p>Quoted</p></blockquote>`, []string{"Quoted"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := c.Convert(tc.html)
			for _, sub := range tc.want {
				if !strings.Contains(got, sub) {
					t.Errorf("missing %q in output: %q", sub, got)
				}
			}
		})
	}
}

// TestKaufmannHandlesTables exercises the case Kaufmann does much
// better than BuiltinConverter — tables. BuiltinConverter would emit
// the raw text inline; Kaufmann formats it as a markdown table.
func TestKaufmannHandlesTables(t *testing.T) {
	c := &KaufmannConverter{}
	got := c.Convert(`<table><thead><tr><th>A</th><th>B</th></tr></thead><tbody><tr><td>1</td><td>2</td></tr></tbody></table>`)
	for _, want := range []string{"A", "B", "1", "2"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in table output: %q", want, got)
		}
	}
}

// TestDefaultConverter exercises the env-driven factory. The cached
// `defaultConverter` is reset between cases so each call re-reads
// the env.
func TestDefaultConverter(t *testing.T) {
	resetDefault := func() {
		defaultConverter = nil
		defaultOnce = sync.Once{}
	}

	t.Setenv("HTML2MD_PROVIDER", "builtin")
	resetDefault()
	if _, ok := Default().(*BuiltinConverter); !ok {
		t.Errorf("HTML2MD_PROVIDER=builtin returned %T, want *BuiltinConverter", Default())
	}

	t.Setenv("HTML2MD_PROVIDER", "kaufmann")
	resetDefault()
	if _, ok := Default().(*KaufmannConverter); !ok {
		t.Errorf("HTML2MD_PROVIDER=kaufmann returned %T, want *KaufmannConverter", Default())
	}

	t.Setenv("HTML2MD_PROVIDER", "")
	resetDefault()
	if _, ok := Default().(*KaufmannConverter); !ok {
		t.Errorf("empty HTML2MD_PROVIDER returned %T, want *KaufmannConverter (default)", Default())
	}

	// Restore for any later tests in the package.
	resetDefault()
}

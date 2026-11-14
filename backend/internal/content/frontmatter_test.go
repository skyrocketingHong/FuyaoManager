package content

import (
	"strings"
	"testing"
)

// TestSplitFrontmatter covers the delimiter grammar byte for byte: LF and
// CRLF endings, an optional UTF-8 BOM, empty frontmatter, an end delimiter
// at EOF, unterminated headers, fake delimiters, and verbatim body
// preservation without any newline normalization.
func TestSplitFrontmatter(t *testing.T) {
	cases := []struct {
		name string
		in   string
		fm   string
		body string
		err  string
	}{
		{name: "lf", in: "---\ntitle: x\n---\nbody", fm: "title: x\n", body: "body"},
		{name: "crlf", in: "---\r\ntitle: x\r\n---\r\nbody", fm: "title: x\r\n", body: "body"},
		{
			name: "bom lf",
			in:   "\xef\xbb\xbf---\ntitle: x\n---\nbody",
			fm:   "title: x\n",
			body: "body",
		},
		{
			name: "bom crlf",
			in:   "\xef\xbb\xbf---\r\ntitle: x\r\n---\r\nbody",
			fm:   "title: x\r\n",
			body: "body",
		},
		{
			name: "no frontmatter keeps everything including bom",
			in:   "\xef\xbb\xbfbody\n---\ntext",
			fm:   "",
			body: "\xef\xbb\xbfbody\n---\ntext",
		},
		{name: "empty frontmatter", in: "---\n---\nbody", fm: "", body: "body"},
		{name: "empty frontmatter crlf", in: "---\r\n---\r\nbody", fm: "", body: "body"},
		{
			name: "end delimiter at eof gives empty body",
			in:   "---\ntitle: x\n---",
			fm:   "title: x\n",
			body: "",
		},
		{
			name: "end delimiter crlf at eof gives empty body",
			in:   "---\ntitle: x\n---\r",
			fm:   "title: x\n",
			body: "",
		},
		{name: "bare start delimiter at eof is unterminated", in: "---", err: "unterminated frontmatter"},
		{
			name: "start line without close is unterminated",
			in:   "---\ntitle: x",
			err:  "unterminated frontmatter",
		},
		{
			name: "suffix text on the end line is not a delimiter",
			in:   "---\ntitle: x\n---extra\nbody",
			err:  "unterminated frontmatter",
		},
		{
			name: "extra dash is not a delimiter",
			in:   "---\ntitle: x\n----\nbody",
			err:  "unterminated frontmatter",
		},
		{
			name: "leading text is not a delimiter",
			in:   "---\ntitle: x\nx---\nbody",
			err:  "unterminated frontmatter",
		},
		{
			name: "trailing space is not a delimiter",
			in:   "---\ntitle: x\n--- \nbody",
			err:  "unterminated frontmatter",
		},
		{
			name: "indented block scalar dashes are not a delimiter",
			in:   "---\ntitle: x\nbody: |\n  ---\n  more\n---\nreal body",
			fm:   "title: x\nbody: |\n  ---\n  more\n",
			body: "real body",
		},
		{
			name: "body blank lines are preserved",
			in:   "---\ntitle: x\n---\n\n\ntext\n",
			fm:   "title: x\n",
			body: "\n\ntext\n",
		},
		{
			name: "mixed newlines in the body are preserved",
			in:   "---\ntitle: x\n---\r\nline1\nline2\r\n",
			fm:   "title: x\n",
			// Only the end line's own CRLF is consumed; the body keeps the
			// rest verbatim.
			body: "line1\nline2\r\n",
		},
		{
			name: "only the end line terminator is consumed",
			in:   "---\ntitle: x\n---\r\nbody",
			fm:   "title: x\n",
			body: "body",
		},
		{
			name: "mid-body dashes never trigger a header",
			in:   "text\n---\nmore",
			fm:   "",
			body: "text\n---\nmore",
		},
		{
			name: "first line with suffix text is not a header",
			in:   "---extra\nbody",
			fm:   "",
			body: "---extra\nbody",
		},
		{
			name: "first line with extra dashes is not a header",
			in:   "----\nbody",
			fm:   "",
			body: "----\nbody",
		},
		{
			name: "bare cr after dashes is not a header",
			in:   "---\rbody",
			fm:   "",
			body: "---\rbody",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fm, body, err := SplitFrontmatter([]byte(tc.in))
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("expected error %q, got %v", tc.err, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(fm) != tc.fm || string(body) != tc.body {
				t.Fatalf("frontmatter %q body %q, want %q / %q", fm, body, tc.fm, tc.body)
			}
		})
	}
}

// TestParsePostFormatVariants proves the projection reads LF, CRLF, and BOM
// documents identically while the digest still describes the complete
// original bytes.
func TestParsePostFormatVariants(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{name: "lf", in: "---\ntitle: T\ndate: 2026-11-12\nlastmod: 2026-11-13\ndraft: false\n---\nbody"},
		{name: "crlf", in: "---\r\ntitle: T\r\ndate: 2026-11-12\r\nlastmod: 2026-11-13\r\ndraft: false\r\n---\r\nbody"},
		{
			name: "bom",
			in:   "\xef\xbb\xbf---\ntitle: T\ndate: 2026-11-12\nlastmod: 2026-11-13\ndraft: false\n---\nbody",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			post, err := ParsePost("tech/x.md", []byte(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if post.Title != "T" || post.Date != "2026-11-12" || post.Lastmod != "2026-11-13" || post.Draft {
				t.Fatalf("projection must match across formats: %+v", post)
			}
			if post.Body != "body" {
				t.Fatalf("body must be verbatim: %q", post.Body)
			}
			if post.Digest != Digest([]byte(tc.in)) {
				t.Fatal("digest must describe the complete original bytes")
			}
		})
	}
}

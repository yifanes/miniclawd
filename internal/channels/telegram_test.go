package channels

import (
	"testing"
)

func TestEscapeMarkdownV2(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain text no special chars",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "backslash in path causes parse error without fix",
			input: `C:\Users\name\file.txt`,
			want:  `C:\\Users\\name\\file\.txt`,
		},
		{
			name:  "fenced code block preserved, content escaped per spec",
			input: "```go\nfmt.Println(`hello`)\n```",
			want:  "```go\nfmt.Println(\\`hello\\`)\n```",
		},
		{
			name:  "fenced code block with backslash inside",
			input: "```\npath = C:\\Users\\\n```",
			want:  "```\npath = C:\\\\Users\\\\\n```",
		},
		{
			name:  "inline code preserved",
			input: "use `os.Exit(1)` to quit",
			want:  "use `os.Exit(1)` to quit",
		},
		{
			name:  "inline code with backslash inside",
			input: "try `C:\\path`",
			want:  "try `C:\\\\path`",
		},
		{
			name:  "special chars in regular text escaped",
			input: "price: 1.5$ (discount: -10%)",
			want:  `price: 1\.5$ \(discount: \-10%\)`,
		},
		{
			name:  "bold markdown becomes escaped literal",
			input: "**bold** and _italic_",
			want:  `\*\*bold\*\* and \_italic\_`,
		},
		{
			name:  "mixed: code block and regular text",
			input: "run this:\n```bash\necho hello\n```\ndone.",
			want:  "run this:\n```bash\necho hello\n```\ndone\\.",
		},
		{
			name:  "unclosed fenced code block: backticks escaped",
			input: "```unclosed",
			want:  "\\`\\`\\`unclosed",
		},
		{
			name:  "unclosed inline code: backtick escaped",
			input: "price `foo",
			want:  "price \\`foo",
		},
		{
			name:  "dot escaped",
			input: "version 1.2.3",
			want:  `version 1\.2\.3`,
		},
		{
			name:  "dash escaped",
			input: "bullet - item",
			want:  `bullet \- item`,
		},
		{
			name:  "exclamation mark escaped",
			input: "hello!",
			want:  `hello\!`,
		},
		{
			name:  "url in regular text",
			input: "see https://example.com/path?a=1&b=2",
			want:  `see https://example\.com/path?a\=1&b\=2`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := escapeMarkdownV2(tc.input)
			if got != tc.want {
				t.Errorf("\ninput: %q\n  got: %q\n want: %q", tc.input, got, tc.want)
			}
		})
	}
}

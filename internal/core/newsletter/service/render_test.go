package service

import "testing"

func TestStripHTML(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"## Hi\n\nRead [more](https://x.test).": "## Hi\n\nRead [more](https://x.test).",
		"<script>alert(1)</script>Text":         "Text",
		"a <b>bold</b> <STYLE>p{}</style>word":  "a bold word",
		"1 < 2 and 3 > 2":                       "1 < 2 and 3 > 2",
	}

	for in, want := range cases {
		if got := stripHTML(in); got != want {
			t.Errorf("stripHTML(%q) = %q, want %q", in, got, want)
		}
	}
}

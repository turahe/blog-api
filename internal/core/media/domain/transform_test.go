package domain

import (
	"testing"
)

func TestParseVariants(t *testing.T) {
	t.Parallel()

	got, err := ParseVariants([]string{"thumbnail:320:webp", " card:640 "})
	if err != nil {
		t.Fatal(err)
	}

	want := []Variant{{Name: "thumbnail", Width: 320, Format: FormatWebP}, {Name: "card", Width: 640}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v", got)
	}

	for _, bad := range [][]string{
		{"thumb"},
		{"Thumb:320"},
		{"thumb:8"},
		{"thumb:5000"},
		{"thumb:320:tiff"},
		{"thumb:320:webp:extra"},
		{"a:320", "a:640"},
	} {
		if _, err := ParseVariants(bad); err == nil {
			t.Fatalf("%v: expected an error", bad)
		}
	}
}

func TestTransformPolicyApplyKeepsExplicitValues(t *testing.T) {
	t.Parallel()

	p := TransformPolicy{Quality: 80, Format: FormatWebP}

	if got := p.Apply(Transform{Width: 100}); got != (Transform{Width: 100, Format: FormatWebP, Quality: 80}) {
		t.Fatalf("defaults not applied: %+v", got)
	}

	explicit := Transform{Width: 100, Format: FormatPNG, Quality: 50}
	if got := p.Apply(explicit); got != explicit {
		t.Fatalf("explicit values overridden: %+v", got)
	}
}

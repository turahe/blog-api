package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Output formats a transform can convert to.
const (
	FormatWebP = "webp"
	FormatAVIF = "avif"
	FormatJPEG = "jpeg"
	FormatPNG  = "png"

	// FormatOriginal is the policy value that keeps the source format.
	FormatOriginal = "original"
)

// Variant width bounds.
const (
	MinVariantWidth = 16
	MaxVariantWidth = 4096
)

// Transform resizes an image to Width, keeping its aspect ratio and never enlarging it.
// An empty Format keeps the source format; a zero Quality leaves the encoder default.
type Transform struct {
	Width   int
	Format  string
	Quality int
}

// TransformableTypes are the source content types a transform accepts. SVG is excluded:
// it is not raster and can carry scripts.
var TransformableTypes = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/gif":  {},
	"image/webp": {},
	"image/avif": {},
}

// TransformFormats are the accepted Format values.
var TransformFormats = map[string]struct{}{
	FormatWebP: {},
	FormatAVIF: {},
	FormatJPEG: {},
	FormatPNG:  {},
}

// Variant is a named resize preset such as thumbnail or card.
type Variant struct {
	Name   string
	Width  int
	Format string
}

var variantPattern = regexp.MustCompile(`^([a-z][a-z0-9_]{0,31}):([0-9]{1,4})(?::([a-z]+))?$`)

// ParseVariant reads a "name:width[:format]" preset.
func ParseVariant(spec string) (Variant, error) {
	m := variantPattern.FindStringSubmatch(strings.TrimSpace(spec))
	if m == nil {
		return Variant{}, fmt.Errorf("variant %q must look like name:width or name:width:format", spec)
	}

	width, _ := strconv.Atoi(m[2])
	if width < MinVariantWidth || width > MaxVariantWidth {
		return Variant{}, fmt.Errorf("variant %q width must be between %d and %d", spec, MinVariantWidth, MaxVariantWidth)
	}

	if _, ok := TransformFormats[m[3]]; m[3] != "" && !ok {
		return Variant{}, fmt.Errorf("variant %q format must be webp, avif, jpeg, or png", spec)
	}

	return Variant{Name: m[1], Width: width, Format: m[3]}, nil
}

// ParseVariants reads presets and rejects repeated names.
func ParseVariants(specs []string) ([]Variant, error) {
	out := make([]Variant, 0, len(specs))
	seen := make(map[string]struct{}, len(specs))

	for _, spec := range specs {
		v, err := ParseVariant(spec)
		if err != nil {
			return nil, err
		}

		if _, dup := seen[v.Name]; dup {
			return nil, fmt.Errorf("variant name %q is repeated", v.Name)
		}

		seen[v.Name] = struct{}{}

		out = append(out, v)
	}

	return out, nil
}

// TransformPolicy is the admin-set transform behaviour: named presets, the output quality,
// and the format used when a request or preset names none (empty keeps the source format).
type TransformPolicy struct {
	Variants []Variant
	Quality  int
	Format   string
}

// Apply fills the policy defaults into t.
func (p TransformPolicy) Apply(t Transform) Transform {
	if t.Format == "" {
		t.Format = p.Format
	}

	if t.Quality == 0 {
		t.Quality = p.Quality
	}

	return t
}

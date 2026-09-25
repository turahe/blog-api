package domain

// Output formats a transform can convert to.
const (
	FormatWebP = "webp"
	FormatAVIF = "avif"
	FormatJPEG = "jpeg"
	FormatPNG  = "png"
)

// Transform resizes an image to Width, keeping its aspect ratio and never enlarging it.
// An empty Format keeps the source format.
type Transform struct {
	Width  int
	Format string
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

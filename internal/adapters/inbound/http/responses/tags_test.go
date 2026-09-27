package responses

import (
	"testing"
	"time"

	"github.com/google/uuid"
	tagdomain "github.com/turahe/blog-api/internal/core/tag/domain"
)

func TestTag(t *testing.T) {
	t.Parallel()

	tag := tagdomain.Tag{
		ID:        5,
		UUID:      uuid.MustParse("0198a1b2-0000-7000-8000-000000008001"),
		Name:      "Go Generics",
		Slug:      "go-generics",
		CreatedAt: time.Date(2026, 8, 1, 17, 0, 0, 0, time.FixedZone("WIB", 7*60*60)),
	}

	assertJSON(t, `{"id": "0198a1b2-0000-7000-8000-000000008001", "name": "Go Generics", "slug": "go-generics",
		"createdAt": "2026-08-01T10:00:00Z"}`, Tag(tag))
}

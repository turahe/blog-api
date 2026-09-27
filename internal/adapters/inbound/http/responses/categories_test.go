package responses

import (
	"testing"
	"time"

	"github.com/google/uuid"
	categorydomain "github.com/turahe/blog-api/internal/core/category/domain"
)

func TestCategory(t *testing.T) {
	t.Parallel()

	parent := uuid.MustParse("0198a1b2-0000-7000-8000-00000000c002")
	image := uuid.MustParse("0198a1b2-0000-7000-8000-00000000c003")
	base := categorydomain.Category{
		ID:          7,
		UUID:        uuid.MustParse("0198a1b2-0000-7000-8000-00000000c001"),
		Name:        "Go",
		Slug:        "go",
		Description: "Gophers",
		Lft:         2,
		Rgt:         3,
		Depth:       1,
		SortOrder:   4,
		CreatedAt:   time.Date(2026, 8, 1, 17, 0, 0, 0, time.FixedZone("WIB", 7*60*60)),
		UpdatedAt:   time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC),
	}

	withRefs := base
	withRefs.ParentUUID, withRefs.ImageUUID = &parent, &image

	const common = `"id": "0198a1b2-0000-7000-8000-00000000c001", "name": "Go", "slug": "go", "description": "Gophers",
		"lft": 2, "rgt": 3, "depth": 1, "sortOrder": 4,
		"createdAt": "2026-08-01T10:00:00Z", "updatedAt": "2026-08-02T10:00:00Z"`

	tests := []struct {
		name     string
		category categorydomain.Category
		want     string
	}{
		{name: "root without image", category: base, want: `{` + common + `, "parentId": null, "imageId": null}`},
		{
			name:     "child with image",
			category: withRefs,
			want: `{` + common + `, "parentId": "0198a1b2-0000-7000-8000-00000000c002",
				"imageId": "0198a1b2-0000-7000-8000-00000000c003"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertJSON(t, tt.want, Category(tt.category))
		})
	}
}

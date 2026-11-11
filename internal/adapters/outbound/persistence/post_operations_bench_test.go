package persistence

import (
	"context"
	"testing"

	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

// BenchmarkListPublished_FirstPage benchmarks ListPublished on page 1 with 20 items per page.
// Measures: full end-to-end operation including database query, cursor encoding, pagination.
func BenchmarkListPublished_FirstPage(b *testing.B) {
	benchmarkListPublished(b, 1, 20, false)
}

// BenchmarkListPublished_Page50 benchmarks ListPublished at page 50 (offset 980).
// Measures: offset pagination performance at mid-range position.
func BenchmarkListPublished_Page50(b *testing.B) {
	benchmarkListPublished(b, 50, 20, false)
}

// BenchmarkListPublished_Page100 benchmarks ListPublished at page 100 (offset 1980).
// Measures: offset pagination performance at deeper position.
func BenchmarkListPublished_Page100(b *testing.B) {
	benchmarkListPublished(b, 100, 20, false)
}

// BenchmarkListPublished_Page500 benchmarks ListPublished at page 500 (offset 9980).
// Measures: offset pagination performance at very deep position (performance degrades).
func BenchmarkListPublished_Page500(b *testing.B) {
	benchmarkListPublished(b, 500, 20, false)
}

// BenchmarkListPublished_Page1000 benchmarks ListPublished at page 1000 (offset 19980).
// Measures: extreme pagination position to show offset query planner burden.
func BenchmarkListPublished_Page1000(b *testing.B) {
	benchmarkListPublished(b, 1000, 20, false)
}

// BenchmarkListPublished_WithTotalCount benchmarks ListPublished with IncludeTotal=true.
// Measures: additional COUNT(*) overhead on first page.
func BenchmarkListPublished_WithTotalCount(b *testing.B) {
	benchmarkListPublished(b, 1, 20, true)
}

// BenchmarkListPublished_CursorPage1 benchmarks ListPublished with cursor pagination at position 1.
// Measures: cursor decode/encode overhead vs offset.
func BenchmarkListPublished_CursorPage1(b *testing.B) {
	seed := benchmarkDB(b)
	ctx := context.Background()
	repo := NewPostRepository(seed.db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		filter := postdomain.ListFilter{
			PageRequest: pagination.PageRequest{
				Mode:         pagination.ModeCursor,
				Forward:      true,
				Limit:        20,
				Cursor:       seed.postRow1Cursor,
				IncludeTotal: false,
			},
			CategoryUUID: &seed.postCategory,
		}
		_, _ = repo.ListPublished(ctx, filter)
	}
}

// BenchmarkListPublished_CursorPage50000 benchmarks ListPublished with cursor pagination at deep position.
// Measures: cursor pagination consistency vs offset drift at deep range.
func BenchmarkListPublished_CursorPage50000(b *testing.B) {
	seed := benchmarkDB(b)
	ctx := context.Background()
	repo := NewPostRepository(seed.db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		filter := postdomain.ListFilter{
			PageRequest: pagination.PageRequest{
				Mode:         pagination.ModeCursor,
				Forward:      true,
				Limit:        20,
				Cursor:       seed.postRow49999Cursor,
				IncludeTotal: false,
			},
			CategoryUUID: &seed.postCategory,
		}
		_, _ = repo.ListPublished(ctx, filter)
	}
}

// BenchmarkListPublished_LargePageSize benchmarks ListPublished with large page size (100 items).
// Measures: memory allocation and scanning overhead with larger result sets.
func BenchmarkListPublished_LargePageSize(b *testing.B) {
	benchmarkListPublished(b, 1, 100, false)
}

// BenchmarkListPublished_SmallPageSize benchmarks ListPublished with small page size (5 items).
// Measures: baseline overhead per-query with minimal result set.
func BenchmarkListPublished_SmallPageSize(b *testing.B) {
	benchmarkListPublished(b, 1, 5, false)
}

func benchmarkListPublished(b *testing.B, page, pageSize int, includeTotal bool) {
	seed := benchmarkDB(b)
	ctx := context.Background()
	repo := NewPostRepository(seed.db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		filter := postdomain.ListFilter{
			PageRequest: pagination.PageRequest{
				Mode:         pagination.ModeOffset,
				Page:         page,
				Limit:        pageSize,
				IncludeTotal: includeTotal,
			},
			CategoryUUID: &seed.postCategory,
		}
		_, _ = repo.ListPublished(ctx, filter)
	}
}

// =============================
// SEARCH BENCHMARKS
// =============================

// BenchmarkSearchPublished_SingleTerm benchmarks SearchPublished with a single-term query.
// Measures: two-phase search (rank + hydration) with simple query.
func BenchmarkSearchPublished_SingleTerm(b *testing.B) {
	benchmarkSearchPublished(b, "ipsum", 1, 20, false)
}

// BenchmarkSearchPublished_ThreeTerms benchmarks SearchPublished with a three-term query.
// Measures: more complex ts_rank calculation and result set overhead.
func BenchmarkSearchPublished_ThreeTerms(b *testing.B) {
	benchmarkSearchPublished(b, "lorem ipsum dolor", 1, 20, false)
}

// BenchmarkSearchPublished_ComplexQuery benchmarks SearchPublished with a complex multi-term query.
// Measures: ts_rank complexity and ts_headline highlighting overhead.
func BenchmarkSearchPublished_ComplexQuery(b *testing.B) {
	benchmarkSearchPublished(b, "lorem | ipsum & dolor & sit", 1, 20, false)
}

// BenchmarkSearchPublished_Page50 benchmarks SearchPublished at page 50.
// Measures: offset pagination performance in search results.
func BenchmarkSearchPublished_Page50(b *testing.B) {
	benchmarkSearchPublished(b, "ipsum", 50, 20, false)
}

// BenchmarkSearchPublished_Page100 benchmarks SearchPublished at page 100.
// Measures: deep pagination performance in search results.
func BenchmarkSearchPublished_Page100(b *testing.B) {
	benchmarkSearchPublished(b, "ipsum", 100, 20, false)
}

// BenchmarkSearchPublished_Page500 benchmarks SearchPublished at page 500.
// Measures: very deep pagination in search results.
func BenchmarkSearchPublished_Page500(b *testing.B) {
	benchmarkSearchPublished(b, "ipsum", 500, 20, false)
}

// BenchmarkSearchPublished_WithTotalCount benchmarks SearchPublished with IncludeTotal=true.
// Measures: COUNT(*) overhead in search results.
func BenchmarkSearchPublished_WithTotalCount(b *testing.B) {
	benchmarkSearchPublished(b, "ipsum", 1, 20, true)
}

// BenchmarkSearchPublished_LargePageSize benchmarks SearchPublished with 100 items per page.
// Measures: memory overhead and ts_headline computation for large result sets.
func BenchmarkSearchPublished_LargePageSize(b *testing.B) {
	benchmarkSearchPublished(b, "ipsum", 1, 100, false)
}

// BenchmarkSearchPublished_SmallPageSize benchmarks SearchPublished with 5 items per page.
// Measures: baseline search overhead with minimal result set.
func BenchmarkSearchPublished_SmallPageSize(b *testing.B) {
	benchmarkSearchPublished(b, "ipsum", 1, 5, false)
}

// BenchmarkSearchPublished_CursorPage1 benchmarks SearchPublished with cursor pagination.
// Measures: cursor overhead vs offset in search results.
func BenchmarkSearchPublished_CursorPage1(b *testing.B) {
	seed := benchmarkDB(b)
	ctx := context.Background()
	search := NewPostSearch(seed.db, "english")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		filter := postdomain.SearchFilter{
			Query: "ipsum",
			PageRequest: pagination.PageRequest{
				Mode:         pagination.ModeCursor,
				Forward:      true,
				Limit:        20,
				Cursor:       "", // Start from beginning
				IncludeTotal: false,
			},
		}
		_, _ = search.SearchPublished(ctx, filter)
	}
}

// BenchmarkSearchPublished_NoResults benchmarks search with no matching results.
// Measures: baseline overhead when query returns no results.
func BenchmarkSearchPublished_NoResults(b *testing.B) {
	benchmarkSearchPublished(b, "xyzabc123notfound", 1, 20, false)
}

func benchmarkSearchPublished(b *testing.B, query string, page, pageSize int, includeTotal bool) {
	seed := benchmarkDB(b)
	ctx := context.Background()
	search := NewPostSearch(seed.db, "english")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		filter := postdomain.SearchFilter{
			Query: query,
			PageRequest: pagination.PageRequest{
				Mode:         pagination.ModeOffset,
				Page:         page,
				Limit:        pageSize,
				IncludeTotal: includeTotal,
			},
		}
		_, _ = search.SearchPublished(ctx, filter)
	}
}

// =============================
// COMPARISON: Cursor vs Offset Drift
// =============================

// BenchmarkListPublished_OffsetVsCursor_Position1 compares offset vs cursor at position 1.
// Sub-benchmarks allow direct ratio comparison: BenchmarkListPublished_OffsetVsCursor_Position1/Offset vs Position1/Cursor.
func BenchmarkListPublished_OffsetVsCursor_Position1(b *testing.B) {
	seed := benchmarkDB(b)
	ctx := context.Background()
	repo := NewPostRepository(seed.db)

	b.Run("Offset", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			filter := postdomain.ListFilter{
				PageRequest: pagination.PageRequest{
					Mode:         pagination.ModeOffset,
					Page:         1,
					Limit:        50,
					IncludeTotal: false,
				},
				CategoryUUID: &seed.postCategory,
			}
			_, _ = repo.ListPublished(ctx, filter)
		}
	})

	b.Run("Cursor", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			filter := postdomain.ListFilter{
				PageRequest: pagination.PageRequest{
					Mode:         pagination.ModeCursor,
					Forward:      true,
					Limit:        50,
					Cursor:       seed.postRow1Cursor,
					IncludeTotal: false,
				},
				CategoryUUID: &seed.postCategory,
			}
			_, _ = repo.ListPublished(ctx, filter)
		}
	})
}

// BenchmarkListPublished_OffsetVsCursor_Position50000 compares offset vs cursor at deep position (row 50k).
// Shows performance divergence at scale; cursor should remain O(log n) while offset degrades to O(n).
func BenchmarkListPublished_OffsetVsCursor_Position50000(b *testing.B) {
	seed := benchmarkDB(b)
	ctx := context.Background()
	repo := NewPostRepository(seed.db)

	b.Run("Offset", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			filter := postdomain.ListFilter{
				PageRequest: pagination.PageRequest{
					Mode:         pagination.ModeOffset,
					Page:         1000, // offset = 49950
					Limit:        50,
					IncludeTotal: false,
				},
				CategoryUUID: &seed.postCategory,
			}
			_, _ = repo.ListPublished(ctx, filter)
		}
	})

	b.Run("Cursor", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			filter := postdomain.ListFilter{
				PageRequest: pagination.PageRequest{
					Mode:         pagination.ModeCursor,
					Forward:      true,
					Limit:        50,
					Cursor:       seed.postRow49999Cursor,
					IncludeTotal: false,
				},
				CategoryUUID: &seed.postCategory,
			}
			_, _ = repo.ListPublished(ctx, filter)
		}
	})
}

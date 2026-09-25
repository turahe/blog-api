package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
)

type fakeSearcher struct {
	calls  int
	filter postdomain.SearchFilter
}

func (f *fakeSearcher) SearchPublished(_ context.Context, filter postdomain.SearchFilter) (postdomain.SearchResult, error) {
	f.calls++
	f.filter = filter

	return postdomain.SearchResult{Page: filter.Page, PerPage: filter.PerPage}, nil
}

func TestSearchNormalizesTheFilter(t *testing.T) {
	t.Parallel()

	searcher := &fakeSearcher{}
	svc := New(nil, nil, nil).WithSearch(searcher)

	got, err := svc.Search(t.Context(), postdomain.SearchFilter{Query: "  go \t generics\n", Page: 0, PerPage: 500})
	require.NoError(t, err)
	require.Equal(t, "go generics", searcher.filter.Query)
	require.Equal(t, 1, got.Page)
	require.Equal(t, 20, got.PerPage)
}

func TestSearchRejectsEmptyAndOverlongQueries(t *testing.T) {
	t.Parallel()

	searcher := &fakeSearcher{}
	svc := New(nil, nil, nil).WithSearch(searcher)

	_, err := svc.Search(t.Context(), postdomain.SearchFilter{Query: " \t "})
	require.ErrorIs(t, err, ErrValidation)

	_, err = svc.Search(t.Context(), postdomain.SearchFilter{Query: strings.Repeat("é", postdomain.MaxSearchQueryLength+1)})
	require.ErrorIs(t, err, ErrValidation)

	_, err = svc.Search(t.Context(), postdomain.SearchFilter{Query: strings.Repeat("é", postdomain.MaxSearchQueryLength)})
	require.NoError(t, err, "the limit counts characters, not bytes")
	require.Equal(t, 1, searcher.calls)
}

func TestSearchWithoutBackendIsUnavailable(t *testing.T) {
	t.Parallel()

	_, err := New(nil, nil, nil).Search(t.Context(), postdomain.SearchFilter{Query: "go"})
	require.ErrorIs(t, err, postdomain.ErrSearchUnavailable)
}

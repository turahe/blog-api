package domain

import (
	"errors"

	"github.com/google/uuid"
)

// MaxSearchQueryLength caps a search query in characters.
const MaxSearchQueryLength = 200

// ErrSearchUnavailable means no search backend is configured.
var ErrSearchUnavailable = errors.New("post search unavailable")

// SearchFilter selects and pages a full-text search over published posts.
type SearchFilter struct {
	Query        string
	Page         int
	PerPage      int
	CategoryUUID *uuid.UUID
	TagUUID      *uuid.UUID
}

// SearchHit is a matching post with its relevance and highlighted fragments. Title and
// Snippet are HTML-escaped text in which only the matched terms are wrapped in <mark>.
type SearchHit struct {
	Post    Post
	Rank    float64
	Title   string
	Snippet string
}

// SearchResult is a page of search hits, best match first.
type SearchResult struct {
	Items   []SearchHit
	Total   int64
	Page    int
	PerPage int
}

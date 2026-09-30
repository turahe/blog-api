package handlers

import (
	"errors"
	nethttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

// postPublicCfg declares the pagination contract used by the public post list.
// Duplicated here (from service and repo) rather than exported from the
// service package to keep HTTP wiring free of repository-specific imports.
var postPublicCfg = pagination.CursorConfig{
	Kind: "posts_public",
	Sort: []pagination.SortField{
		{Name: "published_at", Column: "posts.published_at", Dir: pagination.Desc, Nulls: pagination.NullsLast, Type: pagination.TypeTime},
		{Name: "created_at", Column: "posts.created_at", Dir: pagination.Desc, Type: pagination.TypeTime},
		{Name: "id", Column: "posts.id", Dir: pagination.Desc, Type: pagination.TypeInt64},
	},
	TTL:            pagination.DefaultTTL,
	MaxPerPage:     pagination.DefaultMaxPerPage,
	DefaultPerPage: pagination.DefaultPerPage,
}

// postSearchCfg declares the pagination contract used by the public post search.
// Sort: search rank DESC, published_at DESC NULLS LAST, id DESC provides
// deterministic ordering required for keyset (cursor) pagination.
var postSearchCfg = pagination.CursorConfig{
	Kind: "posts_search",
	Sort: []pagination.SortField{
		{Name: "rank", Dir: pagination.Desc, Type: pagination.TypeFloat64},
		{Name: "published_at", Column: "posts.published_at", Dir: pagination.Desc, Nulls: pagination.NullsLast, Type: pagination.TypeTime},
		{Name: "id", Column: "posts.id", Dir: pagination.Desc, Type: pagination.TypeInt64},
	},
	TTL:            pagination.DefaultTTL,
	MaxPerPage:     pagination.DefaultMaxPerPage,
	DefaultPerPage: pagination.DefaultPerPage,
}

// listPublishedPostsHandler godoc
//
//	@Summary		List published posts
//	@Description	Newest first (published_at DESC, created_at DESC, id DESC). Supports both legacy offset
//	@Description	pagination (page/perPage) and cursor-based keyset pagination (after/before/limit).
//	@Description	With q, runs a full-text search instead: best match first, and each item
//	@Description	gains search.rank, search.title, and search.snippet (HTML-escaped, matches in <mark>).
//	@Description	q uses web search syntax: "quoted phrase", or, -excluded.
//	@Tags			public
//	@Produce		json
//	@Param			q			query		string	false	"full-text search query (max 200 characters)"
//	@Param			page		query		int		false	"page (legacy offset mode)"			default(1)
//	@Param			perPage		query		int		false	"per page (legacy offset mode, alias limit)"	default(20)
//	@Param			limit		query		int		false	"page size (cursor or offset)"		default(20)
//	@Param			after		query		string	false	"opaque cursor: return items after this point"
//	@Param			before		query		string	false	"opaque cursor: return items before this point"
//	@Param			includeTotal	query		bool	false	"when false, skip COUNT(*) to reduce DB load"	default(true)
//	@Param			categoryId	query		string	false	"category UUID"
//	@Param			tagId		query		string	false	"tag UUID"
//	@Success		200			{object}	responses.Envelope
//	@Failure		400			{object}	responses.Envelope
//	@Failure		503			{object}	responses.Envelope	"search.unavailable"
//	@Router			/api/v1/posts [get]
func listPublishedPostsHandler(posts *postservice.PostService) gin.HandlerFunc {
	return func(c *gin.Context) {
		categoryID, err := postservice.ParseOptionalUUID(c.Query("categoryId"))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid categoryId")
			return
		}

		tagID, err := postservice.ParseOptionalUUID(c.Query("tagId"))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid tagId")
			return
		}

		if query := strings.TrimSpace(c.Query("q")); query != "" {
			pr, err := pagination.ParseRequest(c, postSearchCfg)
			if err != nil {
				responses.Failure(c, nethttp.StatusBadRequest, pagination.ErrorCode(err), pagination.ErrorCause(err))
				return
			}
			searchPublishedPosts(c, posts, postdomain.SearchFilter{
				Query:        query,
				CategoryUUID: categoryID,
				TagUUID:      tagID,
				PageRequest:  pr,
			})

			return
		}

		pr, err := pagination.ParseRequest(c, postPublicCfg)
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, pagination.ErrorCode(err), pagination.ErrorCause(err))
			return
		}

		filter := postdomain.ListFilter{CategoryUUID: categoryID, TagUUID: tagID}
		filter.PageRequest = pr

		result, err := posts.ListPublished(c.Request.Context(), filter)
		if err != nil {
			switch {
			case errors.Is(err, pagination.ErrCursorMalformed),
				errors.Is(err, pagination.ErrCursorInvalidSignature),
				errors.Is(err, pagination.ErrCursorExpired),
				errors.Is(err, pagination.ErrCursorWrongKind),
				errors.Is(err, pagination.ErrCursorMissingField),
				errors.Is(err, pagination.ErrCursorFieldType),
				errors.Is(err, pagination.ErrCursorUnsupported):
				responses.Failure(c, nethttp.StatusBadRequest, pagination.ErrorCode(err), pagination.ErrorCause(err))
			default:
				responses.Internal(c, err, "Failed to list posts")
			}
			return
		}

		items := make([]gin.H, 0, len(result.Items))
		for _, post := range result.Items {
			items = append(items, responses.Post(post))
		}

		responses.SuccessPaginatedResult[postdomain.Post](c, nethttp.StatusOK, responses.CursorPageOpts[postdomain.Post]{
			Service: responses.ServicePosts,
			Result:  result,
			Data:    items,
		})
	}
}

func searchPublishedPosts(c *gin.Context, posts *postservice.PostService, filter postdomain.SearchFilter) {
	result, err := posts.Search(c.Request.Context(), filter)

	switch {
	case errors.Is(err, postservice.ErrValidation):
		responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, err.Error())
		return
	case errors.Is(err, postdomain.ErrSearchUnavailable):
		responses.Failure(c, nethttp.StatusServiceUnavailable, "search.unavailable", "Search is not available")
		return
	case err != nil:
		responses.Internal(c, err, "Failed to search posts")
		return
	}

	items := make([]gin.H, 0, len(result.Items))
	for _, hit := range result.Items {
		items = append(items, responses.PostSearchHit(hit))
	}

	responses.SuccessPaginatedResult[postdomain.SearchHit](c, nethttp.StatusOK, responses.CursorPageOpts[postdomain.SearchHit]{
		Service: responses.ServicePosts,
		Result:  result,
		Data:    items,
	})
}

// getPublishedPostHandler godoc
//
//	@Summary	Get published post by slug
//	@Tags		public
//	@Produce	json
//	@Param		param1	path		string	true	"post slug"
//	@Success	200		{object}	responses.Envelope
//	@Failure	404		{object}	responses.Envelope
//	@Router		/api/v1/posts/{param1} [get]
func getPublishedPostHandler(posts *postservice.PostService) gin.HandlerFunc {
	return func(c *gin.Context) {
		slug := strings.TrimSpace(c.Param("param1"))

		post, err := posts.GetPublishedBySlug(c.Request.Context(), slug)
		if errors.Is(err, postdomain.ErrNotFound) {
			responses.Failure(c, nethttp.StatusNotFound, responses.ErrorCodeNotFound, "Post not found")
			return
		}

		if err != nil {
			responses.Internal(c, err, "Failed to load post")
			return
		}

		responses.Success(c, nethttp.StatusOK, responses.Post(post))
	}
}

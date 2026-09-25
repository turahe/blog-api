package handlers

import (
	"errors"
	nethttp "net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
)

// listPublishedPostsHandler godoc
//
//	@Summary		List published posts
//	@Description	Newest first. With q, runs a full-text search instead: best match first, and each item
//	@Description	gains search.rank, search.title, and search.snippet (HTML-escaped, matches in <mark>).
//	@Description	q uses web search syntax: "quoted phrase", or, -excluded.
//	@Tags			public
//	@Produce		json
//	@Param			q			query		string	false	"full-text search query (max 200 characters)"
//	@Param			page		query		int		false	"page"		default(1)
//	@Param			per_page	query		int		false	"per page"	default(20)
//	@Param			category_id	query		string	false	"category UUID"
//	@Param			tag_id		query		string	false	"tag UUID"
//	@Success		200			{object}	responses.Envelope
//	@Failure		400			{object}	responses.Envelope
//	@Failure		503			{object}	responses.Envelope	"search.unavailable"
//	@Router			/api/v1/posts [get]
func listPublishedPostsHandler(posts *postservice.PostService) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))

		categoryID, err := postservice.ParseOptionalUUID(c.Query("category_id"))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid category_id")
			return
		}

		tagID, err := postservice.ParseOptionalUUID(c.Query("tag_id"))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Invalid tag_id")
			return
		}

		if query := strings.TrimSpace(c.Query("q")); query != "" {
			searchPublishedPosts(c, posts, postdomain.SearchFilter{
				Query: query, Page: page, PerPage: perPage, CategoryUUID: categoryID, TagUUID: tagID,
			})

			return
		}

		result, err := posts.ListPublished(c.Request.Context(), postdomain.ListFilter{
			Page: page, PerPage: perPage, CategoryUUID: categoryID, TagUUID: tagID,
		})
		if err != nil {
			responses.Internal(c, err, "Failed to list posts")
			return
		}

		items := make([]gin.H, 0, len(result.Items))
		for _, post := range result.Items {
			items = append(items, responses.Post(post))
		}

		responses.SuccessPaginatedFor(c, nethttp.StatusOK, responses.PageOpts{
			Service: responses.ServicePosts,
			Data:    items,
			Page:    result.Page,
			PerPage: result.PerPage,
			Total:   result.Total,
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

	responses.SuccessPaginatedFor(c, nethttp.StatusOK, responses.PageOpts{
		Service: responses.ServicePosts,
		Data:    items,
		Page:    result.Page,
		PerPage: result.PerPage,
		Total:   result.Total,
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

package handlers

import (
	"context"
	nethttp "net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
)

type postRevisionsAPI interface {
	ListRevisions(ctx context.Context, viewerID uuid.UUID, unrestricted bool, filter postdomain.RevisionFilter) (postdomain.RevisionPage, error)
	GetRevision(ctx context.Context, postID uuid.UUID, ref postdomain.RevisionRef, viewerID uuid.UUID, unrestricted bool) (postdomain.Revision, error)
	RestoreRevision(ctx context.Context, postID uuid.UUID, ref postdomain.RevisionRef, actorID uuid.UUID, unrestricted bool, note string) (postservice.RestoreResult, error)
}

// allPosts reports whether the caller may see and restore revisions of posts they do not author.
type allPosts func(c *gin.Context) bool

// adminListPostRevisionsHandler godoc
//
//	@Summary		List post revisions
//	@Description	Newest first. Authors see only their own posts' history; editors and admins (post.revisions.view_all) see every post's.
//	@Tags			admin
//	@Produce		json
//	@Param			param1			path		string	true	"post UUID"
//	@Param			page			query		int		false	"page (default 1)"
//	@Param			per_page		query		int		false	"page size (default 20, max 100)"
//	@Param			author_id		query		string	false	"only revisions by this user UUID"
//	@Param			from_date		query		string	false	"RFC 3339 time or YYYY-MM-DD (inclusive)"
//	@Param			to_date			query		string	false	"RFC 3339 time or YYYY-MM-DD (inclusive, whole day)"
//	@Param			include_diff	query		bool	false	"include per-field diffs (default true)"
//	@Success		200				{object}	responses.Envelope
//	@Failure		400				{object}	responses.Envelope
//	@Failure		403				{object}	responses.Envelope
//	@Failure		404				{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/posts/{param1}/revisions [get]
func adminListPostRevisionsHandler(posts postRevisionsAPI, all allPosts) gin.HandlerFunc {
	return func(c *gin.Context) {
		viewerID, ok := middleware.CurrentUserID(c)
		if !ok {
			responses.Failure(c, nethttp.StatusUnauthorized, responses.ErrorCodeUnauthorized, "Unauthorized")
			return
		}

		postID, ok := postPathID(c)
		if !ok {
			return
		}

		filter, includeDiff, ok := revisionFilter(c, postID)
		if !ok {
			return
		}

		page, err := posts.ListRevisions(c.Request.Context(), viewerID, all(c), filter)
		if mapPostError(c, err) {
			return
		}

		items := make([]gin.H, 0, len(page.Items))
		for _, rev := range page.Items {
			items = append(items, responses.PostRevision(rev, includeDiff, false))
		}

		responses.SuccessPaginatedFor(c, nethttp.StatusOK, responses.PageOpts{
			Service: responses.ServicePosts,
			Data:    items,
			Page:    page.Page,
			PerPage: page.PerPage,
			Total:   page.Total,
		})
	}
}

// adminGetPostRevisionHandler godoc
//
//	@Summary		Get post revision
//	@Description	One revision with its full snapshot (content, meta, tags, media, SEO) and diff against the previous revision.
//	@Tags			admin
//	@Produce		json
//	@Param			param1	path		string	true	"post UUID"
//	@Param			param2	path		string	true	"revision UUID or revision number"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/posts/{param1}/revisions/{param2} [get]
func adminGetPostRevisionHandler(posts postRevisionsAPI, all allPosts) gin.HandlerFunc {
	return func(c *gin.Context) {
		viewerID, postID, ref, ok := revisionTarget(c)
		if !ok {
			return
		}

		rev, err := posts.GetRevision(c.Request.Context(), postID, ref, viewerID, all(c))
		if mapPostError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServicePosts, responses.CaseSuccess, responses.PostRevision(rev, true, true))
	}
}

// adminRestorePostRevisionHandler godoc
//
//	@Summary		Restore post revision
//	@Description	Copies the revision's title, slug, excerpt, content, comment policy, category, cover, tags, and media back onto the post as a new revision of type restore. The post keeps its status and published_at. Categories, tags, and media that no longer exist (or media that is not ready) are skipped and listed in skipped; if another post took the slug, the current slug stays.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			param1	path		string						true	"post UUID"
//	@Param			param2	path		string						true	"revision UUID or revision number"
//	@Param			body	body		requests.RestoreRevision	false	"optional note"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/posts/{param1}/revisions/{param2}/restore [post]
func adminRestorePostRevisionHandler(posts postRevisionsAPI, all allPosts) gin.HandlerFunc {
	return func(c *gin.Context) {
		actorID, postID, ref, ok := revisionTarget(c)
		if !ok {
			return
		}

		var body requests.RestoreRevision
		if c.Request.ContentLength != 0 && !requests.BindJSON(c, &body) {
			return
		}

		result, err := posts.RestoreRevision(c.Request.Context(), postID, ref, actorID, all(c), strings.TrimSpace(body.RestoreNote))
		if mapPostError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServicePosts, responses.CaseSuccess, gin.H{
			"post":     responses.PostWithTags(result.Post, result.Tags),
			"revision": responses.PostRevision(result.Revision, true, false),
			"skipped":  result.Skipped,
		})
	}
}

// withPostEditor runs handler with the signed-in user and request id in the request
// context, so the post writes it makes attribute their revisions.
func withPostEditor(handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		editor := postdomain.Editor{RequestID: responses.RequestID(c)}
		if userID, ok := middleware.CurrentUserID(c); ok {
			editor.UserID = &userID
		}

		c.Request = c.Request.WithContext(postdomain.WithEditor(c.Request.Context(), editor))
		handler(c)
	}
}

func revisionTarget(c *gin.Context) (uuid.UUID, uuid.UUID, postdomain.RevisionRef, bool) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		responses.Failure(c, nethttp.StatusUnauthorized, responses.ErrorCodeUnauthorized, "Unauthorized")
		return uuid.Nil, uuid.Nil, postdomain.RevisionRef{}, false
	}

	postID, ok := postPathID(c)
	if !ok {
		return uuid.Nil, uuid.Nil, postdomain.RevisionRef{}, false
	}

	ref, err := postdomain.ParseRevisionRef(c.Param("param2"))
	if err != nil {
		responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "Revision must be a UUID or a positive number")
		return uuid.Nil, uuid.Nil, postdomain.RevisionRef{}, false
	}

	return userID, postID, ref, true
}

func revisionFilter(c *gin.Context, postID uuid.UUID) (postdomain.RevisionFilter, bool, bool) {
	filter := postdomain.RevisionFilter{PostUUID: postID}
	filter.Page, filter.PerPage = pageParams(c)

	fail := func(message string) (postdomain.RevisionFilter, bool, bool) {
		responses.Failure(c, nethttp.StatusBadRequest, responses.ErrorCodeValidation, message)
		return postdomain.RevisionFilter{}, false, false
	}

	if raw := strings.TrimSpace(c.Query("author_id")); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return fail("author_id must be a UUID")
		}

		filter.AuthorUUID = &id
	}

	var err error
	if filter.From, err = revisionDate(c.Query("from_date"), false); err != nil {
		return fail("from_date must be an RFC 3339 time or YYYY-MM-DD")
	}

	if filter.To, err = revisionDate(c.Query("to_date"), true); err != nil {
		return fail("to_date must be an RFC 3339 time or YYYY-MM-DD")
	}

	includeDiff := true

	if raw := strings.TrimSpace(c.Query("include_diff")); raw != "" {
		if includeDiff, err = strconv.ParseBool(raw); err != nil {
			return fail("include_diff must be true or false")
		}
	}

	return filter, includeDiff, true
}

// revisionDate parses an RFC 3339 time or a date; a date bound at the end of a range
// covers that whole day.
func revisionDate(raw string, endOfDay bool) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return &t, nil
	}

	day, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		return nil, err
	}

	if endOfDay {
		day = day.Add(24*time.Hour - time.Nanosecond)
	}

	return &day, nil
}

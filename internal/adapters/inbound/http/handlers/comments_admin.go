package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	commentservice "github.com/turahe/blog-api/internal/core/comment/service"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

var commentsAdminCfg = pagination.CursorConfig{
	Kind: "comments_admin",
	Sort: []pagination.SortField{
		{Name: "created_at", Dir: pagination.Asc, Type: pagination.TypeTime},
		{Name: "id", Dir: pagination.Asc, Type: pagination.TypeInt64},
	},
	TTL:            pagination.DefaultTTL,
	MaxPerPage:     pagination.DefaultMaxPerPage,
	DefaultPerPage: pagination.DefaultPerPage,
}

var commentsAdminNewestCfg = pagination.CursorConfig{
	Kind: "comments_admin_newest",
	Sort: []pagination.SortField{
		{Name: "created_at", Dir: pagination.Desc, Type: pagination.TypeTime},
		{Name: "id", Dir: pagination.Desc, Type: pagination.TypeInt64},
	},
	TTL:            pagination.DefaultTTL,
	MaxPerPage:     pagination.DefaultMaxPerPage,
	DefaultPerPage: pagination.DefaultPerPage,
}

type commentModerationAPI interface {
	AdminList(ctx context.Context, in commentservice.AdminListInput) (commentdomain.ListResult, error)
	AdminGet(ctx context.Context, id uuid.UUID) (commentdomain.Review, error)
	Moderate(ctx context.Context, in commentservice.ModerateInput) (commentdomain.Comment, error)
	BulkModerate(ctx context.Context, in commentservice.BulkModerateInput) (int, error)
	HardDelete(ctx context.Context, moderatorID, id uuid.UUID, reason string) (bool, error)
	Stats(ctx context.Context) (commentdomain.Stats, error)
}

// adminListCommentsHandler godoc
//
//	@Summary		List comments for moderation
//	@Description	Comments in any status across posts. Defaults to the moderation queue (pending and flagged), oldest first.
//	@Description	Supports both legacy offset pagination (page/perPage) and cursor-based keyset pagination (after/before/limit).
//	@Tags			admin
//	@Produce		json
//	@Param			status			query		string	false	"comma-separated statuses: pending, approved, flagged, spam, rejected, deleted"
//	@Param			postId			query		string	false	"post UUID"
//	@Param			sort			query		string	false	"oldest or newest"								Enums(oldest, newest)	default(oldest)
//	@Param			page			query		int		false	"page (legacy offset mode)"						default(1)
//	@Param			perPage			query		int		false	"per page (legacy offset mode, alias limit)"	default(20)
//	@Param			limit			query		int		false	"page size (cursor or offset)"					default(20)
//	@Param			after			query		string	false	"opaque cursor: return items after this point"
//	@Param			before			query		string	false	"opaque cursor: return items before this point"
//	@Param			includeTotal	query		bool	false	"when false, skip COUNT(*) to reduce DB load"	default(true)
//	@Success		200				{object}	responses.Envelope
//	@Failure		400				{object}	responses.Envelope
//	@Failure		401				{object}	responses.Envelope
//	@Failure		403				{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/comments [get]
func adminListCommentsHandler(comments commentModerationAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		in := commentservice.AdminListInput{}

		for _, raw := range c.QueryArray("status") {
			for status := range strings.SplitSeq(raw, ",") {
				if status = strings.TrimSpace(status); status != "" {
					in.Statuses = append(in.Statuses, commentdomain.Status(status))
				}
			}
		}

		if raw := strings.TrimSpace(c.Query("postId")); raw != "" {
			postID, err := uuid.Parse(raw)
			if err != nil {
				failCommentValidation(c, "Invalid postId")
				return
			}

			in.PostUUID = &postID
		}

		switch c.DefaultQuery("sort", "oldest") {
		case "oldest":
		case "newest":
			in.NewestFirst = true
		default:
			failCommentValidation(c, "sort must be oldest or newest")
			return
		}

		cfg := commentsAdminCfg
		if in.NewestFirst {
			cfg = commentsAdminNewestCfg
		}
		pr, err := pagination.ParseRequest(c, cfg)
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, pagination.ErrorCode(err), pagination.ErrorCause(err))
			return
		}
		in.PageRequest = pr

		result, err := comments.AdminList(c.Request.Context(), in)
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
				if mapCommentError(c, err) {
					return
				}
			}
			return
		}

		items := make([]gin.H, 0, len(result.Items))
		for _, comment := range result.Items {
			items = append(items, responses.AdminComment(comment))
		}

		responses.SuccessPaginatedResult[commentdomain.Comment](c, nethttp.StatusOK, responses.CursorPageOpts[commentdomain.Comment]{
			Service: responses.ServiceComments,
			Result:  result,
			Data:    items,
		})
	}
}

// adminGetCommentHandler godoc
//
//	@Summary		Get a comment for review
//	@Description	Any status, with author identity, flags, and the moderation log with before/after snapshots.
//	@Tags			admin
//	@Produce		json
//	@Param			param1	path		string	true	"comment UUID"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/comments/{param1} [get]
func adminGetCommentHandler(comments commentModerationAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := commentPathID(c, "Invalid comment id")
		if !ok {
			return
		}

		review, err := comments.AdminGet(c.Request.Context(), id)
		if mapCommentError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceComments, responses.CaseSuccess, responses.CommentReview(review))
	}
}

// adminCommentStatsHandler godoc
//
//	@Summary	Comment moderation stats
//	@Tags		admin
//	@Produce	json
//	@Success	200	{object}	responses.Envelope
//	@Failure	401	{object}	responses.Envelope
//	@Failure	403	{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/admin/comments/stats [get]
func adminCommentStatsHandler(comments commentModerationAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		stats, err := comments.Stats(c.Request.Context())
		if mapCommentError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceComments, responses.CaseSuccess, responses.CommentStats(stats))
	}
}

// adminModerateCommentHandler godoc
//
//	@Summary		Moderate a comment
//	@Description	approve, reject, or spam a comment, or restore a deleted one. Returns 409 when the action is not allowed from the current status.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			param1	path		string						true	"comment UUID"
//	@Param			body	body		requests.ModerateComment	true	"moderation action"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/comments/{param1}/moderate [post]
func adminModerateCommentHandler(comments commentModerationAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		moderatorID, ok := requireCommentUser(c)
		if !ok {
			return
		}

		id, ok := commentPathID(c, "Invalid comment id")
		if !ok {
			return
		}

		var req requests.ModerateComment
		if !requests.BindJSON(c, &req) {
			return
		}

		comment, err := comments.Moderate(c.Request.Context(), commentservice.ModerateInput{
			ModeratorUUID: moderatorID,
			CommentUUID:   id,
			Action:        commentdomain.Action(req.Action),
			Reason:        req.Reason,
			NotifyAuthor:  req.NotifyAuthor,
		})
		if mapCommentError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceComments, responses.CaseSuccess, responses.AdminComment(comment))
	}
}

// adminBulkModerateCommentsHandler godoc
//
//	@Summary		Bulk moderate comments
//	@Description	Applies one action to up to 500 comments in a single transaction: all change or none do. Unknown ids return 404 and disallowed transitions 409, both listing the offending ids in error.details.ids.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			body	body		requests.BulkModerateComments	true	"comment ids and action"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/comments/bulk-moderate [post]
func adminBulkModerateCommentsHandler(comments commentModerationAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		moderatorID, ok := requireCommentUser(c)
		if !ok {
			return
		}

		var req requests.BulkModerateComments
		if !requests.BindJSON(c, &req) {
			return
		}

		ids := make([]uuid.UUID, 0, len(req.IDs))
		for _, raw := range req.IDs {
			id, err := uuid.Parse(raw)
			if err != nil {
				failCommentValidation(c, "Invalid comment id in ids")
				return
			}

			ids = append(ids, id)
		}

		updated, err := comments.BulkModerate(c.Request.Context(), commentservice.BulkModerateInput{
			ModeratorUUID: moderatorID,
			CommentUUIDs:  ids,
			Action:        commentdomain.Action(req.Action),
			Reason:        req.Reason,
		})
		if mapCommentError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceComments, responses.CaseSuccess,
			gin.H{"action": req.Action, "updated": updated})
	}
}

// adminHardDeleteCommentHandler godoc
//
//	@Summary		Permanently delete a comment
//	@Description	Removes the comment row. A comment with replies is scrubbed instead (content and author erased, kept as a deleted placeholder) so its replies survive; outcome reports which happened. The action is recorded in the moderation log.
//	@Tags			admin
//	@Produce		json
//	@Param			param1	path		string	true	"comment UUID"
//	@Param			reason	query		string	false	"moderation reason, up to 1000 characters"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/admin/comments/{param1} [delete]
func adminHardDeleteCommentHandler(comments commentModerationAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		moderatorID, ok := requireCommentUser(c)
		if !ok {
			return
		}

		id, ok := commentPathID(c, "Invalid comment id")
		if !ok {
			return
		}

		scrubbed, err := comments.HardDelete(c.Request.Context(), moderatorID, id, c.Query("reason"))
		if mapCommentError(c, err) {
			return
		}

		outcome := "removed"
		if scrubbed {
			outcome = "scrubbed"
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceComments, responses.CaseSuccess,
			gin.H{"id": id.String(), "outcome": outcome})
	}
}

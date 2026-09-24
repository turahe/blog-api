package handlers

import (
	"context"
	nethttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	commentservice "github.com/turahe/blog-api/internal/core/comment/service"
)

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
//	@Tags			admin
//	@Produce		json
//	@Param			status		query		string	false	"comma-separated statuses: pending, approved, flagged, spam, rejected, deleted"
//	@Param			post_id		query		string	false	"post UUID"
//	@Param			sort		query		string	false	"oldest or newest"	Enums(oldest, newest)	default(oldest)
//	@Param			page		query		int		false	"page"				default(1)
//	@Param			per_page	query		int		false	"per page"			default(20)
//	@Success		200			{object}	responses.Envelope
//	@Failure		400			{object}	responses.Envelope
//	@Failure		401			{object}	responses.Envelope
//	@Failure		403			{object}	responses.Envelope
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

		if raw := strings.TrimSpace(c.Query("post_id")); raw != "" {
			postID, err := uuid.Parse(raw)
			if err != nil {
				failCommentValidation(c, "Invalid post_id")
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

		in.Page, in.PerPage = pageParams(c)

		result, err := comments.AdminList(c.Request.Context(), in)
		if mapCommentError(c, err) {
			return
		}

		items := make([]gin.H, 0, len(result.Items))
		for _, comment := range result.Items {
			items = append(items, responses.AdminComment(comment))
		}

		responses.SuccessPaginatedFor(c, nethttp.StatusOK, responses.PageOpts{
			Service: responses.ServiceComments,
			Data:    items,
			Page:    result.Page,
			PerPage: result.PerPage,
			Total:   result.Total,
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

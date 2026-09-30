package handlers

import (
	"context"
	"errors"
	nethttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	commentservice "github.com/turahe/blog-api/internal/core/comment/service"
	"github.com/turahe/blog-api/internal/shared/pagination"
)

var commentsPublicCfg = pagination.CursorConfig{
	Kind: "comments_public",
	Sort: []pagination.SortField{
		{Name: "created_at", Dir: pagination.Asc, Type: pagination.TypeTime},
		{Name: "id", Dir: pagination.Asc, Type: pagination.TypeInt64},
	},
	TTL:            pagination.DefaultTTL,
	MaxPerPage:     pagination.DefaultMaxPerPage,
	DefaultPerPage: pagination.DefaultPerPage,
}

var commentsMeCfg = pagination.CursorConfig{
	Kind: "comments_me",
	Sort: []pagination.SortField{
		{Name: "created_at", Dir: pagination.Desc, Type: pagination.TypeTime},
		{Name: "id", Dir: pagination.Desc, Type: pagination.TypeInt64},
	},
	TTL:            pagination.DefaultTTL,
	MaxPerPage:     pagination.DefaultMaxPerPage,
	DefaultPerPage: pagination.DefaultPerPage,
}

type commentAPI interface {
	Create(ctx context.Context, in commentservice.CreateInput) (commentdomain.Comment, error)
	ListForPost(ctx context.Context, filter commentdomain.ListFilter) (commentdomain.ListResult, error)
	GetThread(ctx context.Context, id uuid.UUID) (commentdomain.Thread, error)
	ListMine(ctx context.Context, filter commentdomain.ListFilter) (commentdomain.ListResult, error)
	Update(ctx context.Context, actorID, id uuid.UUID, content string) (commentdomain.Comment, error)
	Delete(ctx context.Context, actorID, id uuid.UUID) error
	Flag(ctx context.Context, in commentservice.FlagInput) error
	ToggleUpvote(ctx context.Context, voterID, id uuid.UUID) (bool, int, error)
}

// listPostCommentsHandler godoc
//
//	@Summary		List comments on a post
//	@Description	Top-level comments by default; pass parentId to list the direct replies of a comment.
//	@Description	Supports both legacy offset pagination (page/perPage) and cursor-based keyset pagination (after/before/limit).
//	@Tags			public
//	@Produce		json
//	@Param			param1			path		string	true	"post UUID"
//	@Param			parentId		query		string	false	"parent comment UUID"
//	@Param			page			query		int		false	"page (legacy offset mode)"			default(1)
//	@Param			perPage			query		int		false	"per page (legacy offset mode, alias limit)"	default(20)
//	@Param			limit			query		int		false	"page size (cursor or offset)"		default(20)
//	@Param			after			query		string	false	"opaque cursor: return items after this point"
//	@Param			before			query		string	false	"opaque cursor: return items before this point"
//	@Param			includeTotal	query		bool	false	"when false, skip COUNT(*) to reduce DB load"	default(true)
//	@Success		200				{object}	responses.Envelope
//	@Failure		400				{object}	responses.Envelope
//	@Failure		403				{object}	responses.Envelope
//	@Failure		404				{object}	responses.Envelope
//	@Router			/api/v1/posts/{param1}/comments [get]
func listPostCommentsHandler(comments commentAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		postID, ok := commentPathID(c, "Invalid post id")
		if !ok {
			return
		}

		var parentID *uuid.UUID

		if raw := strings.TrimSpace(c.Query("parentId")); raw != "" {
			parsed, err := uuid.Parse(raw)
			if err != nil {
				failCommentValidation(c, "Invalid parentId")
				return
			}

			parentID = &parsed
		}

		pr, err := pagination.ParseRequest(c, commentsPublicCfg)
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, pagination.ErrorCode(err), pagination.ErrorCause(err))
			return
		}

		filter := commentdomain.ListFilter{PostUUID: &postID, ParentUUID: parentID}
		filter.PageRequest = pr

		result, err := comments.ListForPost(c.Request.Context(), filter)
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
			items = append(items, responses.Comment(comment))
		}

		responses.SuccessPaginatedResult[commentdomain.Comment](c, nethttp.StatusOK, responses.CursorPageOpts[commentdomain.Comment]{
			Service: responses.ServiceComments,
			Result:  result,
			Data:    items,
		})
	}
}

// createPostCommentHandler godoc
//
//	@Summary		Comment on a post
//	@Description	Signed-in users comment as themselves. Guests send authorName and authorEmail when guest comments are enabled; guest comments await moderation. When the server has TURNSTILE_SECRET_KEY set, guests must also send turnstileResponse.
//	@Tags			public
//	@Accept			json
//	@Produce		json
//	@Param			param1	path		string					true	"post UUID"
//	@Param			body	body		requests.CreateComment	true	"comment"
//	@Success		201		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Failure		422		{object}	responses.Envelope
//	@Failure		429		{object}	responses.Envelope
//	@Failure		503		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/posts/{param1}/comments [post]
func createPostCommentHandler(comments commentAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		postID, ok := commentPathID(c, "Invalid post id")
		if !ok {
			return
		}

		var req requests.CreateComment
		if !requests.BindJSON(c, &req) {
			return
		}

		in := commentservice.CreateInput{
			PostUUID:     postID,
			AuthorName:   req.AuthorName,
			AuthorEmail:  req.AuthorEmail,
			Content:      req.Content,
			Honeypot:     req.Honeypot,
			CaptchaToken: req.TurnstileResponse,
			ClientIP:     c.ClientIP(),
			UserAgent:    c.Request.UserAgent(),
		}
		if req.ParentID != "" {
			parentID, err := uuid.Parse(req.ParentID)
			if err != nil {
				failCommentValidation(c, "Invalid parentId")
				return
			}

			in.ParentUUID = &parentID
		}

		if userID, ok := middleware.CurrentUserID(c); ok {
			in.AuthorUUID = &userID
		}

		comment, err := comments.Create(c.Request.Context(), in)
		if mapCommentError(c, err) {
			return
		}

		if comment.Status == commentdomain.StatusSpam {
			comment.Status = commentdomain.StatusPending
		}

		responses.SuccessFor(c, nethttp.StatusCreated, responses.ServiceComments, responses.CaseSuccess, responses.Comment(comment))
	}
}

// getCommentHandler godoc
//
//	@Summary	Get a comment with its first replies
//	@Tags		public
//	@Produce	json
//	@Param		param1	path		string	true	"comment UUID"
//	@Success	200		{object}	responses.Envelope
//	@Failure	403		{object}	responses.Envelope
//	@Failure	404		{object}	responses.Envelope
//	@Router		/api/v1/comments/{param1} [get]
func getCommentHandler(comments commentAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := commentPathID(c, "Invalid comment id")
		if !ok {
			return
		}

		thread, err := comments.GetThread(c.Request.Context(), id)
		if mapCommentError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceComments, responses.CaseSuccess, responses.CommentThread(thread))
	}
}

// flagCommentHandler godoc
//
//	@Summary		Flag a comment for moderation
//	@Description	Repeat flags from the same user (or guest IP and user agent) are accepted but counted once.
//	@Tags			public
//	@Accept			json
//	@Produce		json
//	@Param			param1	path		string					true	"comment UUID"
//	@Param			body	body		requests.FlagComment	true	"flag"
//	@Success		202		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Failure		429		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/comments/{param1}/flag [post]
func flagCommentHandler(comments commentAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := commentPathID(c, "Invalid comment id")
		if !ok {
			return
		}

		var req requests.FlagComment
		if !requests.BindJSON(c, &req) {
			return
		}

		in := commentservice.FlagInput{
			CommentUUID: id,
			Reason:      req.ReasonCode,
			Details:     req.Details,
			ClientIP:    c.ClientIP(),
			UserAgent:   c.Request.UserAgent(),
		}
		if userID, ok := middleware.CurrentUserID(c); ok {
			in.ReporterUUID = &userID
		}

		if err := comments.Flag(c.Request.Context(), in); mapCommentError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusAccepted, responses.ServiceComments, responses.CaseAccepted, gin.H{"received": true})
	}
}

// listMyCommentsHandler godoc
//
//	@Summary		List my comments
//	@Description	All of the caller's comments except deleted ones, newest first, including pending and rejected.
//	@Description	Supports both legacy offset pagination (page/perPage) and cursor-based keyset pagination (after/before/limit).
//	@Tags			self-service
//	@Produce		json
//	@Param			page			query		int		false	"page (legacy offset mode)"			default(1)
//	@Param			perPage			query		int		false	"per page (legacy offset mode, alias limit)"	default(20)
//	@Param			limit			query		int		false	"page size (cursor or offset)"		default(20)
//	@Param			after			query		string	false	"opaque cursor: return items after this point"
//	@Param			before			query		string	false	"opaque cursor: return items before this point"
//	@Param			includeTotal	query		bool	false	"when false, skip COUNT(*) to reduce DB load"	default(true)
//	@Success		200				{object}	responses.Envelope
//	@Failure		401				{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/me/comments [get]
func listMyCommentsHandler(comments commentAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := requireCommentUser(c)
		if !ok {
			return
		}

		pr, err := pagination.ParseRequest(c, commentsMeCfg)
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, pagination.ErrorCode(err), pagination.ErrorCause(err))
			return
		}

		filter := commentdomain.ListFilter{AuthorUUID: &userID}
		filter.PageRequest = pr

		result, err := comments.ListMine(c.Request.Context(), filter)
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
			items = append(items, responses.Comment(comment))
		}

		responses.SuccessPaginatedResult[commentdomain.Comment](c, nethttp.StatusOK, responses.CursorPageOpts[commentdomain.Comment]{
			Service: responses.ServiceComments,
			Result:  result,
			Data:    items,
		})
	}
}

// patchCommentHandler godoc
//
//	@Summary		Edit my comment
//	@Description	Authors may edit their own comment within the edit window after posting.
//	@Tags			self-service
//	@Accept			json
//	@Produce		json
//	@Param			param1	path		string					true	"comment UUID"
//	@Param			body	body		requests.UpdateComment	true	"new content"
//	@Success		200		{object}	responses.Envelope
//	@Failure		400		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Failure		409		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/comments/{param1} [patch]
func patchCommentHandler(comments commentAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := requireCommentUser(c)
		if !ok {
			return
		}

		id, ok := commentPathID(c, "Invalid comment id")
		if !ok {
			return
		}

		var req requests.UpdateComment
		if !requests.BindJSON(c, &req) {
			return
		}

		comment, err := comments.Update(c.Request.Context(), userID, id, req.Content)
		if mapCommentError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceComments, responses.CaseSuccess, responses.Comment(comment))
	}
}

// deleteCommentHandler godoc
//
//	@Summary		Delete my comment
//	@Description	Soft delete: the comment stays in the thread as a placeholder without content or author.
//	@Tags			self-service
//	@Produce		json
//	@Param			param1	path		string	true	"comment UUID"
//	@Success		200		{object}	responses.Envelope
//	@Failure		401		{object}	responses.Envelope
//	@Failure		403		{object}	responses.Envelope
//	@Failure		404		{object}	responses.Envelope
//	@Security		Bearer
//	@Router			/api/v1/comments/{param1} [delete]
func deleteCommentHandler(comments commentAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := requireCommentUser(c)
		if !ok {
			return
		}

		id, ok := commentPathID(c, "Invalid comment id")
		if !ok {
			return
		}

		if err := comments.Delete(c.Request.Context(), userID, id); mapCommentError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceComments, responses.CaseSuccess, nil)
	}
}

// upvoteCommentHandler godoc
//
//	@Summary	Toggle my upvote on a comment
//	@Tags		self-service
//	@Produce	json
//	@Param		param1	path		string	true	"comment UUID"
//	@Success	200		{object}	responses.Envelope
//	@Failure	401		{object}	responses.Envelope
//	@Failure	403		{object}	responses.Envelope
//	@Failure	404		{object}	responses.Envelope
//	@Failure	429		{object}	responses.Envelope
//	@Security	Bearer
//	@Router		/api/v1/comments/{param1}/upvote [post]
func upvoteCommentHandler(comments commentAPI) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := requireCommentUser(c)
		if !ok {
			return
		}

		id, ok := commentPathID(c, "Invalid comment id")
		if !ok {
			return
		}

		upvoted, count, err := comments.ToggleUpvote(c.Request.Context(), userID, id)
		if mapCommentError(c, err) {
			return
		}

		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceComments, responses.CaseSuccess,
			gin.H{"id": id.String(), "upvoted": upvoted, "upvoteCount": count})
	}
}

func commentPathID(c *gin.Context, message string) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("param1")))
	if err != nil {
		failCommentValidation(c, message)
		return uuid.Nil, false
	}

	return id, true
}

func requireCommentUser(c *gin.Context) (uuid.UUID, bool) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		responses.FailureFor(c, nethttp.StatusUnauthorized, responses.FailureOpts{
			Service: responses.ServiceComments,
			Code:    responses.ErrorCodeUnauthorized,
			Message: "Authentication required",
		})
	}

	return userID, ok
}

func failCommentValidation(c *gin.Context, message string) {
	responses.FailureFor(c, nethttp.StatusBadRequest, responses.FailureOpts{
		Service: responses.ServiceComments,
		Code:    responses.ErrorCodeValidation,
		Message: message,
	})
}

func mapCommentError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}

	status, code, message, known := classifyCommentError(err)
	if !known {
		responses.RecordError(c, err)
	}

	responses.FailureFor(c, status, responses.FailureOpts{
		Service: responses.ServiceComments,
		Code:    code,
		Message: message,
		Details: batchDetails(err),
	})

	return true
}

// batchDetails lists the comments that blocked a bulk moderation.
func batchDetails(err error) any {
	batch, ok := errors.AsType[*commentdomain.BatchError](err)
	if !ok {
		return nil
	}

	ids := make([]string, 0, len(batch.IDs))
	for _, id := range batch.IDs {
		ids = append(ids, id.String())
	}

	return gin.H{"ids": ids}
}

// commentErrorRule maps a comment error to a response. An empty message means the error
// text is shown; unexpected errors are also reported to error tracking.
type commentErrorRule struct {
	err        error
	status     int
	code       string
	message    string
	unexpected bool
}

var commentErrorRules = []commentErrorRule{
	{commentdomain.ErrInvalidTransition, nethttp.StatusConflict, "comment.invalid_transition", "The action is not allowed from the comment's current status", false},
	{commentdomain.ErrValidation, nethttp.StatusBadRequest, responses.ErrorCodeValidation, "", false},
	{commentdomain.ErrGuestDisabled, nethttp.StatusUnauthorized, responses.ErrorCodeUnauthorized, "Sign in to comment", false},
	{commentdomain.ErrCommentsDisabled, nethttp.StatusForbidden, "comment.disabled", "Comments are disabled on this post", false},
	{commentdomain.ErrCommentsClosed, nethttp.StatusForbidden, "comment.closed", "This post is not accepting new comments", false},
	{commentdomain.ErrChallengeFailed, nethttp.StatusBadRequest, "comments.spam.challenge_invalid", "Captcha verification failed; please try again", false},
	{commentdomain.ErrChallengeUnavailable, nethttp.StatusServiceUnavailable, "comments.spam.challenge_unavailable", "Captcha verification is unavailable; please try again later", true},
	{commentdomain.ErrForbidden, nethttp.StatusForbidden, responses.ErrorCodeForbidden, "You can only change your own comments", false},
	{commentdomain.ErrEditWindowClosed, nethttp.StatusForbidden, "comment.edit_window_closed", "The edit window for this comment has closed", false},
	{commentdomain.ErrNotFound, nethttp.StatusNotFound, responses.ErrorCodeNotFound, "Comment not found", false},
	{commentdomain.ErrPostNotFound, nethttp.StatusNotFound, responses.ErrorCodeNotFound, "Post not found", false},
	{commentdomain.ErrNotEditable, nethttp.StatusConflict, "comment.not_editable", "This comment can no longer be edited", false},
	{commentdomain.ErrParentInvalid, nethttp.StatusUnprocessableEntity, "comment.parent_invalid", "parentId must be an approved comment on the same post", false},
	{commentdomain.ErrDepthExceeded, nethttp.StatusUnprocessableEntity, "comment.depth_exceeded", "Replies cannot be nested deeper than 5 levels", false},
}

func classifyCommentError(err error) (status int, code, message string, known bool) {
	for _, rule := range commentErrorRules {
		if !errors.Is(err, rule.err) {
			continue
		}

		message = rule.message
		if message == "" {
			message = err.Error()
		}

		return rule.status, rule.code, message, !rule.unexpected
	}

	return nethttp.StatusInternalServerError, responses.ErrorCodeInternal, "Failed to process comment", false
}

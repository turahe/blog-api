package handlers

import (
	"errors"
	nethttp "net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/middleware"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/requests"
	"github.com/turahe/blog-api/internal/adapters/inbound/http/responses"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
)

func loginHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.Login
		if !requests.BindJSON(c, &req) {
			return
		}
		pair, err := auth.Login(c.Request.Context(), req.Email, req.Password, c.Request.UserAgent(), c.ClientIP(), req.Remember)
		if err != nil {
			code, message, status := authservice.MapError(err)
			responses.FailureFor(c, status, responses.ServiceAuth, responses.CaseCodeForStatus(status), code, message, nil)
			return
		}
		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAuth, responses.CaseSuccess, responses.TokenPair(pair))
	}
}

func refreshHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.Refresh
		if !requests.BindJSON(c, &req) {
			return
		}
		pair, err := auth.Refresh(c.Request.Context(), req.RefreshToken, c.Request.UserAgent(), c.ClientIP())
		if err != nil {
			code, message, status := authservice.MapError(err)
			responses.FailureFor(c, status, responses.ServiceAuth, responses.CaseCodeForStatus(status), code, message, nil)
			return
		}
		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAuth, responses.CaseSuccess, responses.TokenPair(pair))
	}
}

func logoutHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := middleware.CurrentUserID(c)
		if !ok {
			responses.FailureFor(c, nethttp.StatusUnauthorized, responses.ServiceAuth, responses.CaseUnauthorized, "unauthorized", "Authentication required", nil)
			return
		}
		var req requests.Logout
		_ = c.ShouldBindJSON(&req) // body optional
		if err := auth.Logout(c.Request.Context(), userID, req.RefreshToken); err != nil {
			code, message, status := authservice.MapError(err)
			responses.FailureFor(c, status, responses.ServiceAuth, responses.CaseCodeForStatus(status), code, message, nil)
			return
		}
		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAuth, responses.CaseSuccess, gin.H{})
	}
}

func forgotPasswordHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.ForgotPassword
		if !requests.BindJSON(c, &req) {
			return
		}
		if err := auth.ForgotPassword(c.Request.Context(), req.EmailOrUsername); err != nil {
			code, message, status := authservice.MapError(err)
			responses.FailureFor(c, status, responses.ServiceAuth, responses.CaseCodeForStatus(status), code, message, nil)
			return
		}
		responses.SuccessFor(c, nethttp.StatusAccepted, responses.ServiceAuth, responses.CaseAccepted, gin.H{})
	}
}

func resetTokenValidityHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimSpace(c.Param("param1"))
		validity, err := auth.CheckResetToken(c.Request.Context(), token)
		if err != nil {
			code, message, status := authservice.MapResetError(err)
			responses.FailureFor(c, status, responses.ServiceAuth, responses.CaseCodeForStatus(status), code, message, nil)
			return
		}
		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAuth, responses.CaseSuccess, gin.H{
			"valid":      validity.Valid,
			"expires_at": validity.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}
}

func resetPasswordHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req requests.ResetPassword
		if !requests.BindJSON(c, &req) {
			return
		}
		if err := auth.ResetPassword(c.Request.Context(), req.Token, req.NewPassword, req.ConfirmPassword); err != nil {
			code, message, status := authservice.MapResetError(err)
			responses.FailureFor(c, status, responses.ServiceAuth, responses.CaseCodeForStatus(status), code, message, nil)
			return
		}
		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAuth, responses.CaseSuccess, gin.H{})
	}
}

func changePasswordHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := middleware.CurrentUserID(c)
		if !ok {
			responses.FailureFor(c, nethttp.StatusUnauthorized, responses.ServiceAuth, responses.CaseUnauthorized, "unauthorized", "Authentication required", nil)
			return
		}
		var req requests.ChangePassword
		if !requests.BindJSON(c, &req) {
			return
		}
		revokeAll := true
		if req.RevokeAllSessions != nil {
			revokeAll = *req.RevokeAllSessions
		}
		changedAt, invalidated, err := auth.ChangePassword(
			c.Request.Context(), userID, req.CurrentPassword, req.NewPassword, req.ConfirmPassword, revokeAll,
		)
		if err != nil {
			code, message, status := authservice.MapError(err)
			responses.FailureFor(c, status, responses.ServiceAuth, responses.CaseCodeForStatus(status), code, message, nil)
			return
		}
		responses.SuccessFor(c, nethttp.StatusOK, responses.ServiceAuth, responses.CaseSuccess, gin.H{
			"password_changed_at":  changedAt.UTC().Format(time.RFC3339),
			"sessions_invalidated": invalidated,
		})
	}
}

func listPublishedPostsHandler(posts *postservice.PostService) gin.HandlerFunc {
	return func(c *gin.Context) {
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
		categoryID, err := postservice.ParseOptionalUUID(c.Query("category_id"))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid category_id")
			return
		}
		tagID, err := postservice.ParseOptionalUUID(c.Query("tag_id"))
		if err != nil {
			responses.Failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid tag_id")
			return
		}
		result, err := posts.ListPublished(c.Request.Context(), postdomain.ListFilter{
			Page: page, PerPage: perPage, CategoryID: categoryID, TagID: tagID,
		})
		if err != nil {
			responses.Failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to list posts")
			return
		}
		items := make([]gin.H, 0, len(result.Items))
		for _, post := range result.Items {
			items = append(items, responses.Post(post))
		}
		responses.SuccessPaginatedFor(c, nethttp.StatusOK, responses.ServicePosts, items, result.Page, result.PerPage, result.Total)
	}
}

func getPublishedPostHandler(posts *postservice.PostService) gin.HandlerFunc {
	return func(c *gin.Context) {
		slug := strings.TrimSpace(c.Param("param1"))
		post, err := posts.GetPublishedBySlug(c.Request.Context(), slug)
		if errors.Is(err, postdomain.ErrNotFound) {
			responses.Failure(c, nethttp.StatusNotFound, "not_found", "Post not found")
			return
		}
		if err != nil {
			responses.Failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to load post")
			return
		}
		responses.Success(c, nethttp.StatusOK, responses.Post(post))
	}
}

package http

import (
	"errors"
	nethttp "net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	authdomain "github.com/turahe/blog-api/internal/core/auth/domain"
	authports "github.com/turahe/blog-api/internal/core/auth/ports"
	authservice "github.com/turahe/blog-api/internal/core/auth/service"
	postdomain "github.com/turahe/blog-api/internal/core/post/domain"
	postservice "github.com/turahe/blog-api/internal/core/post/service"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Remember bool   `json:"remember"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func loginHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req loginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
			return
		}
		pair, err := auth.Login(c.Request.Context(), req.Email, req.Password, c.Request.UserAgent(), c.ClientIP(), req.Remember)
		if err != nil {
			code, message, status := authservice.MapError(err)
			failure(c, status, code, message)
			return
		}
		success(c, nethttp.StatusOK, tokenPairJSON(pair))
	}
}

func refreshHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req refreshRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
			return
		}
		pair, err := auth.Refresh(c.Request.Context(), req.RefreshToken, c.Request.UserAgent(), c.ClientIP())
		if err != nil {
			code, message, status := authservice.MapError(err)
			failure(c, status, code, message)
			return
		}
		success(c, nethttp.StatusOK, tokenPairJSON(pair))
	}
}

func logoutHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUserID(c)
		if !ok {
			failure(c, nethttp.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}
		var req logoutRequest
		_ = c.ShouldBindJSON(&req)
		if err := auth.Logout(c.Request.Context(), userID, req.RefreshToken); err != nil {
			code, message, status := authservice.MapError(err)
			failure(c, status, code, message)
			return
		}
		success(c, nethttp.StatusOK, gin.H{})
	}
}

type forgotRequest struct {
	EmailOrUsername string `json:"email_or_username"`
}

func forgotPasswordHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req forgotRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
			return
		}
		if err := auth.ForgotPassword(c.Request.Context(), req.EmailOrUsername); err != nil {
			code, message, status := authservice.MapError(err)
			failure(c, status, code, message)
			return
		}
		success(c, nethttp.StatusAccepted, gin.H{})
	}
}

type resetCompleteRequest struct {
	Token           string `json:"token"`
	NewPassword     string `json:"new_password"`
	ConfirmPassword string `json:"confirm_password"`
}

func resetTokenValidityHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimSpace(c.Param("param1"))
		validity, err := auth.CheckResetToken(c.Request.Context(), token)
		if err != nil {
			code, message, status := authservice.MapResetError(err)
			failure(c, status, code, message)
			return
		}
		success(c, nethttp.StatusOK, gin.H{
			"valid":      validity.Valid,
			"expires_at": validity.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}
}

func resetPasswordHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req resetCompleteRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
			return
		}
		if err := auth.ResetPassword(c.Request.Context(), req.Token, req.NewPassword, req.ConfirmPassword); err != nil {
			code, message, status := authservice.MapResetError(err)
			failure(c, status, code, message)
			return
		}
		success(c, nethttp.StatusOK, gin.H{})
	}
}

type changePasswordRequest struct {
	CurrentPassword   string `json:"current_password"`
	NewPassword       string `json:"new_password"`
	ConfirmPassword   string `json:"confirm_password"`
	RevokeAllSessions *bool  `json:"revoke_all_sessions"`
}

func changePasswordHandler(auth authports.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := currentUserID(c)
		if !ok {
			failure(c, nethttp.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}
		var req changePasswordRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid request body")
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
			failure(c, status, code, message)
			return
		}
		success(c, nethttp.StatusOK, gin.H{
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
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid category_id")
			return
		}
		tagID, err := postservice.ParseOptionalUUID(c.Query("tag_id"))
		if err != nil {
			failure(c, nethttp.StatusBadRequest, "validation_error", "Invalid tag_id")
			return
		}
		result, err := posts.ListPublished(c.Request.Context(), postdomain.ListFilter{
			Page: page, PerPage: perPage, CategoryID: categoryID, TagID: tagID,
		})
		if err != nil {
			failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to list posts")
			return
		}
		items := make([]gin.H, 0, len(result.Items))
		for _, post := range result.Items {
			items = append(items, postJSON(post))
		}
		successWithMeta(c, nethttp.StatusOK, items, &Meta{
			RequestID: requestID(c),
			Page:      result.Page,
			PerPage:   result.PerPage,
			Total:     result.Total,
		})
	}
}

func getPublishedPostHandler(posts *postservice.PostService) gin.HandlerFunc {
	return func(c *gin.Context) {
		slug := strings.TrimSpace(c.Param("param1"))
		post, err := posts.GetPublishedBySlug(c.Request.Context(), slug)
		if errors.Is(err, postdomain.ErrNotFound) {
			failure(c, nethttp.StatusNotFound, "not_found", "Post not found")
			return
		}
		if err != nil {
			failure(c, nethttp.StatusInternalServerError, "internal_error", "Failed to load post")
			return
		}
		success(c, nethttp.StatusOK, postJSON(post))
	}
}

func tokenPairJSON(pair authdomain.TokenPair) gin.H {
	return gin.H{
		"access_token":  pair.AccessToken,
		"refresh_token": pair.RefreshToken,
		"token_type":    pair.TokenType,
		"expires_in":    pair.ExpiresIn,
	}
}

func postJSON(post postdomain.Post) gin.H {
	var categoryID any
	if post.CategoryID != nil {
		categoryID = post.CategoryID.String()
	}
	var publishedAt any
	if post.PublishedAt != nil {
		publishedAt = post.PublishedAt.UTC().Format(time.RFC3339)
	}
	return gin.H{
		"id":           post.ID.String(),
		"author_id":    post.AuthorID.String(),
		"category_id":  categoryID,
		"title":        post.Title,
		"slug":         post.Slug,
		"excerpt":      post.Excerpt,
		"content":      post.Content,
		"status":       string(post.Status),
		"published_at": publishedAt,
		"created_at":   post.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":   post.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

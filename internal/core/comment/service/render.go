package service

import (
	"context"
	"html"
	"strings"

	"github.com/google/uuid"
	commentdomain "github.com/turahe/blog-api/internal/core/comment/domain"
	"github.com/turahe/blog-api/internal/core/comment/ports"
)

// escapeRenderer is the fallback when no markdown renderer is configured: it escapes
// the text and keeps paragraphs, so ContentHTML is always safe to embed.
type escapeRenderer struct{}

func (escapeRenderer) Render(source string) string {
	if source == "" {
		return ""
	}

	var b strings.Builder

	for para := range strings.SplitSeq(strings.ReplaceAll(source, "\r\n", "\n"), "\n\n") {
		if para = strings.TrimSpace(para); para == "" {
			continue
		}

		b.WriteString("<p>")
		b.WriteString(strings.ReplaceAll(html.EscapeString(para), "\n", "<br>"))
		b.WriteString("</p>\n")
	}

	return b.String()
}

// renderingRepository fills ContentHTML on comments stored before it was persisted.
type renderingRepository struct {
	ports.Repository

	renderer ports.Renderer
}

func (r renderingRepository) fill(comment commentdomain.Comment) commentdomain.Comment {
	if comment.ContentHTML == "" && comment.Content != "" {
		comment.ContentHTML = r.renderer.Render(comment.Content)
	}

	return comment
}

func (r renderingRepository) fillAll(comments []commentdomain.Comment) []commentdomain.Comment {
	for i := range comments {
		comments[i] = r.fill(comments[i])
	}

	return comments
}

func (r renderingRepository) GetByID(ctx context.Context, id uuid.UUID) (commentdomain.Comment, error) {
	comment, err := r.Repository.GetByID(ctx, id)

	return r.fill(comment), err
}

func (r renderingRepository) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]commentdomain.Comment, error) {
	comments, err := r.Repository.GetByIDs(ctx, ids)

	return r.fillAll(comments), err
}

func (r renderingRepository) List(ctx context.Context, filter commentdomain.ListFilter) (commentdomain.ListResult, error) {
	result, err := r.Repository.List(ctx, filter)
	result.Items = r.fillAll(result.Items)

	return result, err
}

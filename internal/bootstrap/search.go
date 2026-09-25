package bootstrap

import (
	"context"
	"log/slog"

	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	postports "github.com/turahe/blog-api/internal/core/post/ports"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
)

// newPostSearch queries with the language posts.search_vector was built with, so results
// stay correct when SEARCH_LANGUAGE changed but the index was not rebuilt yet. It returns nil,
// leaving search disabled, when the column is missing.
func newPostSearch(ctx context.Context, cfg config.Config, db *database.Database, logger *slog.Logger) postports.Searcher {
	language, err := persistence.IndexedSearchLanguage(ctx, db.GORM)
	if err != nil {
		logger.Warn("post search disabled", "error", err)
		return nil
	}

	if language != cfg.SearchLanguage {
		logger.Warn("post search index uses a different language than SEARCH_LANGUAGE; run `app search reindex`",
			"indexed", language, "configured", cfg.SearchLanguage)
	}

	return persistence.NewPostSearch(db.GORM, language)
}

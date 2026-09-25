package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
)

func newSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Manage the post full-text search index",
	}
	cmd.AddCommand(newSearchReindexCmd())

	return cmd
}

func newSearchReindexCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reindex",
		Short: "Rebuild the post search index for SEARCH_LANGUAGE",
		Long: "The index follows every post write on its own; reindex is only needed after changing " +
			"SEARCH_LANGUAGE. It rewrites the posts table under an exclusive lock, so writes and reads " +
			"of posts wait until it finishes. Restart app serve afterwards so searches use the new language.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			cfg, err := config.LoadBackground()
			if err != nil {
				return err
			}

			db, err := database.Open(cmd.Context(), cfg)
			if err != nil {
				return err
			}

			defer func() { err = errors.Join(err, db.Close()) }()

			if err := persistence.RebuildPostSearchIndex(cmd.Context(), db.GORM, cfg.SearchLanguage); err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "rebuilt the post search index for %q\n", cfg.SearchLanguage)

			return err
		},
	}
}

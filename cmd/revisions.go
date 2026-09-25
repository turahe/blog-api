package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/turahe/blog-api/internal/adapters/outbound/persistence"
	"github.com/turahe/blog-api/internal/platform/config"
	"github.com/turahe/blog-api/internal/platform/database"
)

func newRevisionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revisions",
		Short: "Manage post revision history",
	}
	cmd.AddCommand(newRevisionsPruneCmd())

	return cmd
}

func newRevisionsPruneCmd() *cobra.Command {
	var keep int

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Delete all but the newest revisions of every post",
		Long:  "Revisions are kept until pruned. prune keeps the newest --keep revisions of each post (at least 1) and deletes the rest.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) (err error) {
			if keep < 1 {
				return errors.New("--keep must be at least 1")
			}

			cfg, err := config.LoadBackground()
			if err != nil {
				return err
			}

			db, err := database.Open(cmd.Context(), cfg)
			if err != nil {
				return err
			}

			defer func() { err = errors.Join(err, db.Close()) }()

			deleted, err := persistence.NewPostRevisionRepository(db.GORM).Prune(cmd.Context(), keep)
			if err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "deleted %d post revisions (kept the newest %d per post)\n", deleted, keep)

			return err
		},
	}
	cmd.Flags().IntVar(&keep, "keep", 0, "revisions to keep per post (required, at least 1)")
	_ = cmd.MarkFlagRequired("keep")

	return cmd
}

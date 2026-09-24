package cmd

import (
	"errors"

	"github.com/spf13/cobra"
)

func newSchedulerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scheduler",
		Short: "Run recurring background jobs",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return errors.New("scheduler is scaffolded but not implemented")
		},
	}
}

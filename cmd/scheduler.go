package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newSchedulerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scheduler",
		Short: "Run recurring background jobs",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return fmt.Errorf("%q is scaffolded but not implemented", "scheduler")
		},
	}
}

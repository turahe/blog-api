package cmd

import (
	"github.com/spf13/cobra"
)

const (
	groupRuntime = "runtime"
	groupData    = "data"
	groupOps     = "ops"
)

var (
	version   = "0.1.0-dev"
	commit    = "unknown"
	buildTime = "unknown"

	// envFile is bound to --env-file on the root command.
	envFile string
)

// Execute runs the root command tree. The repo-root main package calls this;
// tests build a fresh tree via newRootCmd so flag state does not leak across cases.
func Execute() error {
	return newRootCmd().Execute()
}

func newRootCmd() *cobra.Command {
	envFile = ""
	cmd := &cobra.Command{
		Use:           "app",
		Short:         "Blog Platform API",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(c *cobra.Command, _ []string) error {
			switch c.Name() {
			case "help", "completion":
				return nil
			}

			path, err := resolveEnvFile(envFile)
			if err != nil {
				return err
			}

			if path == "" {
				return nil
			}

			return loadEnvFile(path)
		},
	}
	cmd.AddGroup(
		&cobra.Group{ID: groupRuntime, Title: "Runtime Commands:"},
		&cobra.Group{ID: groupData, Title: "Data Commands:"},
		&cobra.Group{ID: groupOps, Title: "Ops Commands:"},
	)
	cmd.PersistentFlags().StringVar(
		&envFile,
		"env-file",
		"",
		`load KEY=VALUE env file before config (existing env wins; default: .env when present; "-" disables)`,
	)

	serveCmd := newServeCmd()
	serveCmd.GroupID = groupRuntime
	workerCmd := newWorkerCmd()
	workerCmd.GroupID = groupRuntime
	schedulerCmd := newSchedulerCmd()
	schedulerCmd.GroupID = groupRuntime

	migrateCmd := newMigrateCmd()
	migrateCmd.GroupID = groupData
	seedCmd := newSeedCmd()
	seedCmd.GroupID = groupData

	doctorCmd := newDoctorCmd()
	doctorCmd.GroupID = groupOps
	versionCmd := newVersionCmd()
	versionCmd.GroupID = groupOps

	cmd.AddCommand(
		serveCmd,
		workerCmd,
		schedulerCmd,
		migrateCmd,
		seedCmd,
		doctorCmd,
		versionCmd,
	)

	return cmd
}

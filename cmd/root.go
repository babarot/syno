package cmd

import (
	"github.com/spf13/cobra"
)

func NewRootCmd(version, revision string) *cobra.Command {
	root := &cobra.Command{
		Use:           "syno",
		Short:         "Find and inspect Synology NAS on the local network",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version + " (" + revision + ")",
	}

	root.AddCommand(
		newDiscoverCmd(),
		newLoginCmd(),
		newStatusCmd(),
	)

	return root
}

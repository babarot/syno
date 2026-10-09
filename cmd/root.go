package cmd

import (
	"github.com/spf13/cobra"

	"github.com/babarot/syno/internal/version"
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "syno",
		Short:         "Find and inspect Synology NAS on the local network",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.String(),
	}

	root.PersistentFlags().StringVarP(&profileFlag, "profile", "p", "", "Profile to use (default: $SYNO_PROFILE, then the current profile)")

	root.AddCommand(
		newDiscoverCmd(),
		newLoginCmd(),
		newProfileCmd(),
		newStatusCmd(),
		newDoctorCmd(),
		newAPICmd(),
		newContainerCmd(),
	)

	return root
}

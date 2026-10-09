package main

import (
	"fmt"
	"os"

	"github.com/babarot/syno/cmd"
)

var (
	version  = "dev"
	revision = "HEAD"
)

func main() {
	if err := cmd.NewRootCmd(version, revision).Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/babarot/syno/cmd"
)

var (
	version  = "dev"
	revision = "HEAD"
)

func main() {
	err := cmd.NewRootCmd(version, revision).Execute()
	if err == nil {
		return
	}
	code := 1
	var exitErr *cmd.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.Code
		err = exitErr.Err
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
	os.Exit(code)
}

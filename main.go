package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/babarot/syno/cmd"
)

func main() {
	err := cmd.NewRootCmd().Execute()
	if err == nil {
		return
	}
	code := 1
	if exitErr, ok := errors.AsType[*cmd.ExitError](err); ok {
		code = exitErr.Code
		err = exitErr.Err
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
	os.Exit(code)
}

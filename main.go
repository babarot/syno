package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/babarot/syno/cmd"
)

func main() {
	// Ctrl-C cancels the context, so that deferred cleanup, such as stopping
	// tasks started on the NAS, runs before exiting. A second one exits at
	// once.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
	}()
	err := cmd.NewRootCmd().ExecuteContext(ctx)
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

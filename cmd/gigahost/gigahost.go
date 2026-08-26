// Command gigahost is the CLI for the Gigahost API. The command tree
// is defined in package cli; this binary is a thin wrapper that wires
// together stdin/stdout/stderr, version info and the OS signal handler
// that cancels the request context on Ctrl-C.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/kradalby/gigahost-go/cli"
)

// These are set by the Go linker or goreleaser via -ldflags "-X main.version=...".
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	code := cli.Run(ctx, os.Args[1:], cli.Options{
		Version: version,
		Commit:  commit,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	})

	cancel()
	os.Exit(code)
}

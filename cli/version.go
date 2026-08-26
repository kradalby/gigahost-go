package cli

import (
	"context"
	"fmt"

	"github.com/peterbourgon/ff/v4"
)

func newVersionCmd(opts Options, parent *ff.FlagSet) *ff.Command {
	fs := ff.NewFlagSet("version").SetParent(parent)

	return &ff.Command{
		Name:      "version",
		Usage:     "gigahost version",
		ShortHelp: "Print gigahost version info.",
		Flags:     fs,
		Exec: func(_ context.Context, _ []string) error {
			version := opts.Version
			if version == "" {
				version = "dev"
			}

			commit := opts.Commit
			if commit == "" {
				commit = "unknown"
			}

			fmt.Fprintf(opts.Stdout, "gigahost %s (%s)\n", version, commit)

			return nil
		},
	}
}

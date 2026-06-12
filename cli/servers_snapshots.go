package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/peterbourgon/ff/v4"
)

// snapshotRow is the trimmed projection used for table output of snapshots.
type snapshotRow struct {
	ID          int64  `cli:"ID"`
	DisplayName string `cli:"Name"`
	State       string `cli:"State"`
}

func newServersSnapshotsCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("snapshots").SetParent(parent)

	var name string

	fs.StringVar(&name, 'n', "name", "", "snapshot name (for create)")

	cmd := &ff.Command{
		Name:      "snapshots",
		Usage:     "gigahost servers snapshots COMMAND SERVER",
		ShortHelp: "Manage VPS snapshots.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		{
			Name:  "list",
			Usage: "gigahost servers snapshots list SERVER",
			Flags: ff.NewFlagSet("list").SetParent(fs),
			Exec: func(ctx context.Context, args []string) error {
				if len(args) != 1 {
					return errors.New("exactly one SERVER argument is required")
				}

				if err := load(); err != nil {
					return err
				}

				client, err := c.Client()
				if err != nil {
					return err
				}

				serverID, err := resolveServerArg(ctx, client, args[0])
				if err != nil {
					return err
				}

				snaps, err := client.Snapshots.List(ctx, serverID)
				if err != nil {
					return err
				}

				view := make([]snapshotRow, 0, len(snaps))
				for _, s := range snaps {
					view = append(view, snapshotRow{
						ID:          s.ID,
						DisplayName: s.DisplayName,
						State:       string(s.State),
					})
				}

				return c.Render(view)
			},
		},
		{
			Name:  "create",
			Usage: "gigahost servers snapshots create --name NAME SERVER",
			Flags: ff.NewFlagSet("create").SetParent(fs),
			Exec: func(ctx context.Context, args []string) error {
				if len(args) != 1 {
					return errors.New("exactly one SERVER argument is required")
				}

				if name == "" {
					return errors.New("--name is required")
				}

				if err := load(); err != nil {
					return err
				}

				client, err := c.Client()
				if err != nil {
					return err
				}

				serverID, err := resolveServerArg(ctx, client, args[0])
				if err != nil {
					return err
				}

				if err := client.Snapshots.Create(ctx, serverID, name); err != nil {
					return err
				}

				// Create is async; list and match on DisplayName to get the ID.
				// A lookup failure is non-fatal: the snapshot creation was triggered.
				if snaps, lookupErr := client.Snapshots.List(ctx, serverID); lookupErr == nil {
					for _, s := range snaps {
						if s.DisplayName == name {
							return c.Render(snapshotRow{
								ID:          s.ID,
								DisplayName: s.DisplayName,
								State:       string(s.State),
							})
						}
					}
				}

				if c.format == outputTable {
					fmt.Fprintf(c.Out, "Snapshot %q is being created on server %s\n", name, serverID)
				}

				return nil
			},
		},
		{
			Name:  "delete",
			Usage: "gigahost servers snapshots delete SERVER SNAPSHOT_ID",
			Flags: ff.NewFlagSet("delete").SetParent(fs),
			Exec: func(ctx context.Context, args []string) error {
				if len(args) != 2 {
					return errors.New("two arguments are required: SERVER SNAPSHOT_ID")
				}

				snapID, err := strconv.ParseInt(args[1], 10, 64)
				if err != nil {
					return fmt.Errorf("invalid snapshot ID %q: %w", args[1], err)
				}

				if err := load(); err != nil {
					return err
				}

				client, err := c.Client()
				if err != nil {
					return err
				}

				serverID, err := resolveServerArg(ctx, client, args[0])
				if err != nil {
					return err
				}

				if err := client.Snapshots.Delete(ctx, serverID, snapID); err != nil {
					return err
				}

				fmt.Fprintf(c.Out, "Snapshot %d deleted from server %s\n", snapID, serverID)

				return nil
			},
		},
	}

	return cmd
}

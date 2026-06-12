package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	gigahost "github.com/kradalby/gigahost-go/client"
	"github.com/peterbourgon/ff/v4"
)

func newServersISOsCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("isos").SetParent(parent)

	cmd := &ff.Command{
		Name:      "isos",
		Usage:     "gigahost servers isos COMMAND SERVER",
		ShortHelp: "List and mount uploaded ISO images.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		{
			Name:  "list",
			Usage: "gigahost servers isos list SERVER",
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

				isos, err := client.ISOs.List(ctx, serverID)
				if err != nil {
					return err
				}

				type serverISORow struct {
					ID      string `cli:"ID"`
					Name    string `cli:"Name"`
					State   string `cli:"State"`
					Mounted bool   `cli:"Mounted"`
					Size    int64  `cli:"Size (bytes)"`
				}

				view := make([]serverISORow, 0, len(isos))
				for _, iso := range isos {
					view = append(view, serverISORow{
						ID:      iso.ID,
						Name:    iso.Name,
						State:   iso.State,
						Mounted: iso.Mounted,
						Size:    iso.Size,
					})
				}

				return c.Render(view)
			},
		},
		{
			Name:  "mount",
			Usage: "gigahost servers isos mount SERVER ISO",
			Flags: ff.NewFlagSet("mount").SetParent(fs),
			Exec: func(ctx context.Context, args []string) error {
				if len(args) != 2 {
					return errors.New("two arguments are required: SERVER ISO")
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

				iso, err := resolveServerISO(ctx, client, serverID, args[1])
				if err != nil {
					return err
				}

				if err := client.ISOs.Mount(ctx, serverID, iso.ID); err != nil {
					return err
				}

				fmt.Fprintf(c.Out, "ISO %q mounted on server %s\n", iso.Name, serverID)

				return nil
			},
		},
	}

	return cmd
}

// resolveServerISO matches an ISO name or ID against the server's
// uploaded ISOs: ID or exact name (case-insensitive) wins outright,
// otherwise a unique substring match.
func resolveServerISO(ctx context.Context, client *gigahost.Client, serverID, ref string) (*gigahost.ISO, error) {
	isos, err := client.ISOs.List(ctx, serverID)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(isos))

	for i := range isos {
		if isos[i].ID == ref || strings.EqualFold(isos[i].Name, ref) {
			return &isos[i], nil
		}

		names[i] = isos[i].Name
	}

	var matched []int

	for i, name := range names {
		if strings.Contains(strings.ToLower(name), strings.ToLower(strings.TrimSpace(ref))) {
			matched = append(matched, i)
		}
	}

	switch len(matched) {
	case 1:
		return &isos[matched[0]], nil
	case 0:
		return nil, fmt.Errorf("iso %q not found; available: %s", ref, strings.Join(names, ", "))
	default:
		matchedNames := make([]string, len(matched))
		for i, j := range matched {
			matchedNames[i] = names[j]
		}

		return nil, fmt.Errorf("iso %q is ambiguous; matches: %s", ref, strings.Join(matchedNames, ", "))
	}
}

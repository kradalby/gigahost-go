package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/peterbourgon/ff/v4"

	gigahost "github.com/kradalby/gigahost-go/client"
)

func newServersUpgradesCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("upgrades").SetParent(parent)

	cmd := &ff.Command{
		Name:      "upgrades",
		Usage:     "gigahost servers upgrades COMMAND SERVER",
		ShortHelp: "List and apply package upgrades.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		{
			Name:      "list",
			Usage:     "gigahost servers upgrades list SERVER",
			ShortHelp: "List available upgrade packages.",
			Flags:     ff.NewFlagSet("list").SetParent(fs),
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

				pkgs, err := client.Upgrades.List(ctx, serverID)
				if err != nil {
					return err
				}

				return c.Render(pkgs)
			},
		},
		{
			Name:      "apply",
			Usage:     "gigahost servers upgrades apply SERVER PACKAGE",
			ShortHelp: "Upgrade the server to a new package.",
			Flags:     ff.NewFlagSet("apply").SetParent(fs),
			Exec: func(ctx context.Context, args []string) error {
				if len(args) != 2 {
					return errors.New("two arguments are required: SERVER PACKAGE")
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

				pkg, err := resolveUpgradePackage(ctx, client, serverID, args[1])
				if err != nil {
					return err
				}

				if err := client.Upgrades.Apply(ctx, serverID, pkg.ProductID); err != nil {
					return err
				}

				fmt.Fprintf(c.Out, "Server %s upgrading to package %s\n", serverID, pkg.ProductName)

				return nil
			},
		},
	}

	return cmd
}

// resolveUpgradePackage matches a product name or ID against the server's
// available upgrade packages: ID or exact name (case-insensitive) wins
// outright, otherwise a unique substring match.
func resolveUpgradePackage(ctx context.Context, client *gigahost.Client, serverID, ref string) (*gigahost.UpgradePackage, error) {
	pkgs, err := client.Upgrades.List(ctx, serverID)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(pkgs))

	for i := range pkgs {
		if pkgs[i].ProductID == ref || strings.EqualFold(pkgs[i].ProductName, ref) {
			return &pkgs[i], nil
		}

		names[i] = pkgs[i].ProductName
	}

	var matched []int

	for i, name := range names {
		if strings.Contains(strings.ToLower(name), strings.ToLower(strings.TrimSpace(ref))) {
			matched = append(matched, i)
		}
	}

	switch len(matched) {
	case 1:
		return &pkgs[matched[0]], nil
	case 0:
		return nil, fmt.Errorf("package %q not found; available: %s", ref, strings.Join(names, ", "))
	default:
		matchedNames := make([]string, len(matched))
		for i, j := range matched {
			matchedNames[i] = names[j]
		}

		return nil, fmt.Errorf("package %q is ambiguous; matches: %s", ref, strings.Join(matchedNames, ", "))
	}
}

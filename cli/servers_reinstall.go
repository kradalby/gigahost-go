package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/peterbourgon/ff/v4"

	gigahost "github.com/kradalby/gigahost-go/client"
)

func newServersReinstallCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("reinstall").SetParent(parent)

	var (
		osRef    string
		language string
		keyboard string
		timezone string
		hostname string
	)

	fs.StringVar(&osRef, 0, "os", "", "operating system slug, codename, name, or ID (see `gigahost deploy os`)")
	fs.StringVar(&language, 0, "language", "en_US", "OS language locale")
	fs.StringVar(&keyboard, 0, "keyboard", "us", "keyboard layout")
	fs.StringVar(&timezone, 0, "timezone", "Europe/Oslo", "timezone")
	fs.StringVar(&hostname, 0, "hostname", "", "new hostname (defaults to the current one)")

	cmd := &ff.Command{
		Name:      "reinstall",
		Usage:     "gigahost servers reinstall COMMAND",
		ShortHelp: "Reinstall the operating system on a server.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		{
			Name:      "distros",
			Usage:     "gigahost servers reinstall distros",
			ShortHelp: "List available distributions.",
			Flags:     ff.NewFlagSet("distros").SetParent(fs),
			Exec: func(ctx context.Context, _ []string) error {
				if err := load(); err != nil {
					return err
				}

				client, err := c.Client()
				if err != nil {
					return err
				}

				distros, err := client.Reinstall.ListDistributions(ctx)
				if err != nil {
					return err
				}

				return c.Render(distros)
			},
		},
		{
			Name:      "os",
			Usage:     "gigahost servers reinstall os DISTRO_ID",
			ShortHelp: "List available operating systems for a distribution.",
			Flags:     ff.NewFlagSet("os").SetParent(fs),
			Exec: func(ctx context.Context, args []string) error {
				if len(args) != 1 {
					return errors.New("exactly one DISTRO_ID argument is required")
				}

				if err := load(); err != nil {
					return err
				}

				client, err := c.Client()
				if err != nil {
					return err
				}

				oses, err := client.Reinstall.ListOperatingSystems(ctx, args[0])
				if err != nil {
					return err
				}

				return c.Render(oses)
			},
		},
		{
			Name:      "run",
			Usage:     "gigahost servers reinstall run --os OS --hostname H [flags] SERVER",
			ShortHelp: "Reinstall a server. This destroys all data on the target.",
			Flags:     ff.NewFlagSet("run").SetParent(fs),
			Exec: func(ctx context.Context, args []string) error {
				if len(args) != 1 {
					return errors.New("exactly one SERVER argument is required")
				}

				if osRef == "" {
					return errors.New("--os is required")
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

				resolved, err := client.Reinstall.ResolveOS(ctx, osRef)
				if err != nil {
					return err
				}

				fmt.Fprintf(c.Out, "Reinstalling %s with %s...\n", serverID, resolved.Slug)

				result, err := client.Reinstall.Reinstall(ctx, serverID, gigahost.ReinstallRequest{
					OSID:     resolved.OS.ID,
					Language: language,
					Keyboard: keyboard,
					Timezone: timezone,
					Hostname: hostname,
				})
				if err != nil {
					return err
				}

				fmt.Fprintf(c.Out, "Reinstall initiated on %s\n", serverID)

				if result.RootPasswd != "" {
					fmt.Fprintf(c.Out, "Root password: %s\n", result.RootPasswd)
				}

				fmt.Fprintf(c.Out, "Reboot required: %v\n", result.Reboot)

				return nil
			},
		},
	}

	return cmd
}

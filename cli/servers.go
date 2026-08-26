package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/peterbourgon/ff/v4"

	gigahost "github.com/kradalby/gigahost-go/client"
)

func newServersCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("servers").SetParent(parent)

	cmd := &ff.Command{
		Name:      "servers",
		Usage:     "gigahost servers COMMAND",
		ShortHelp: "Manage virtual and dedicated servers.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		newServersListCmd(c, fs, load),
		newServersGetCmd(c, fs, load),
		newServersPowerCmd(c, fs, load),
		newServersRebootCmd(c, fs, load),
		newServersRenameCmd(c, fs, load),
		newServersReverseCmd(c, fs, load),
		newServersIPOrderCmd(c, fs, load),
		newServersGraphsCmd(c, fs, load),
		newServersIPsCmd(c, fs, load),
		newServersSnapshotsCmd(c, fs, load),
		newServersReinstallCmd(c, fs, load),
		newServersIPMICmd(c, fs, load),
		newServersISOsCmd(c, fs, load),
		newServersUpgradesCmd(c, fs, load),
	}

	return cmd
}

func newServersListCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("list").SetParent(parent)

	return &ff.Command{
		Name:      "list",
		Usage:     "gigahost servers list",
		ShortHelp: "List all servers.",
		Flags:     fs,
		Exec: func(ctx context.Context, _ []string) error {
			if err := load(); err != nil {
				return err
			}

			client, err := c.Client()
			if err != nil {
				return err
			}

			servers, err := client.Servers.List(ctx)
			if err != nil {
				return err
			}

			view := make([]serversRow, 0, len(servers))
			for _, s := range servers {
				// Deploy-time hostnames land in srv_name; srv_hostname is
				// often empty on fresh servers.
				hostname := s.Hostname
				if hostname == "" {
					hostname = s.Name
				}

				view = append(view, serversRow{
					ID:        s.ID,
					Hostname:  hostname,
					Type:      s.Type,
					VPSType:   s.VPSType,
					Cores:     s.Cores,
					RAM:       s.RAM,
					Status:    statusString(s.Status),
					Suspended: s.Suspended,
					Location:  s.Location,
				})
			}

			return c.Render(view)
		},
	}
}

func newServersGetCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("get").SetParent(parent)

	return &ff.Command{
		Name:      "get",
		Usage:     "gigahost servers get SERVER",
		ShortHelp: "Show detailed information about a server (by ID or hostname).",
		Flags:     fs,
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

			id, err := resolveServerArg(ctx, client, args[0])
			if err != nil {
				return err
			}

			server, err := client.Servers.Get(ctx, id)
			if err != nil {
				return err
			}

			return c.Render(server)
		},
	}
}

func newServersPowerCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("power").SetParent(parent)

	cmd := &ff.Command{
		Name:      "power",
		Usage:     "gigahost servers power {on|off|state} SERVER",
		ShortHelp: "Control server power state.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		{
			Name:  "on",
			Usage: "gigahost servers power on SERVER",
			Flags: ff.NewFlagSet("on").SetParent(fs),
			Exec: func(ctx context.Context, args []string) error {
				return serverPowerOp(ctx, c, load, args, func(client *gigahost.Client, id string) error {
					return client.Servers.PowerOn(ctx, id)
				}, "Powered on")
			},
		},
		{
			Name:  "off",
			Usage: "gigahost servers power off SERVER",
			Flags: ff.NewFlagSet("off").SetParent(fs),
			Exec: func(ctx context.Context, args []string) error {
				return serverPowerOp(ctx, c, load, args, func(client *gigahost.Client, id string) error {
					return client.Servers.PowerOff(ctx, id)
				}, "Powered off")
			},
		},
		{
			Name:  "state",
			Usage: "gigahost servers power state SERVER",
			Flags: ff.NewFlagSet("state").SetParent(fs),
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

				id, err := resolveServerArg(ctx, client, args[0])
				if err != nil {
					return err
				}

				state, err := client.Servers.GetPowerState(ctx, id)
				if err != nil {
					return err
				}

				fmt.Fprintf(c.Out, "power=%v timestamp=%s\n", state.PowerState, state.Timestamp.Format("2006-01-02T15:04:05Z07:00"))

				return nil
			},
		},
	}

	return cmd
}

func newServersRebootCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("reboot").SetParent(parent)

	return &ff.Command{
		Name:      "reboot",
		Usage:     "gigahost servers reboot SERVER",
		ShortHelp: "Reboot a server.",
		Flags:     fs,
		Exec: func(ctx context.Context, args []string) error {
			return serverPowerOp(ctx, c, load, args, func(client *gigahost.Client, id string) error {
				return client.Servers.Reboot(ctx, id)
			}, "Rebooting")
		},
	}
}

func newServersRenameCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("rename").SetParent(parent)

	var name string

	fs.StringVar(&name, 'n', "name", "", "new descriptive name")

	return &ff.Command{
		Name:      "rename",
		Usage:     "gigahost servers rename --name NAME SERVER",
		ShortHelp: "Update the descriptive name of a server.",
		Flags:     fs,
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

			id, err := resolveServerArg(ctx, client, args[0])
			if err != nil {
				return err
			}

			if err := client.Servers.UpdateName(ctx, id, name); err != nil {
				return err
			}

			// Re-fetch to render the updated server row.
			// A lookup failure is non-fatal: the rename succeeded.
			if server, lookupErr := client.Servers.Get(ctx, id); lookupErr == nil {
				hostname := server.Hostname
				if hostname == "" {
					hostname = server.Name
				}

				return c.Render(serversRow{
					ID:        server.ID,
					Hostname:  hostname,
					Type:      server.Type,
					VPSType:   server.VPSType,
					Cores:     server.Cores,
					RAM:       server.RAM,
					Status:    statusString(server.Status),
					Suspended: server.Suspended,
					Location:  server.Location,
				})
			}

			if c.format == outputTable {
				fmt.Fprintf(c.Out, "Renamed server %s to %q\n", id, name)
			}

			return nil
		},
	}
}

// resolveServerArg resolves a SERVER positional (numeric ID or hostname) to
// its ID.
func resolveServerArg(ctx context.Context, cl *gigahost.Client, ref string) (string, error) {
	srv, err := cl.Servers.Resolve(ctx, ref)
	if err != nil {
		return "", err
	}

	return srv.ID, nil
}

// serverPowerOp is a tiny helper for the repetitive shape of power
// commands: validate args, load config, resolve the server, hit the API,
// announce success.
func serverPowerOp(
	ctx context.Context,
	c *Context,
	load func() error,
	args []string,
	op func(*gigahost.Client, string) error,
	announce string,
) error {
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

	id, err := resolveServerArg(ctx, client, args[0])
	if err != nil {
		return err
	}

	if err := op(client, id); err != nil {
		return err
	}

	fmt.Fprintf(c.Out, "%s server %s\n", announce, id)

	return nil
}

// serversRow is the trimmed projection used for table output.
type serversRow struct {
	ID        string `cli:"ID"`
	Hostname  string `cli:"Hostname"`
	Type      string `cli:"Type"`
	VPSType   string `cli:"Virt"`
	Cores     int    `cli:"Cores"`
	RAM       int    `cli:"RAM (GB)"`
	Status    string `cli:"Status"`
	Suspended bool   `cli:"Suspended"`
	Location  string `cli:"Location"`
}

func statusString(on bool) string {
	if on {
		return "up"
	}

	return "down"
}

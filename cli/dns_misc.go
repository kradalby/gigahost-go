package cli

import (
	"context"
	"errors"
	"strings"

	gigahost "github.com/kradalby/gigahost-go/client"
	"github.com/peterbourgon/ff/v4"
)

func newDNSNameserversCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("nameservers").SetParent(parent)

	var nameservers []string

	fs.StringListVar(&nameservers, 'n', "nameserver", "nameserver to delegate to (repeatable, minimum 2)")

	cmd := &ff.Command{
		Name:      "nameservers",
		Usage:     "gigahost dns nameservers COMMAND ZONE",
		ShortHelp: "Manage nameserver delegation for a registered domain.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		{
			Name:  "set",
			Usage: "gigahost dns nameservers set --nameserver NS1 --nameserver NS2 ZONE",
			Flags: ff.NewFlagSet("set").SetParent(fs),
			Exec: func(ctx context.Context, args []string) error {
				if len(args) != 1 {
					return errors.New("exactly one ZONE argument is required")
				}

				if len(nameservers) < 2 {
					return errors.New("at least two --nameserver values are required (Norid policy)")
				}

				if err := load(); err != nil {
					return err
				}

				client, err := c.Client()
				if err != nil {
					return err
				}

				zid, err := resolveZoneArg(ctx, client, args[0])
				if err != nil {
					return err
				}

				if err := client.DNS.SetNameservers(ctx, zid, nameservers); err != nil {
					return err
				}

				type nsRow struct {
					Zone        string `cli:"Zone"`
					Nameservers string `cli:"Nameservers"`
				}

				return c.Render(nsRow{
					Zone:        args[0],
					Nameservers: strings.Join(nameservers, ", "),
				})
			},
		},
	}

	return cmd
}

func newDNSPTRCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("ptr").SetParent(parent)

	var (
		prefix   string
		version  string
		zoneName string
	)

	fs.StringVar(&prefix, 0, "prefix", "", "IP prefix, e.g. \"185.181.63\" or \"2a03:94e0::\"")
	fs.StringEnumVar(&version, 0, "version", "IP version", "ipv4", "ipv6")
	fs.StringVar(&zoneName, 'z', "zone-name", "", "PTR zone name, e.g. 63.181.185.in-addr.arpa")

	cmd := &ff.Command{
		Name:      "ptr",
		Usage:     "gigahost dns ptr COMMAND",
		ShortHelp: "Manage reverse DNS (PTR) zones.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		{
			Name:  "create",
			Usage: "gigahost dns ptr create --prefix P --version ipv4|ipv6 --zone-name NAME",
			Flags: ff.NewFlagSet("create").SetParent(fs),
			Exec: func(ctx context.Context, _ []string) error {
				if prefix == "" || version == "" || zoneName == "" {
					return errors.New("--prefix, --version and --zone-name are required")
				}

				if err := load(); err != nil {
					return err
				}

				client, err := c.Client()
				if err != nil {
					return err
				}

				resp, err := client.DNS.CreatePTRZone(ctx, gigahost.CreatePTRZoneRequest{
					Prefix:    prefix,
					IPVersion: gigahost.IPVersion(version),
					ZoneName:  zoneName,
				})
				if err != nil {
					return err
				}

				type ptrRow struct {
					ZoneID   string `cli:"Zone ID"`
					ZoneName string `cli:"Zone Name"`
					Prefix   string `cli:"Prefix"`
					Version  string `cli:"IP Version"`
				}

				return c.Render(ptrRow{
					ZoneID:   resp.ZoneID,
					ZoneName: zoneName,
					Prefix:   prefix,
					Version:  version,
				})
			},
		},
	}

	return cmd
}

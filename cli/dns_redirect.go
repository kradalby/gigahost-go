package cli

import (
	"context"
	"errors"
	"fmt"

	gigahost "github.com/kradalby/gigahost-go/client"
	"github.com/peterbourgon/ff/v4"
)

// dnsRedirectRow is the table row for redirect listings.
type dnsRedirectRow struct {
	Domain    string `cli:"Domain"`
	Source    string `cli:"Source"`
	TargetURL string `cli:"Target URL"`
	Enabled   bool   `cli:"Enabled"`
	CreatedAt string `cli:"Created"`
}

func newDNSRedirectsCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("redirects").SetParent(parent)

	var (
		zoneID    string
		source    string
		targetURL string
	)

	fs.StringVar(&zoneID, 'z', "zone", "", "zone name or ID")
	fs.StringVar(&source, 's', "source", "@", "source subdomain (\"@\" for the zone apex)")
	fs.StringVar(&targetURL, 'V', "target-url", "", "redirect target URL")

	cmd := &ff.Command{
		Name:      "redirects",
		Usage:     "gigahost dns redirects COMMAND",
		ShortHelp: "Manage HTTP redirects for a hosted domain.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		{
			Name:  "list",
			Usage: "gigahost dns redirects list --zone ZONE",
			Flags: ff.NewFlagSet("list").SetParent(fs),
			Exec: func(ctx context.Context, _ []string) error {
				if zoneID == "" {
					return errors.New("--zone is required")
				}

				if err := load(); err != nil {
					return err
				}

				client, err := c.Client()
				if err != nil {
					return err
				}

				zid, err := resolveZoneArg(ctx, client, zoneID)
				if err != nil {
					return err
				}

				redirects, err := client.DNS.ListRedirects(ctx, zid)
				if err != nil {
					return err
				}

				view := make([]dnsRedirectRow, 0, len(redirects))
				for _, r := range redirects {
					view = append(view, dnsRedirectRow{
						Domain:    r.Domain,
						Source:    r.Source,
						TargetURL: r.TargetURL,
						Enabled:   r.Enabled,
						CreatedAt: shortTime(r.CreatedAt),
					})
				}

				return c.Render(view)
			},
		},
		{
			Name:  "create",
			Usage: "gigahost dns redirects create --zone ZONE [--source @] --target-url URL",
			Flags: ff.NewFlagSet("create").SetParent(fs),
			Exec: func(ctx context.Context, _ []string) error {
				if zoneID == "" || targetURL == "" {
					return errors.New("--zone and --target-url are required")
				}

				if err := load(); err != nil {
					return err
				}

				client, err := c.Client()
				if err != nil {
					return err
				}

				zid, err := resolveZoneArg(ctx, client, zoneID)
				if err != nil {
					return err
				}

				if err := client.DNS.CreateRedirect(ctx, zid, gigahost.CreateRedirectRequest{
					Source:    source,
					TargetURL: targetURL,
				}); err != nil {
					return err
				}

				// CreateRedirect returns nothing; list and match on source.
				redirects, err := client.DNS.ListRedirects(ctx, zid)
				if err != nil {
					return err
				}

				for _, r := range redirects {
					if r.Source == source {
						return c.Render(dnsRedirectRow{
							Domain:    r.Domain,
							Source:    r.Source,
							TargetURL: r.TargetURL,
							Enabled:   r.Enabled,
							CreatedAt: shortTime(r.CreatedAt),
						})
					}
				}

				if c.format == outputTable {
					fmt.Fprintln(c.Out, "Redirect created.")
				}

				return nil
			},
		},
		{
			Name:  "update",
			Usage: "gigahost dns redirects update --zone ZONE --source @ --target-url URL",
			Flags: ff.NewFlagSet("update").SetParent(fs),
			Exec: func(ctx context.Context, _ []string) error {
				if zoneID == "" || source == "" || targetURL == "" {
					return errors.New("--zone, --source and --target-url are required")
				}

				if err := load(); err != nil {
					return err
				}

				client, err := c.Client()
				if err != nil {
					return err
				}

				zid, err := resolveZoneArg(ctx, client, zoneID)
				if err != nil {
					return err
				}

				if err := client.DNS.UpdateRedirect(ctx, zid, source, targetURL); err != nil {
					return err
				}

				fmt.Fprintln(c.Out, "Redirect updated.")

				return nil
			},
		},
		{
			Name:  "delete",
			Usage: "gigahost dns redirects delete --zone ZONE --source @",
			Flags: ff.NewFlagSet("delete").SetParent(fs),
			Exec: func(ctx context.Context, _ []string) error {
				if zoneID == "" || source == "" {
					return errors.New("--zone and --source are required")
				}

				if err := load(); err != nil {
					return err
				}

				client, err := c.Client()
				if err != nil {
					return err
				}

				zid, err := resolveZoneArg(ctx, client, zoneID)
				if err != nil {
					return err
				}

				if err := client.DNS.DeleteRedirect(ctx, zid, source); err != nil {
					return err
				}

				fmt.Fprintln(c.Out, "Redirect deleted.")

				return nil
			},
		},
	}

	return cmd
}

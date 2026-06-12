package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	gigahost "github.com/kradalby/gigahost-go/client"
	"github.com/peterbourgon/ff/v4"
)

func newBGPCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("bgp").SetParent(parent)

	cmd := &ff.Command{
		Name:      "bgp",
		Usage:     "gigahost bgp COMMAND",
		ShortHelp: "Manage BGP peering.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		{
			Name:      "show",
			Usage:     "gigahost bgp show",
			ShortHelp: "Show BGP ASNs, prefixes and sessions.",
			Flags:     ff.NewFlagSet("show").SetParent(fs),
			Exec: func(ctx context.Context, _ []string) error {
				if err := load(); err != nil {
					return err
				}

				client, err := c.Client()
				if err != nil {
					return err
				}

				data, err := client.BGP.Get(ctx)
				if err != nil {
					return err
				}

				// For structured output (json/yaml) emit the full aggregate; for
				// table mode render the three sub-collections separately.
				if c.format != outputTable {
					return c.Render(data)
				}

				asnRows := make([]bgpASNRow, 0, len(data.ASNs))
				for _, a := range data.ASNs {
					asnRows = append(asnRows, bgpASNRow{
						ID:     a.ID,
						ASN:    a.ASN,
						Name:   a.Name,
						Status: a.Status,
					})
				}

				sessionRows := make([]bgpSessionRow, 0, len(data.Sessions))
				for _, s := range data.Sessions {
					sessionRows = append(sessionRows, bgpSessionRow{
						ID:           s.ID,
						ASNID:        s.ASNID,
						IPID:         s.IPID,
						IPAddress:    s.IPAddress,
						Status:       s.Status,
						DefaultRoute: s.DefaultRoute,
					})
				}

				if err := c.Render(asnRows); err != nil {
					return err
				}

				fmt.Fprintln(c.Out)

				return c.Render(sessionRows)
			},
		},
		newBGPASNCmd(c, fs, load),
		newBGPSessionCmd(c, fs, load),
	}

	return cmd
}

func newBGPASNCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("asn").SetParent(parent)

	cmd := &ff.Command{
		Name:      "asn",
		Usage:     "gigahost bgp asn COMMAND",
		ShortHelp: "Manage BGP ASNs.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		{
			Name:      "submit",
			Usage:     "gigahost bgp asn submit ASN",
			ShortHelp: "Submit a new ASN for approval.",
			Flags:     ff.NewFlagSet("submit").SetParent(fs),
			Exec: func(ctx context.Context, args []string) error {
				if len(args) != 1 {
					return errors.New("exactly one ASN argument is required")
				}

				if err := load(); err != nil {
					return err
				}

				client, err := c.Client()
				if err != nil {
					return err
				}

				if err := client.BGP.SubmitASN(ctx, args[0]); err != nil {
					return err
				}

				// SubmitASN returns nothing; look up via BGP.Get to render the record.
				// A lookup failure is non-fatal: the ASN was still submitted.
				if data, lookupErr := client.BGP.Get(ctx); lookupErr == nil {
					want := strings.TrimSpace(args[0])
					if strings.HasPrefix(strings.ToUpper(want), "AS") {
						want = want[2:]
					}

					for _, a := range data.ASNs {
						if a.ASN == want {
							return c.Render(bgpASNRow{
								ID:     a.ID,
								ASN:    a.ASN,
								Name:   a.Name,
								Status: a.Status,
							})
						}
					}
				}

				if c.format == outputTable {
					fmt.Fprintf(c.Out, "Submitted ASN %s for review.\n", args[0])
				}

				return nil
			},
		},
	}

	return cmd
}

func newBGPSessionCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("session").SetParent(parent)

	var (
		asnID     string
		asn       string
		server    string
		redundant bool
		defaultRt bool
		ipv4ID    string
		ipv6ID    string
		ipv4Addr  string
		ipv6Addr  string
	)

	fs.StringVar(&asnID, 'a', "asn-id", "", "ASN ID (numeric)")
	fs.StringVar(&asn, 0, "asn", "", "AS number, e.g. 212345 or AS212345")
	fs.StringVar(&server, 's', "server", "", "server ID or hostname owning the session IPs")
	fs.BoolVar(&redundant, 0, "redundant", "create redundant sessions")
	fs.BoolVar(&defaultRt, 0, "default-route", "receive default route")
	fs.StringVar(&ipv4ID, 0, "ipv4-id", "", "IP ID for IPv4 session")
	fs.StringVar(&ipv6ID, 0, "ipv6-id", "", "IP ID for IPv6 session")
	fs.StringVar(&ipv4Addr, 0, "ipv4", "", "IPv4 address for the session (requires --server)")
	fs.StringVar(&ipv6Addr, 0, "ipv6", "", "IPv6 address for the session (requires --server)")

	cmd := &ff.Command{
		Name:      "session",
		Usage:     "gigahost bgp session COMMAND",
		ShortHelp: "Manage BGP sessions.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		{
			Name:  "create",
			Usage: "gigahost bgp session create (--asn ASN | --asn-id N) [--redundant] [--default-route] [--server SRV --ipv4 ADDR | --ipv4-id N] [--ipv6 ADDR | --ipv6-id N]",
			Flags: ff.NewFlagSet("create").SetParent(fs),
			Exec: func(ctx context.Context, _ []string) error {
				if asnID == "" && asn == "" {
					return errors.New("--asn or --asn-id is required")
				}

				if (ipv4Addr != "" || ipv6Addr != "") && server == "" {
					return errors.New("--server is required with --ipv4/--ipv6")
				}

				if err := load(); err != nil {
					return err
				}

				client, err := c.Client()
				if err != nil {
					return err
				}

				resolvedASN := asnID

				if asn != "" {
					data, err := client.BGP.Get(ctx)
					if err != nil {
						return err
					}

					resolvedASN, err = resolveASNID(data.ASNs, asn)
					if err != nil {
						return err
					}
				}

				ipv4 := ipv4ID
				ipv6 := ipv6ID

				if ipv4Addr != "" || ipv6Addr != "" {
					srv, err := client.Servers.Resolve(ctx, server)
					if err != nil {
						return err
					}

					if ipv4Addr != "" {
						ipv4, err = resolveServerIPID(srv, ipv4Addr)
						if err != nil {
							return err
						}
					}

					if ipv6Addr != "" {
						ipv6, err = resolveServerIPID(srv, ipv6Addr)
						if err != nil {
							return err
						}
					}
				}

				if err := client.BGP.CreateSession(ctx, resolvedASN, gigahost.CreateBGPSessionRequest{
					Redundant:    redundant,
					DefaultRoute: defaultRt,
					IPIDv4:       ipv4,
					IPIDv6:       ipv6,
				}); err != nil {
					return err
				}

				// CreateSession returns nothing; look up via BGP.Get to get the session ID.
				// A lookup failure is non-fatal: the session was still created.
				if data, lookupErr := client.BGP.Get(ctx); lookupErr == nil {
					// Match: same ASN ID and IP IDs (either v4 or v6 will match).
					for _, s := range data.Sessions {
						if s.ASNID == resolvedASN && (s.IPID == ipv4 || s.IPID == ipv6) {
							return c.Render(bgpSessionRow{
								ID:           s.ID,
								ASNID:        s.ASNID,
								IPID:         s.IPID,
								IPAddress:    s.IPAddress,
								Status:       s.Status,
								DefaultRoute: s.DefaultRoute,
							})
						}
					}
				}

				if c.format == outputTable {
					fmt.Fprintln(c.Out, "BGP session created.")
				}

				return nil
			},
		},
		newBGPSessionDeleteCmd(c, fs, load),
	}

	return cmd
}

// bgpASNRow is the trimmed projection used for table output of ASN records.
type bgpASNRow struct {
	ID     string `cli:"ID"`
	ASN    string `cli:"ASN"`
	Name   string `cli:"Name"`
	Status string `cli:"Status"`
}

// bgpSessionRow is the trimmed projection used for table output of BGP sessions.
type bgpSessionRow struct {
	ID           string `cli:"ID"`
	ASNID        string `cli:"ASN ID"`
	IPID         string `cli:"IP ID"`
	IPAddress    string `cli:"IP Address"`
	Status       string `cli:"Status"`
	DefaultRoute bool   `cli:"Default Route"`
}

// resolveASNID resolves an AS number ("212345" or "AS212345") to the
// account's internal ASN ID.
func resolveASNID(asns []gigahost.BGPASN, ref string) (string, error) {
	want := strings.TrimSpace(ref)
	if strings.HasPrefix(strings.ToUpper(want), "AS") {
		want = want[2:]
	}

	known := make([]string, 0, len(asns))

	for _, a := range asns {
		if a.ASN == want {
			return a.ID, nil
		}

		known = append(known, "AS"+a.ASN)
	}

	return "", fmt.Errorf("asn %q not found; known ASNs: %s", ref, strings.Join(known, ", "))
}

// resolveServerIPID resolves an IP address assigned to a server to its
// internal IP ID.
func resolveServerIPID(srv *gigahost.Server, addr string) (string, error) {
	known := make([]string, 0, len(srv.IPs))

	for _, ip := range srv.IPs {
		if ip.Address == addr {
			return ip.ID, nil
		}

		known = append(known, ip.Address)
	}

	return "", fmt.Errorf("ip %q not found on server %s; known addresses: %s",
		addr, srv.Hostname, strings.Join(known, ", "))
}

func newBGPSessionDeleteCmd(c *Context, fs *ff.FlagSet, load func() error) *ff.Command {
	return &ff.Command{
		Name:  "delete",
		Usage: "gigahost bgp session delete SESSION_ID",
		Flags: ff.NewFlagSet("delete").SetParent(fs),
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return errors.New("exactly one SESSION_ID argument is required")
			}

			if err := load(); err != nil {
				return err
			}

			client, err := c.Client()
			if err != nil {
				return err
			}

			if err := client.BGP.DeleteSession(ctx, args[0]); err != nil {
				return err
			}

			fmt.Fprintf(c.Out, "Deleted BGP session %s\n", args[0])

			return nil
		},
	}
}

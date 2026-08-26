package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/peterbourgon/ff/v4"

	gigahost "github.com/kradalby/gigahost-go/client"
)

func newServersReverseCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("reverse").SetParent(parent)

	var (
		ip       string
		ipID     string
		subnetID string
		dns      string
	)

	fs.StringVar(&ip, 0, "ip", "", "IP address on the server (resolved to its IP/subnet ID)")
	fs.StringVar(&ipID, 0, "ip-id", "", "IPv4 IP ID (use for A records)")
	fs.StringVar(&subnetID, 0, "subnet-id", "", "IPv6 subnet ID (use for NS delegation)")
	fs.StringVar(&dns, 'd', "dns", "", "hostname to set as reverse record")

	return &ff.Command{
		Name:      "reverse",
		Usage:     "gigahost servers reverse [--ip ADDRESS|--ip-id|--subnet-id ID] --dns HOST SERVER",
		ShortHelp: "Update reverse DNS (rDNS) for a server IP or IPv6 subnet.",
		Flags:     fs,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return errors.New("exactly one SERVER argument is required")
			}

			if dns == "" || (ip == "" && ipID == "" && subnetID == "") {
				return errors.New("--dns and one of --ip/--ip-id/--subnet-id are required")
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

			if ip != "" {
				entry, err := resolveServerIP(ctx, client, serverID, ip)
				if err != nil {
					return err
				}

				ipID = entry.ID
				subnetID = entry.SubnetID
			}

			if err := client.Servers.UpdateReverse(ctx, serverID, gigahost.UpdateReverseRequest{
				IPID:     ipID,
				SubnetID: subnetID,
				DNS:      dns,
			}); err != nil {
				return err
			}

			// Re-fetch IPs to render the updated IP row (ID is needed for terraform import server_rdns).
			// A lookup failure is non-fatal: the reverse update succeeded.
			if server, lookupErr := client.Servers.Get(ctx, serverID); lookupErr == nil {
				// Find the IP we just updated.
				for _, sip := range server.IPs {
					if sip.ID == ipID || sip.SubnetID == subnetID {
						return c.Render(serverIPRow{
							IPID:     sip.ID,
							SubnetID: sip.SubnetID,
							Version:  sip.Version,
							Address:  sip.Address,
							Reverse:  sip.Reverse,
							Type:     sip.Type,
						})
					}
				}
			}

			if c.format == outputTable {
				fmt.Fprintf(c.Out, "Reverse DNS updated on server %s\n", serverID)
			}

			return nil
		},
	}
}

// resolveServerIP matches an IP address against the server's assigned IPs.
func resolveServerIP(ctx context.Context, client *gigahost.Client, serverID, address string) (*gigahost.ServerIP, error) {
	server, err := client.Servers.Get(ctx, serverID)
	if err != nil {
		return nil, err
	}

	addrs := make([]string, len(server.IPs))

	for i := range server.IPs {
		if strings.EqualFold(server.IPs[i].Address, address) {
			return &server.IPs[i], nil
		}

		addrs[i] = server.IPs[i].Address
	}

	return nil, fmt.Errorf("ip %q not found on server %s; available: %s",
		address, serverID, strings.Join(addrs, ", "))
}

func newServersIPsCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("ips").SetParent(parent)

	return &ff.Command{
		Name:      "ips",
		Usage:     "gigahost servers ips SERVER",
		ShortHelp: "List the IP addresses assigned to a server.",
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

			serverID, err := resolveServerArg(ctx, client, args[0])
			if err != nil {
				return err
			}

			server, err := client.Servers.Get(ctx, serverID)
			if err != nil {
				return err
			}

			view := make([]serverIPRow, 0, len(server.IPs))
			for _, ip := range server.IPs {
				view = append(view, serverIPRow{
					IPID:     ip.ID,
					SubnetID: ip.SubnetID,
					Version:  ip.Version,
					Address:  ip.Address,
					Reverse:  ip.Reverse,
					Type:     ip.Type,
				})
			}

			return c.Render(view)
		},
	}
}

// serverIPRow is the trimmed projection used for table output.
type serverIPRow struct {
	IPID     string `cli:"IP ID"`
	SubnetID string `cli:"Subnet ID"`
	Version  string `cli:"Version"`
	Address  string `cli:"Address"`
	Reverse  string `cli:"Reverse"`
	Type     string `cli:"Type"`
}

func newServersIPOrderCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("ip-order").SetParent(parent)

	var ipType string

	fs.StringEnumVar(&ipType, 0, "type", "IP order kind", "l3", "l2")

	return &ff.Command{
		Name:      "ip-order",
		Usage:     "gigahost servers ip-order --type l2|l3 SERVER",
		ShortHelp: "Order an additional IPv4 address for a server.",
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

			serverID, err := resolveServerArg(ctx, client, args[0])
			if err != nil {
				return err
			}

			if err := client.Servers.OrderIPv4(ctx, serverID, gigahost.IPType(ipType)); err != nil {
				return err
			}

			fmt.Fprintf(c.Out, "Ordered additional %s IPv4 for server %s\n", ipType, serverID)

			return nil
		},
	}
}

func newServersGraphsCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("graphs").SetParent(parent)

	var (
		kind   string
		window string
		out    string
	)

	fs.StringEnumVar(&kind, 0, "kind", "graph kind", "bandwidth", "packets")
	fs.StringEnumVar(&window, 'w', "window", "time window", "day", "week", "month", "year")
	fs.StringVar(&out, 'O', "output-file", "", "file to write the PNG to (required)")

	return &ff.Command{
		Name:      "graphs",
		Usage:     "gigahost servers graphs --kind bandwidth|packets --window day|week|month|year --output-file FILE SERVER",
		ShortHelp: "Fetch a bandwidth or packet graph as a PNG image.",
		Flags:     fs,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return errors.New("exactly one SERVER argument is required")
			}

			if out == "" {
				return errors.New("--output-file is required")
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

			var graphs *gigahost.PortGraphs
			if kind == "bandwidth" {
				graphs, err = client.Servers.GetBandwidthGraphs(ctx, serverID)
			} else {
				graphs, err = client.Servers.GetPacketGraphs(ctx, serverID)
			}

			if err != nil {
				return err
			}

			var encoded string

			switch window {
			case "day":
				encoded = graphs.GraphDay
			case "week":
				encoded = graphs.GraphWeek
			case "month":
				encoded = graphs.GraphMonth
			case "year":
				encoded = graphs.GraphYear
			}

			data, err := gigahost.Base64Bytes(encoded)
			if err != nil {
				return err
			}

			if err := os.WriteFile(out, data, 0o600); err != nil {
				return fmt.Errorf("write %s: %w", out, err)
			}

			fmt.Fprintf(c.Out, "Wrote %d bytes to %s\n", len(data), out)

			return nil
		},
	}
}

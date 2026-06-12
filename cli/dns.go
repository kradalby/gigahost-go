package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	gigahost "github.com/kradalby/gigahost-go/client"
	"github.com/peterbourgon/ff/v4"
)

// resolveZoneArg resolves a zone reference (numeric ID or zone name) to its ID.
func resolveZoneArg(ctx context.Context, cl *gigahost.Client, ref string) (string, error) {
	z, err := cl.DNS.ResolveZone(ctx, ref)
	if err != nil {
		return "", err
	}

	return z.ID, nil
}

func newDNSCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("dns").SetParent(parent)

	cmd := &ff.Command{
		Name:      "dns",
		Usage:     "gigahost dns COMMAND",
		ShortHelp: "Manage DNS zones, records, redirects and DynDNS.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		newDNSZonesCmd(c, fs, load),
		newDNSRecordsCmd(c, fs, load),
		newDNSRedirectsCmd(c, fs, load),
		newDNSDNSSECCmd(c, fs, load),
		newDNSDomainCmd(c, fs, load),
		newDNSRegistrantCmd(c, fs, load),
		newDNSNameserversCmd(c, fs, load),
		newDNSPTRCmd(c, fs, load),
		newDNSDynDNSCmd(c, fs, load),
	}

	return cmd
}

func newDNSZonesCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("zones").SetParent(parent)

	cmd := &ff.Command{
		Name:      "zones",
		Usage:     "gigahost dns zones COMMAND",
		ShortHelp: "Manage DNS zones.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		{
			Name:  "list",
			Usage: "gigahost dns zones list",
			Flags: ff.NewFlagSet("list").SetParent(fs),
			Exec: func(ctx context.Context, _ []string) error {
				if err := load(); err != nil {
					return err
				}

				client, err := c.Client()
				if err != nil {
					return err
				}

				zones, err := client.DNS.ListZones(ctx)
				if err != nil {
					return err
				}

				view := make([]dnsZoneRow, 0, len(zones))
				for _, z := range zones {
					view = append(view, dnsZoneRow{
						ID:         z.ID,
						Name:       z.Name,
						Type:       string(z.Type),
						Active:     z.Active,
						Records:    z.RecordCount,
						Registered: z.IsRegistered,
						Updated:    shortTime(z.UpdatedAt),
					})
				}

				return c.Render(view)
			},
		},
		newDNSZoneCreateCmd(c, fs, load),
		{
			Name:  "delete",
			Usage: "gigahost dns zones delete ZONE",
			Flags: ff.NewFlagSet("delete").SetParent(fs),
			Exec: func(ctx context.Context, args []string) error {
				if len(args) != 1 {
					return errors.New("exactly one ZONE argument is required")
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

				if err := client.DNS.DeleteZone(ctx, zid); err != nil {
					return err
				}

				fmt.Fprintf(c.Out, "Deleted zone %s\n", args[0])

				return nil
			},
		},
	}

	return cmd
}

func newDNSZoneCreateCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("create").SetParent(parent)

	var (
		zoneType   string
		defaults   bool
		useExistNS bool
	)

	fs.StringVar(&zoneType, 0, "type", "NATIVE", "zone type (NATIVE|MASTER|SLAVE)")
	fs.BoolVar(&defaults, 0, "defaults", "create default records")
	fs.BoolVar(&useExistNS, 0, "use-existing-ns", "preserve existing nameservers when creating via transfer")

	return &ff.Command{
		Name:      "create",
		Usage:     "gigahost dns zones create ZONE_NAME",
		ShortHelp: "Create a new DNS zone.",
		Flags:     fs,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return errors.New("exactly one ZONE_NAME argument is required")
			}

			if err := load(); err != nil {
				return err
			}

			client, err := c.Client()
			if err != nil {
				return err
			}

			resp, err := client.DNS.CreateZone(ctx, gigahost.CreateZoneRequest{
				Name:                 args[0],
				Type:                 gigahost.ZoneType(zoneType),
				CreateDefaultRecords: defaults,
				UseExistingNS:        useExistNS,
			})
			if err != nil {
				return err
			}

			return c.Render(dnsZoneRow{
				ID:   resp.ID,
				Name: args[0],
				Type: zoneType,
			})
		},
	}
}

// dnsRecordParams holds the shared flag state for dns records subcommands.
type dnsRecordParams struct {
	zoneID   string
	recName  string
	recType  string
	recValue string
	recTTL   int
	recPrio  int
}

func newDNSRecordsCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("records").SetParent(parent)

	var p dnsRecordParams

	fs.StringVar(&p.zoneID, 'z', "zone", "", "zone name or ID")
	fs.StringVar(&p.recName, 'n', "name", "", "record name")
	fs.StringVar(&p.recType, 0, "type", "A", "record type")
	fs.StringVar(&p.recValue, 'V', "value", "", "record value")
	fs.IntVar(&p.recTTL, 0, "ttl", 3600, "record TTL")
	fs.IntVar(&p.recPrio, 0, "priority", 0, "record priority (MX only)")

	cmd := &ff.Command{
		Name:      "records",
		Usage:     "gigahost dns records COMMAND",
		ShortHelp: "Manage DNS records.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		{
			Name:  "list",
			Usage: "gigahost dns records list --zone ZONE",
			Flags: ff.NewFlagSet("list").SetParent(fs),
			Exec: func(ctx context.Context, _ []string) error {
				return dnsRecordsList(ctx, c, load, &p)
			},
		},
		{
			Name:  "create",
			Usage: "gigahost dns records create --zone ZONE --name NAME --type TYPE --value VAL",
			Flags: ff.NewFlagSet("create").SetParent(fs),
			Exec: func(ctx context.Context, _ []string) error {
				return dnsRecordsCreate(ctx, c, load, &p)
			},
		},
		{
			Name:  "update",
			Usage: "gigahost dns records update --zone ZONE [--name NAME] [--type TYPE] [--value VAL] [--ttl SEC] [--priority N] RECORD_ID",
			Flags: ff.NewFlagSet("update").SetParent(fs),
			Exec: func(ctx context.Context, args []string) error {
				return dnsRecordsUpdate(ctx, c, load, args, &p)
			},
		},
		{
			Name:  "delete",
			Usage: "gigahost dns records delete --zone ZONE --name NAME --type TYPE RECORD_ID",
			Flags: ff.NewFlagSet("delete").SetParent(fs),
			Exec: func(ctx context.Context, args []string) error {
				return dnsRecordsDelete(ctx, c, load, args, &p)
			},
		},
	}

	return cmd
}

func dnsRecordsList(ctx context.Context, c *Context, load func() error, p *dnsRecordParams) error {
	if p.zoneID == "" {
		return errors.New("--zone is required")
	}

	if err := load(); err != nil {
		return err
	}

	client, err := c.Client()
	if err != nil {
		return err
	}

	zid, err := resolveZoneArg(ctx, client, p.zoneID)
	if err != nil {
		return err
	}

	records, err := client.DNS.ListRecords(ctx, zid)
	if err != nil {
		return err
	}

	view := make([]dnsRecordRow, 0, len(records))
	for _, r := range records {
		row := dnsRecordRow{
			ID:    r.ID,
			Name:  r.Name,
			Type:  string(r.Type),
			Value: r.Value,
			TTL:   r.TTL,
		}

		if r.Priority != nil {
			row.Priority = strconv.Itoa(*r.Priority)
		}

		view = append(view, row)
	}

	return c.Render(view)
}

func dnsRecordsCreate(ctx context.Context, c *Context, load func() error, p *dnsRecordParams) error {
	if p.zoneID == "" || p.recValue == "" {
		return errors.New("--zone and --value are required")
	}

	if err := load(); err != nil {
		return err
	}

	client, err := c.Client()
	if err != nil {
		return err
	}

	zid, err := resolveZoneArg(ctx, client, p.zoneID)
	if err != nil {
		return err
	}

	req := gigahost.CreateRecordRequest{
		Name:  p.recName,
		Type:  gigahost.RecordType(p.recType),
		Value: p.recValue,
		TTL:   p.recTTL,
	}

	if p.recPrio != 0 {
		prio := p.recPrio
		req.Priority = &prio
	}

	if err := client.DNS.CreateRecord(ctx, zid, req); err != nil {
		return err
	}

	// CreateRecord returns nothing; list and match to get the ID.
	records, err := client.DNS.ListRecords(ctx, zid)
	if err != nil {
		return err
	}

	row, found := matchRecord(records, p.recName, p.recType, p.recValue)
	if !found {
		if c.format == outputTable {
			fmt.Fprintln(c.Out, "Record created.")
		}

		return nil
	}

	return c.Render(row)
}

func dnsRecordsUpdate(ctx context.Context, c *Context, load func() error, args []string, p *dnsRecordParams) error {
	if len(args) != 1 {
		return errors.New("exactly one RECORD_ID argument is required")
	}

	if p.zoneID == "" {
		return errors.New("--zone is required")
	}

	if err := load(); err != nil {
		return err
	}

	client, err := c.Client()
	if err != nil {
		return err
	}

	zid, err := resolveZoneArg(ctx, client, p.zoneID)
	if err != nil {
		return err
	}

	req := gigahost.UpdateRecordRequest{
		Name:  p.recName,
		Type:  gigahost.RecordType(p.recType),
		Value: p.recValue,
		TTL:   p.recTTL,
	}

	if p.recPrio != 0 {
		prio := p.recPrio
		req.Priority = &prio
	}

	if err := client.DNS.UpdateRecord(ctx, zid, args[0], req); err != nil {
		return err
	}

	fmt.Fprintln(c.Out, "Record updated.")

	return nil
}

func dnsRecordsDelete(ctx context.Context, c *Context, load func() error, args []string, p *dnsRecordParams) error {
	if len(args) != 1 {
		return errors.New("exactly one RECORD_ID argument is required")
	}

	if p.zoneID == "" || p.recName == "" || p.recType == "" {
		return errors.New("--zone, --name and --type are required")
	}

	if err := load(); err != nil {
		return err
	}

	client, err := c.Client()
	if err != nil {
		return err
	}

	zid, err := resolveZoneArg(ctx, client, p.zoneID)
	if err != nil {
		return err
	}

	if err := client.DNS.DeleteRecord(ctx, zid, args[0], p.recName, gigahost.RecordType(p.recType)); err != nil {
		return err
	}

	fmt.Fprintln(c.Out, "Record deleted.")

	return nil
}

func newDNSDynDNSCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("dyndns").SetParent(parent)

	var (
		hosts []string
		v4    string
		v6    string
	)

	fs.StringListVar(&hosts, 'H', "hostname", "hostname to update (repeatable)")
	fs.StringVar(&v4, 0, "ip", "", "IPv4 address (empty = detect from source IP)")
	fs.StringVar(&v6, 0, "ipv6", "", "IPv6 address")

	return &ff.Command{
		Name:      "dyndns",
		Usage:     "gigahost dns dyndns --hostname HOST [--ip IP] [--ipv6 IPV6]",
		ShortHelp: "Update dynamic DNS record(s).",
		Flags:     fs,
		Exec: func(ctx context.Context, _ []string) error {
			if len(hosts) == 0 {
				return errors.New("at least one --hostname is required")
			}

			if err := load(); err != nil {
				return err
			}

			client, err := c.Client()
			if err != nil {
				return err
			}

			results, err := client.DynDNS.Update(ctx, gigahost.UpdateRequest{
				Username:  c.Config.Username,
				Password:  c.Config.Password,
				Hostnames: hosts,
				IPv4:      v4,
				IPv6:      v6,
			})
			if err != nil {
				return err
			}

			return c.Render(results)
		},
	}
}

type dnsZoneRow struct {
	ID         string `cli:"ID"`
	Name       string `cli:"Name"`
	Type       string `cli:"Type"`
	Active     bool   `cli:"Active"`
	Records    int    `cli:"Records"`
	Registered bool   `cli:"Registered"`
	Updated    string `cli:"Updated"`
}

type dnsRecordRow struct {
	ID       string `cli:"ID"`
	Name     string `cli:"Name"`
	Type     string `cli:"Type"`
	Value    string `cli:"Value"`
	TTL      int    `cli:"TTL"`
	Priority string `cli:"Prio"`
}

// matchRecord finds the first record in list whose name, type and value
// match those just submitted. Returns false when nothing matches.
func matchRecord(records []gigahost.DNSRecord, name, rtype, value string) (dnsRecordRow, bool) {
	for _, r := range records {
		if r.Name == name && string(r.Type) == rtype && r.Value == value {
			row := dnsRecordRow{
				ID:    r.ID,
				Name:  r.Name,
				Type:  string(r.Type),
				Value: r.Value,
				TTL:   r.TTL,
			}

			if r.Priority != nil {
				row.Priority = strconv.Itoa(*r.Priority)
			}

			return row, true
		}
	}

	return dnsRecordRow{}, false
}

func shortTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}

	return t.UTC().Format("2006-01-02 15:04 UTC")
}

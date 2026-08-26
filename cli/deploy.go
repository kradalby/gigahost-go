package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/peterbourgon/ff/v4"

	gigahost "github.com/kradalby/gigahost-go/client"
)

func newDeployCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("deploy").SetParent(parent)

	cmd := &ff.Command{
		Name:      "deploy",
		Usage:     "gigahost deploy COMMAND",
		ShortHelp: "Deploy and monitor hourly-billed cloud servers.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		newDeployCatalogCmd(c, fs, load),
		newDeployTypesCmd(c, fs, load),
		newDeploySizesCmd(c, fs, load),
		newDeployRegionsCmd(c, fs, load),
		newDeployOSCmd(c, fs, load),
		newDeployCreateCmd(c, fs, load),
		newDeployStatusCmd(c, fs, load),
		newDeployISOsCmd(c, fs, load),
	}

	return cmd
}

// sizeRow is the shared table row for catalog/sizes listings: the slug
// columns are exactly what `deploy create` and `gigahost_server` accept.
type sizeRow struct {
	Platform    string  `cli:"Platform"`
	Type        string  `cli:"Type"`
	Size        string  `cli:"Size"`
	Cores       int     `cli:"Cores"`
	MemoryGB    int     `cli:"Memory (GB)"`
	StorageGB   int     `cli:"Storage (GB)"`
	RateHourly  float64 `cli:"Hourly"`
	RateMonthly float64 `cli:"Monthly"`
	Currency    string  `cli:"Currency"`
}

// catalogSizeRows flattens the deployable products of a catalog into rows,
// optionally filtered by platform/type slugs.
func catalogSizeRows(cat *gigahost.DeployCatalog, platformFilter, typeFilter string) []sizeRow {
	var rows []sizeRow

	for i := range cat.Tiers {
		tier := &cat.Tiers[i]
		typeSlug := tier.TypeSlug()

		if typeFilter != "" && typeSlug != typeFilter {
			continue
		}

		for j := range tier.Products {
			p := &tier.Products[j]
			if !p.Deployable() {
				continue
			}

			if platformFilter != "" && p.PlatformSlug() != platformFilter {
				continue
			}

			storage := 0
			for _, d := range p.Specs.Disks {
				storage += d.SizeGB
			}

			rows = append(rows, sizeRow{
				Platform:    p.PlatformSlug(),
				Type:        typeSlug,
				Size:        p.SizeSlug(),
				Cores:       p.Specs.CPUCores,
				MemoryGB:    p.Specs.RAMGB,
				StorageGB:   storage,
				RateHourly:  p.RateHourly,
				RateMonthly: p.RateMonthly,
				Currency:    cat.Currency,
			})
		}
	}

	return rows
}

// regionRow is the table row for region listings.
type regionRow struct {
	Slug    string `cli:"Slug"`
	Name    string `cli:"Name"`
	Short   string `cli:"Short"`
	Country string `cli:"Country"`
	Active  bool   `cli:"Active"`
}

func regionRows(cat *gigahost.DeployCatalog) []regionRow {
	rows := make([]regionRow, 0, len(cat.Regions))

	for i := range cat.Regions {
		r := &cat.Regions[i]
		rows = append(rows, regionRow{
			Slug:    r.Slug(),
			Name:    r.Name,
			Short:   r.NameShort,
			Country: r.Country,
			Active:  r.Active,
		})
	}

	return rows
}

func newDeployCatalogCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("catalog").SetParent(parent)

	return &ff.Command{
		Name:      "catalog",
		Usage:     "gigahost deploy catalog",
		ShortHelp: "Full deploy overview: sizes and regions.",
		Flags:     fs,
		Exec: func(ctx context.Context, _ []string) error {
			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			cat, err := cl.Deploy.GetCatalog(ctx)
			if err != nil {
				return err
			}

			sizes := catalogSizeRows(cat, "", "")
			regions := regionRows(cat)

			// Structured output gets one document; table mode two tables.
			if c.format != outputTable {
				return c.Render(struct {
					Sizes   []sizeRow   `json:"sizes"`
					Regions []regionRow `json:"regions"`
				}{Sizes: sizes, Regions: regions})
			}

			if err := c.Render(sizes); err != nil {
				return err
			}

			fmt.Fprintln(c.Out)

			return c.Render(regions)
		},
	}
}

func newDeployTypesCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("types").SetParent(parent)

	return &ff.Command{
		Name:      "types",
		Usage:     "gigahost deploy types",
		ShortHelp: "List server types (for --type and gigahost_server.type).",
		Flags:     fs,
		Exec: func(ctx context.Context, _ []string) error {
			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			cat, err := cl.Deploy.GetCatalog(ctx)
			if err != nil {
				return err
			}

			type typeRow struct {
				Type     string `cli:"Type"`
				Platform string `cli:"Platform"`
				Sizes    int    `cli:"Sizes"`
			}

			var rows []typeRow

			for i := range cat.Tiers {
				tier := &cat.Tiers[i]

				count := 0
				platform := ""

				for j := range tier.Products {
					p := &tier.Products[j]
					if !p.Deployable() {
						continue
					}

					count++
					platform = p.PlatformSlug()
				}

				if count == 0 {
					continue
				}

				rows = append(rows, typeRow{
					Type:     tier.TypeSlug(),
					Platform: platform,
					Sizes:    count,
				})
			}

			return c.Render(rows)
		},
	}
}

func newDeploySizesCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("sizes").SetParent(parent)

	var (
		typeFilter     string
		platformFilter string
	)

	fs.StringVar(&typeFilter, 't', "type", "", "Only sizes of this type, e.g. value")
	fs.StringVar(&platformFilter, 0, "platform", "", "Only sizes on this platform (cloud or metal)")

	return &ff.Command{
		Name:      "sizes",
		Usage:     "gigahost deploy sizes [--type TYPE] [--platform PLATFORM]",
		ShortHelp: "List server sizes (for --size and gigahost_server.size).",
		Flags:     fs,
		Exec: func(ctx context.Context, _ []string) error {
			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			cat, err := cl.Deploy.GetCatalog(ctx)
			if err != nil {
				return err
			}

			return c.Render(catalogSizeRows(cat, strings.ToLower(platformFilter), strings.ToLower(typeFilter)))
		},
	}
}

func newDeployRegionsCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("regions").SetParent(parent)

	return &ff.Command{
		Name:      "regions",
		Usage:     "gigahost deploy regions",
		ShortHelp: "List regions (for --region and gigahost_server.region).",
		Flags:     fs,
		Exec: func(ctx context.Context, _ []string) error {
			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			cat, err := cl.Deploy.GetCatalog(ctx)
			if err != nil {
				return err
			}

			return c.Render(regionRows(cat))
		},
	}
}

func newDeployOSCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("os").SetParent(parent)

	return &ff.Command{
		Name:      "os",
		Usage:     "gigahost deploy os [DISTRIBUTION]",
		ShortHelp: "List operating systems (for --os and gigahost_server.os).",
		Flags:     fs,
		Exec: func(ctx context.Context, args []string) error {
			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			all, err := cl.Reinstall.ListAllOperatingSystems(ctx)
			if err != nil {
				return err
			}

			filter := ""
			if len(args) > 0 {
				filter = strings.ToLower(args[0])
			}

			type osRow struct {
				Slug         string `cli:"Slug"`
				Distribution string `cli:"Distribution"`
				Codename     string `cli:"Codename"`
				Name         string `cli:"Name"`
				Arch         string `cli:"Arch"`
				MinRAMGB     int    `cli:"Min RAM (GB)"`
			}

			var rows []osRow

			for _, o := range all {
				if filter != "" &&
					!strings.Contains(strings.ToLower(o.Distribution.Value), filter) &&
					!strings.Contains(strings.ToLower(o.Distribution.Name), filter) {
					continue
				}

				rows = append(rows, osRow{
					Slug:         o.Slug,
					Distribution: strings.ToLower(o.Distribution.Value),
					Codename:     o.OS.Distribution,
					Name:         o.OS.Name,
					Arch:         o.OS.Arch,
					MinRAMGB:     o.OS.MinRAM,
				})
			}

			return c.Render(rows)
		},
	}
}

func newDeployCreateCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("create").SetParent(parent)

	var (
		platform string
		typeSlug string
		sizeSlug string
		region   string
		osSlug   string
		isoName  string
		rescue   bool
		quantity int
		backup   bool
		wait     bool
	)

	var (
		hostnamesCSV string // comma-separated hostnames
		sshKeysCSV   string // comma-separated key names or IDs
	)

	fs.StringVar(&typeSlug, 't', "type", "", "Server type, e.g. value (see `deploy types`)")
	fs.StringVar(&sizeSlug, 's', "size", "", "Server size, e.g. 2c-4gb-40gb (see `deploy sizes`)")
	fs.StringVar(&platform, 0, "platform", "", "Platform (default cloud)")
	fs.StringVar(&region, 'r', "region", "", "Region, e.g. sfj (optional with a single region; see `deploy regions`)")
	fs.StringVar(&osSlug, 0, "os", "", "Operating system, e.g. debian-12 (one of --os/--iso/--rescue; see `deploy os`)")
	fs.StringVar(&isoName, 0, "iso", "", "Uploaded ISO name to boot (one of --os/--iso/--rescue; see `deploy isos`)")
	fs.BoolVar(&rescue, 0, "rescue", "Boot into rescue mode")
	fs.IntVar(&quantity, 'q', "quantity", 1, "Number of servers to deploy")
	fs.BoolVar(&backup, 0, "backup", "Enable backups (+25% cost)")
	fs.BoolVar(&wait, 'w', "wait", "Poll /deploy/status until all servers are ready")
	fs.StringVar(&hostnamesCSV, 'n', "hostnames", "", "Comma-separated hostnames for the new servers")
	fs.StringVar(&sshKeysCSV, 'k', "ssh-keys", "", "Comma-separated SSH key names or IDs to inject")

	return &ff.Command{
		Name:      "create",
		Usage:     "gigahost deploy create --type TYPE --size SIZE (--os OS|--iso ISO|--rescue) [flags]",
		ShortHelp: "Deploy one or more hourly-billed servers.",
		Flags:     fs,
		Exec: func(ctx context.Context, _ []string) error {
			var hostnames, sshKeyRefs []string

			if hostnamesCSV != "" {
				hostnames = strings.Split(hostnamesCSV, ",")
			}

			if sshKeysCSV != "" {
				sshKeyRefs = strings.Split(sshKeysCSV, ",")
			}

			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			cat, err := cl.Deploy.GetCatalog(ctx)
			if err != nil {
				return err
			}

			product, err := cat.FindProduct(gigahost.ProductSelector{
				Platform: platform,
				Type:     typeSlug,
				Size:     sizeSlug,
			})
			if err != nil {
				return err
			}

			resolvedRegion, err := cat.RegionForProduct(product, region)
			if err != nil {
				return err
			}

			req := gigahost.DeployServerRequest{
				ProductID: product.ID,
				PriceID:   product.PriceID,
				RegionID:  resolvedRegion.ID,
				Rescue:    rescue,
				Quantity:  quantity,
				Backups:   backup,
				Hostnames: hostnames,
			}

			if osSlug != "" {
				resolved, rerr := cl.Reinstall.ResolveOS(ctx, osSlug)
				if rerr != nil {
					return rerr
				}

				req.OSID = resolved.OS.ID

				osSlug = resolved.Slug
			}

			if isoName != "" {
				resolved, rerr := cl.Deploy.ResolveISO(ctx, isoName)
				if rerr != nil {
					return rerr
				}

				req.ISOID = resolved.ID
			}

			if len(sshKeyRefs) > 0 {
				req.SSHKeys, err = cl.Account.ResolveSSHKeys(ctx, sshKeyRefs)
				if err != nil {
					return err
				}
			}

			boot := osSlug
			if isoName != "" {
				boot = "iso " + isoName
			} else if rescue {
				boot = "rescue"
			}

			if c.format == outputTable {
				fmt.Fprintf(c.Out, "Deploying %s (%s, %g %s/h) in %s with %s...\n",
					product.SizeSlug(), product.Name, product.RateHourly, cat.Currency,
					resolvedRegion.Slug(), boot)
			}

			resp, err := cl.Deploy.Deploy(ctx, req)
			if err != nil {
				return err
			}

			if !wait || len(resp.OrderIDs) == 0 {
				type orderRow struct {
					OrderID     string `cli:"Order ID"`
					OrderNumber string `cli:"Order Number"`
					RateHourly  string `cli:"Rate/hr"`
					MonthlyCap  string `cli:"Monthly Cap"`
					Currency    string `cli:"Currency"`
				}

				rows := make([]orderRow, 0, len(resp.OrderIDs))
				for i, id := range resp.OrderIDs {
					num := ""
					if i < len(resp.OrderNumbers) {
						num = resp.OrderNumbers[i]
					}

					rows = append(rows, orderRow{
						OrderID:     id,
						OrderNumber: num,
						RateHourly:  resp.RateHourly,
						MonthlyCap:  resp.MonthlyCap,
						Currency:    resp.Currency,
					})
				}

				return c.Render(rows)
			}

			if c.format == outputTable {
				fmt.Fprintln(c.Out, "Waiting for servers to become ready...")
			}

			return pollDeployStatus(ctx, cl, c, resp.OrderIDs)
		},
	}
}

// pollDeployStatus polls /deploy/status until all servers are ready, printing
// progress. It returns an error if the context is cancelled before completion.
func pollDeployStatus(ctx context.Context, cl *gigahost.Client, c *Context, orderIDs []string) error {
	const pollInterval = 5 * time.Second

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}

		status, err := cl.Deploy.GetStatus(ctx, orderIDs)
		if err != nil {
			return fmt.Errorf("poll status: %w", err)
		}

		if c.format == outputTable {
			for _, s := range status.Servers {
				fmt.Fprintf(c.Out, "  %s  %-12s  %s\n", s.OrderID, s.Status, s.Hostname)
			}
		}

		if status.AllReady {
			type readyRow struct {
				ServerID string `cli:"Server ID"`
				Hostname string `cli:"Hostname"`
				IP       string `cli:"IP"`
				Password string `cli:"Password"`
				OrderID  string `cli:"Order ID"`
			}

			rows := make([]readyRow, 0, len(status.Servers))
			for _, s := range status.Servers {
				rows = append(rows, readyRow{
					ServerID: s.ServerID,
					Hostname: s.Hostname,
					IP:       s.IP,
					Password: s.Password,
					OrderID:  s.OrderID,
				})
			}

			return c.Render(rows)
		}
	}
}

func newDeployStatusCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("status").SetParent(parent)

	return &ff.Command{
		Name:      "status",
		Usage:     "gigahost deploy status ORDER_ID...",
		ShortHelp: "Check deployment status for one or more order IDs.",
		Flags:     fs,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				return errors.New("at least one ORDER_ID argument is required")
			}

			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			status, err := cl.Deploy.GetStatus(ctx, args)
			if err != nil {
				return err
			}

			type statusRow struct {
				OrderID  string `cli:"Order ID"`
				Hostname string `cli:"Hostname"`
				ServerID string `cli:"Server ID"`
				IP       string `cli:"IP"`
				Status   string `cli:"Status"`
			}

			var rows []statusRow
			for _, s := range status.Servers {
				rows = append(rows, statusRow{
					OrderID:  s.OrderID,
					Hostname: s.Hostname,
					ServerID: s.ServerID,
					IP:       s.IP,
					Status:   string(s.Status),
				})
			}

			return c.Render(rows)
		},
	}
}

func newDeployISOsCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("isos").SetParent(parent)

	return &ff.Command{
		Name:      "isos",
		Usage:     "gigahost deploy isos",
		ShortHelp: "List ISOs available for deployment (for --iso and gigahost_server.iso).",
		Flags:     fs,
		Exec: func(ctx context.Context, _ []string) error {
			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			isos, err := cl.Deploy.ListISOs(ctx)
			if err != nil {
				return err
			}

			type isoRow struct {
				Name string `cli:"Name"`
				ID   string `cli:"ID"`
				Size int64  `cli:"Size (bytes)"`
			}

			var rows []isoRow
			for _, iso := range isos {
				rows = append(rows, isoRow{
					Name: iso.Name,
					ID:   iso.ID,
					Size: iso.Size,
				})
			}

			return c.Render(rows)
		},
	}
}

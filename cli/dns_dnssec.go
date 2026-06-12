package cli

import (
	"context"
	"errors"
	"fmt"

	gigahost "github.com/kradalby/gigahost-go/client"
	"github.com/peterbourgon/ff/v4"
)

func newDNSDNSSECCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("dnssec").SetParent(parent)

	cmd := &ff.Command{
		Name:      "dnssec",
		Usage:     "gigahost dns dnssec COMMAND ZONE",
		ShortHelp: "Manage DNSSEC for a registered domain.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		{
			Name:  "enable",
			Usage: "gigahost dns dnssec enable ZONE",
			Flags: ff.NewFlagSet("enable").SetParent(fs),
			Exec: func(ctx context.Context, args []string) error {
				return dnssecToggle(ctx, c, load, args, true)
			},
		},
		{
			Name:  "disable",
			Usage: "gigahost dns dnssec disable ZONE",
			Flags: ff.NewFlagSet("disable").SetParent(fs),
			Exec: func(ctx context.Context, args []string) error {
				return dnssecToggle(ctx, c, load, args, false)
			},
		},
		{
			Name:  "ds",
			Usage: "gigahost dns dnssec ds ZONE",
			Flags: ff.NewFlagSet("ds").SetParent(fs),
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

				ds, err := client.DNS.GetDSRecords(ctx, zid)
				if err != nil {
					return err
				}

				return c.Render(ds)
			},
		},
		{
			Name:  "external-ds",
			Usage: "gigahost dns dnssec external-ds ZONE",
			Flags: ff.NewFlagSet("external-ds").SetParent(fs),
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

				ds, err := client.DNS.GetExternalDSRecords(ctx, zid)
				if err != nil {
					return err
				}

				return c.Render(ds.DSRecords)
			},
		},
		newDNSDNSSECSubmitExternalCmd(c, fs, load),
	}

	return cmd
}

func newDNSDNSSECSubmitExternalCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("submit-external").SetParent(parent)

	var (
		keyTag     int
		algorithm  int
		digestType int
		digest     string
	)

	fs.IntVar(&keyTag, 0, "key-tag", 0, "key tag (0..65535)")
	fs.IntVar(&algorithm, 0, "algorithm", 0, "DNSSEC algorithm (5,7,8,10,13,14,15,16)")
	fs.IntVar(&digestType, 0, "digest-type", 0, "digest type (1=SHA-1, 2=SHA-256, 4=SHA-384)")
	fs.StringVar(&digest, 0, "digest", "", "hexadecimal digest")

	return &ff.Command{
		Name:  "submit-external",
		Usage: "gigahost dns dnssec submit-external --key-tag N --algorithm N --digest-type N --digest HEX ZONE",
		ShortHelp: "Submit an external DS record to Norid for a DNSSEC-enabled domain " +
			"using third-party nameservers.",
		Flags: fs,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return errors.New("exactly one ZONE argument is required")
			}

			if digest == "" {
				return errors.New("--digest is required")
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

			ds := []gigahost.DSRecord{{
				KeyTag:     keyTag,
				Algorithm:  algorithm,
				DigestType: digestType,
				Digest:     digest,
			}}

			if err := client.DNS.SubmitExternalDSRecords(ctx, zid, ds); err != nil {
				return err
			}

			fmt.Fprintln(c.Out, "External DS record submitted to Norid.")

			return nil
		},
	}
}

func dnssecToggle(ctx context.Context, c *Context, load func() error, args []string, enable bool) error {
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

	if err := client.DNS.SetDNSSEC(ctx, zid, enable); err != nil {
		return err
	}

	if !enable {
		if c.format == outputTable {
			fmt.Fprintf(c.Out, "DNSSEC disabled on zone %s\n", args[0])
		}

		return nil
	}

	// On enable, fetch and render the DS records — that's the actionable output.
	// A lookup failure is non-fatal: DNSSEC was still enabled.
	if ds, lookupErr := client.DNS.GetDSRecords(ctx, zid); lookupErr == nil {
		return c.Render(ds)
	}

	if c.format == outputTable {
		fmt.Fprintf(c.Out, "DNSSEC enabled on zone %s\n", args[0])
	}

	return nil
}

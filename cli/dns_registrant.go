package cli

import (
	"context"
	"errors"
	"fmt"

	gigahost "github.com/kradalby/gigahost-go/client"
	"github.com/peterbourgon/ff/v4"
)

func newDNSRegistrantCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("registrant").SetParent(parent)

	cmd := &ff.Command{
		Name:      "registrant",
		Usage:     "gigahost dns registrant COMMAND ZONE",
		ShortHelp: "Manage domain registrant information.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		{
			Name:  "show",
			Usage: "gigahost dns registrant show ZONE",
			Flags: ff.NewFlagSet("show").SetParent(fs),
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

				reg, err := client.DNS.GetRegistrant(ctx, zid)
				if err != nil {
					return err
				}

				return c.Render(reg)
			},
		},
		newDNSRegistrantUpdateCmd(c, fs, load),
		newDNSRegistrantEmailCmd(c, fs, load),
		newDNSAutoRenewCmd(c, fs, load),
	}

	return cmd
}

func newDNSRegistrantUpdateCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("update").SetParent(parent)

	var (
		registrantType string
		email          string
		applicantName  string
		zip            string
		city           string
		orgNumber      string
		companyName    string
		pid            string
		agree          bool
	)

	fs.StringEnumVar(&registrantType, 0, "registrant-type", "registrant kind", "organization", "person")
	fs.StringVar(&email, 'e', "email", "", "contact email address")
	fs.StringVar(&applicantName, 0, "applicant-name", "", "applicant name")
	fs.StringVar(&zip, 0, "zip", "", "postal code")
	fs.StringVar(&city, 0, "city", "", "city")
	fs.StringVar(&orgNumber, 0, "org-number", "", "Norwegian organisation number (organisation only)")
	fs.StringVar(&companyName, 0, "company-name", "", "company name (organisation only)")
	fs.StringVar(&pid, 0, "pid", "", "personal identifier N.PRI.XXXXXXXX (person only)")
	fs.BoolVar(&agree, 0, "agree-to-terms", "accept the Norid Applicant Declaration")

	return &ff.Command{
		Name:      "update",
		Usage:     "gigahost dns registrant update --agree-to-terms [flags] ZONE",
		ShortHelp: "Change the registrant of a registered .no domain.",
		Flags:     fs,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return errors.New("exactly one ZONE argument is required")
			}

			if !agree {
				return errors.New("--agree-to-terms is required (Norid Applicant Declaration)")
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

			return client.DNS.UpdateRegistrant(ctx, zid, gigahost.UpdateRegistrantRequest{
				RegistrantType: gigahost.RegistrantType(registrantType),
				Email:          email,
				ApplicantName:  applicantName,
				ZipCode:        zip,
				City:           city,
				AgreeToTerms:   agree,
				OrgNumber:      orgNumber,
				CompanyName:    companyName,
				PID:            pid,
			})
		},
	}
}

func newDNSRegistrantEmailCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("set-email").SetParent(parent)

	var (
		email   string
		protect bool
	)

	fs.StringVar(&email, 'e', "email", "", "new registrant email address")
	fs.BoolVar(&protect, 0, "protect", "enable WHOIS email protection (forwarding alias)")

	return &ff.Command{
		Name:      "set-email",
		Usage:     "gigahost dns registrant set-email --email NEW --protect ZONE",
		ShortHelp: "Update the registrant contact email.",
		Flags:     fs,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return errors.New("exactly one ZONE argument is required")
			}

			if email == "" {
				return errors.New("--email is required")
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

			resp, err := client.DNS.SetRegistrantEmail(ctx, zid, gigahost.SetRegistrantEmailRequest{
				Email:            email,
				EnableProtection: protect,
			})
			if err != nil {
				return err
			}

			fmt.Fprintf(c.Out, "Registrant email updated (%s, protection=%v)\n", resp.Email, resp.Protected)

			return nil
		},
	}
}

func newDNSAutoRenewCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("auto-renew").SetParent(parent)

	var enable bool

	fs.BoolVar(&enable, 0, "enable", "enable automatic renewal (default: disable)")

	return &ff.Command{
		Name:      "auto-renew",
		Usage:     "gigahost dns registrant auto-renew [--enable] ZONE",
		ShortHelp: "Toggle automatic domain renewal.",
		Flags:     fs,
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

			if err := client.DNS.SetAutoRenew(ctx, zid, enable); err != nil {
				return err
			}

			if enable {
				fmt.Fprintf(c.Out, "Auto-renew enabled on zone %s\n", args[0])
			} else {
				fmt.Fprintf(c.Out, "Auto-renew disabled on zone %s\n", args[0])
			}

			return nil
		},
	}
}

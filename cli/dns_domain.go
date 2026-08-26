package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/peterbourgon/ff/v4"

	gigahost "github.com/kradalby/gigahost-go/client"
)

func newDNSDomainCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("domain").SetParent(parent)

	cmd := &ff.Command{
		Name:      "domain",
		Usage:     "gigahost dns domain COMMAND",
		ShortHelp: "Look up, check and register Norwegian (.no) domains.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		{
			Name:      "check",
			Usage:     "gigahost dns domain check DOMAIN",
			ShortHelp: "Check whether a .no domain is available for registration.",
			Flags:     ff.NewFlagSet("check").SetParent(fs),
			Exec: func(ctx context.Context, args []string) error {
				if len(args) != 1 {
					return errors.New("exactly one DOMAIN argument is required")
				}

				if err := load(); err != nil {
					return err
				}

				client, err := c.Client()
				if err != nil {
					return err
				}

				check, err := client.DNS.CheckDomain(ctx, args[0])
				if err != nil {
					return err
				}

				return c.Render(check)
			},
		},
		{
			Name:      "lookup-org",
			Usage:     "gigahost dns domain lookup-org ORG_NUMBER",
			ShortHelp: "Look up a Norwegian organisation in Brønnøysundregistrene.",
			Flags:     ff.NewFlagSet("lookup-org").SetParent(fs),
			Exec: func(ctx context.Context, args []string) error {
				if len(args) != 1 {
					return errors.New("exactly one ORG_NUMBER argument is required")
				}

				if err := load(); err != nil {
					return err
				}

				client, err := c.Client()
				if err != nil {
					return err
				}

				org, err := client.DNS.LookupOrganization(ctx, args[0])
				if err != nil {
					return err
				}

				return c.Render(org)
			},
		},
		newDNSDomainRegisterCmd(c, fs, load),
	}

	return cmd
}

func newDNSDomainRegisterCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("register").SetParent(parent)

	var (
		domainName     string
		registrantType string
		email          string
		applicantName  string
		zip            string
		city           string
		orgNumber      string
		companyName    string
		pid            string
		firstName      string
		lastName       string
		useGigahostNS  bool
		nameservers    []string
	)

	fs.StringVar(&domainName, 'd', "domain", "", "domain name (ends in .no)")
	fs.StringEnumVar(&registrantType, 0, "registrant-type", "registrant kind", "organization", "person")
	fs.StringVar(&email, 'e', "email", "", "contact email address")
	fs.StringVar(&applicantName, 0, "applicant-name", "", "applicant name")
	fs.StringVar(&zip, 0, "zip", "", "postal code")
	fs.StringVar(&city, 0, "city", "", "city")
	fs.StringVar(&orgNumber, 0, "org-number", "", "Norwegian organisation number (organisation only)")
	fs.StringVar(&companyName, 0, "company-name", "", "company name (organisation only)")
	fs.StringVar(&pid, 0, "pid", "", "personal identifier N.PRI.XXXXXXXX (person only)")
	fs.StringVar(&firstName, 0, "first-name", "", "first name (person only)")
	fs.StringVar(&lastName, 0, "last-name", "", "last name (person only)")
	fs.BoolVar(&useGigahostNS, 0, "use-gigahost-ns", "use Gigahost nameservers (default)")
	fs.StringListVar(&nameservers, 0, "nameserver", "external nameserver (repeatable, min 2 when not using gigahost NS)")

	return &ff.Command{
		Name:      "register",
		Usage:     "gigahost dns domain register --domain NAME --registrant-type T [flags]",
		ShortHelp: "Register a new .no domain.",
		Flags:     fs,
		Exec: func(ctx context.Context, _ []string) error {
			if domainName == "" || registrantType == "" || email == "" {
				return errors.New("--domain, --registrant-type and --email are required")
			}

			if err := load(); err != nil {
				return err
			}

			client, err := c.Client()
			if err != nil {
				return err
			}

			req := gigahost.RegisterDomainRequest{
				DomainName:     domainName,
				RegistrantType: gigahost.RegistrantType(registrantType),
				Email:          email,
				ApplicantName:  applicantName,
				ZipCode:        zip,
				City:           city,
				OrgNumber:      orgNumber,
				CompanyName:    companyName,
				PID:            pid,
				FirstName:      firstName,
				LastName:       lastName,
				Nameservers:    nameservers,
			}

			if useGigahostNS {
				req.UseGigahostNS = &useGigahostNS
			}

			resp, err := client.DNS.RegisterDomain(ctx, req)
			if err != nil {
				return err
			}

			fmt.Fprintf(c.Out, "Registered %s (zone ID: %s, expires %s)\n", resp.DomainName, resp.ZoneID, resp.ExpiresAt)

			return nil
		},
	}
}

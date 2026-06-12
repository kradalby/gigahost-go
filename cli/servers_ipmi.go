package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/peterbourgon/ff/v4"
)

func newServersIPMICmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("ipmi").SetParent(parent)

	var acl string

	fs.StringVar(&acl, 0, "acl", "", "semicolon-separated list of source IPs and CIDRs permitted to connect")

	return &ff.Command{
		Name:      "ipmi",
		Usage:     "gigahost servers ipmi --acl 'IP;CIDR' SERVER",
		ShortHelp: "Request a short-lived KVM/IPMI session (~3h validity).",
		Flags:     fs,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return errors.New("exactly one SERVER argument is required")
			}

			if acl == "" {
				return errors.New("--acl is required")
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

			sess, err := client.IPMI.Create(ctx, serverID, acl)
			if err != nil {
				return err
			}

			fmt.Fprintf(c.Out, "KVM host:     %s\n", sess.IPAddress)
			fmt.Fprintf(c.Out, "KVM username: %s\n", sess.Username)
			fmt.Fprintf(c.Out, "KVM password: %s\n", sess.Password)

			return nil
		},
	}
}

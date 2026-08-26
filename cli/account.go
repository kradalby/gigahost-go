package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/peterbourgon/ff/v4"

	gigahost "github.com/kradalby/gigahost-go/client"
)

// resolveUserArg resolves a user reference (numeric contact ID or login
// email, case-insensitive) to its contact ID. Failed resolution lists the
// known emails.
func resolveUserArg(ctx context.Context, cl *gigahost.Client, ref string) (string, error) {
	if isNumericID(ref) {
		return ref, nil
	}

	acc, err := cl.Account.Get(ctx)
	if err != nil {
		return "", err
	}

	emails := make([]string, 0, len(acc.Contacts))

	for _, ct := range acc.Contacts {
		if strings.EqualFold(ct.Username, ref) {
			return ct.ID, nil
		}

		emails = append(emails, ct.Username)
	}

	return "", fmt.Errorf("user %q not found; known users: %s", ref, strings.Join(emails, ", "))
}

// isNumericID reports whether s is a non-empty base-10 number — the
// convention for "this is a raw ID, skip name resolution".
func isNumericID(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

func newAccountCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("account").SetParent(parent)

	cmd := &ff.Command{
		Name:      "account",
		Usage:     "gigahost account COMMAND",
		ShortHelp: "Manage account profile, users, SSH keys and API keys.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		newAccountShowCmd(c, fs, load),
		newAccountBalanceCmd(c, fs, load),
		newAccountInvoicesCmd(c, fs, load),
		newAccountUsersCmd(c, fs, load),
		newAccountSSHKeysCmd(c, fs, load),
		newAccountAPIKeysCmd(c, fs, load),
	}

	return cmd
}

// ── account show ──────────────────────────────────────────────────────────

func newAccountShowCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	return &ff.Command{
		Name:      "show",
		Usage:     "gigahost account show",
		ShortHelp: "Show account profile.",
		Flags:     ff.NewFlagSet("show").SetParent(parent),
		Exec: func(ctx context.Context, _ []string) error {
			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			acc, err := cl.Account.Get(ctx)
			if err != nil {
				return err
			}

			return c.Render(acc)
		},
	}
}

// ── account balance ───────────────────────────────────────────────────────

func newAccountBalanceCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	return &ff.Command{
		Name:      "balance",
		Usage:     "gigahost account balance",
		ShortHelp: "Show prepaid account credit.",
		Flags:     ff.NewFlagSet("balance").SetParent(parent),
		Exec: func(ctx context.Context, _ []string) error {
			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			bill, err := cl.Billing.Get(ctx)
			if err != nil {
				return err
			}

			type creditRow struct {
				Currency string `cli:"Currency"`
				Amount   string `cli:"Credit"`
			}

			rows := make([]creditRow, 0, len(bill.Credit))
			for _, cr := range bill.Credit {
				rows = append(rows, creditRow{Currency: cr.Currency, Amount: cr.Amount})
			}

			return c.Render(rows)
		},
	}
}

// ── account invoices ──────────────────────────────────────────────────────

func newAccountInvoicesCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	return &ff.Command{
		Name:      "invoices",
		Usage:     "gigahost account invoices",
		ShortHelp: "List invoices.",
		Flags:     ff.NewFlagSet("invoices").SetParent(parent),
		Exec: func(ctx context.Context, _ []string) error {
			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			// /billing is accessible with API-key tokens; /my/invoices is not.
			bill, err := cl.Billing.Get(ctx)
			if err != nil {
				return err
			}

			type invoiceRow struct {
				ID       string `cli:"ID"`
				Number   string `cli:"Number"`
				OrderID  string `cli:"Order ID"`
				Date     string `cli:"Date"`
				DueDate  string `cli:"Due Date"`
				Total    string `cli:"Total"`
				TotalVAT string `cli:"Total+VAT"`
				Paid     bool   `cli:"Paid"`
			}

			rows := make([]invoiceRow, 0, len(bill.Invoices))
			for _, inv := range bill.Invoices {
				rows = append(rows, invoiceRow{
					ID:       inv.ID,
					Number:   inv.Number,
					OrderID:  inv.OrderID,
					Date:     inv.Date.Format("2006-01-02"),
					DueDate:  inv.DueDate.Format("2006-01-02"),
					Total:    inv.Total,
					TotalVAT: inv.TotalVAT,
					Paid:     inv.Paid,
				})
			}

			return c.Render(rows)
		},
	}
}

// ── account users ─────────────────────────────────────────────────────────

func newAccountUsersCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("users").SetParent(parent)

	cmd := &ff.Command{
		Name:      "users",
		Usage:     "gigahost account users COMMAND",
		ShortHelp: "Manage user contacts on the account.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		newAccountUsersListCmd(c, fs, load),
		newAccountUsersGetCmd(c, fs, load),
		newAccountUsersInviteCmd(c, fs, load),
		newAccountUsersUpdateCmd(c, fs, load),
		newAccountUsersDeleteCmd(c, fs, load),
		newAccountUsersGrantServerCmd(c, fs, load),
		newAccountUsersRevokeServerCmd(c, fs, load),
	}

	return cmd
}

func newAccountUsersListCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	return &ff.Command{
		Name:      "list",
		Usage:     "gigahost account users list",
		ShortHelp: "List all user contacts.",
		Flags:     ff.NewFlagSet("list").SetParent(parent),
		Exec: func(ctx context.Context, _ []string) error {
			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			acc, err := cl.Account.Get(ctx)
			if err != nil {
				return err
			}

			type contactRow struct {
				ID          string `cli:"ID"`
				Name        string `cli:"Name"`
				Username    string `cli:"Email"`
				AccessLevel string `cli:"Access Level"`
				TwoFA       string `cli:"2FA"`
			}

			var rows []contactRow

			for _, ct := range acc.Contacts {
				twoFA := "no"
				if ct.TwoFA {
					twoFA = "yes"
				}

				rows = append(rows, contactRow{
					ID:          ct.ID,
					Name:        ct.Name,
					Username:    ct.Username,
					AccessLevel: ct.AccessLevel,
					TwoFA:       twoFA,
				})
			}

			return c.Render(rows)
		},
	}
}

func newAccountUsersGetCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	return &ff.Command{
		Name:      "get",
		Usage:     "gigahost account users get USER",
		ShortHelp: "Show a user contact with assigned servers.",
		Flags:     ff.NewFlagSet("get").SetParent(parent),
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return errors.New("exactly one USER argument is required (ID or email)")
			}

			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			uid, err := resolveUserArg(ctx, cl, args[0])
			if err != nil {
				return err
			}

			u, err := cl.Account.GetUser(ctx, uid)
			if err != nil {
				return err
			}

			return c.Render(u)
		},
	}
}

func newAccountUsersInviteCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("invite").SetParent(parent)

	var name, email, accessLevel string

	fs.StringVar(&name, 'n', "name", "", "display name")
	fs.StringVar(&email, 'e', "email", "", "email address")
	fs.StringVar(&accessLevel, 'a', "access-level", "user", "access level: admin, user, or server")

	return &ff.Command{
		Name:      "invite",
		Usage:     "gigahost account users invite --name NAME --email EMAIL [--access-level LEVEL]",
		ShortHelp: "Invite a new user contact.",
		Flags:     fs,
		Exec: func(ctx context.Context, _ []string) error {
			if name == "" {
				return errors.New("--name is required")
			}

			if email == "" {
				return errors.New("--email is required")
			}

			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			if err := cl.Account.InviteUser(ctx, gigahost.InviteUserRequest{
				Name:        name,
				Username:    email,
				AccessLevel: accessLevel,
			}); err != nil {
				return err
			}

			fmt.Fprintf(c.Out, "Invitation sent to %s\n", email)

			return nil
		},
	}
}

func newAccountUsersUpdateCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("update").SetParent(parent)

	var name, email, accessLevel, password string

	fs.StringVar(&name, 'n', "name", "", "display name")
	fs.StringVar(&email, 'e', "email", "", "email address")
	fs.StringVar(&accessLevel, 'a', "access-level", "", "access level: admin, user, or server")
	fs.StringVar(&password, 0, "password", "", "new password (optional)")

	return &ff.Command{
		Name:      "update",
		Usage:     "gigahost account users update USER [flags]",
		ShortHelp: "Update a user contact.",
		Flags:     fs,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return errors.New("exactly one USER argument is required (ID or email)")
			}

			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			uid, err := resolveUserArg(ctx, cl, args[0])
			if err != nil {
				return err
			}

			if err := cl.Account.UpdateUser(ctx, uid, gigahost.UpdateUserRequest{
				Name:        name,
				Username:    email,
				AccessLevel: accessLevel,
				Password:    password,
			}); err != nil {
				return err
			}

			fmt.Fprintf(c.Out, "Updated user %s\n", args[0])

			return nil
		},
	}
}

func newAccountUsersDeleteCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	return &ff.Command{
		Name:      "delete",
		Usage:     "gigahost account users delete USER",
		ShortHelp: "Remove a user contact.",
		Flags:     ff.NewFlagSet("delete").SetParent(parent),
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return errors.New("exactly one USER argument is required (ID or email)")
			}

			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			uid, err := resolveUserArg(ctx, cl, args[0])
			if err != nil {
				return err
			}

			if err := cl.Account.DeleteUser(ctx, uid); err != nil {
				return err
			}

			fmt.Fprintf(c.Out, "Deleted user %s\n", args[0])

			return nil
		},
	}
}

func newAccountUsersGrantServerCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("grant-server").SetParent(parent)

	var serverID string

	fs.StringVar(&serverID, 's', "server-id", "", "server ID or hostname to grant access to")

	return &ff.Command{
		Name:      "grant-server",
		Usage:     "gigahost account users grant-server USER --server-id SRV",
		ShortHelp: "Grant a user access to a server.",
		Flags:     fs,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return errors.New("exactly one USER argument is required (ID or email)")
			}

			if serverID == "" {
				return errors.New("--server-id is required")
			}

			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			uid, err := resolveUserArg(ctx, cl, args[0])
			if err != nil {
				return err
			}

			srv, err := cl.Servers.Resolve(ctx, serverID)
			if err != nil {
				return err
			}

			if err := cl.Account.GrantServerAccess(ctx, uid, srv.ID); err != nil {
				return err
			}

			fmt.Fprintf(c.Out, "Granted user %s access to server %s\n", args[0], serverID)

			return nil
		},
	}
}

func newAccountUsersRevokeServerCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	return &ff.Command{
		Name:      "revoke-server",
		Usage:     "gigahost account users revoke-server USER RELATION_ID",
		ShortHelp: "Revoke a user's access to a server.",
		Flags:     ff.NewFlagSet("revoke-server").SetParent(parent),
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 2 {
				return errors.New("exactly two arguments required: USER RELATION_ID")
			}

			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			uid, err := resolveUserArg(ctx, cl, args[0])
			if err != nil {
				return err
			}

			if err := cl.Account.RevokeServerAccess(ctx, uid, args[1]); err != nil {
				return err
			}

			fmt.Fprintf(c.Out, "Revoked relation %s from user %s\n", args[1], args[0])

			return nil
		},
	}
}

// sshKeyRow is the trimmed projection used for table output of SSH keys.
type sshKeyRow struct {
	ID      string `cli:"ID"`
	Name    string `cli:"Name"`
	AddedAt string `cli:"Added"`
}

// ── account ssh-keys ──────────────────────────────────────────────────────

func newAccountSSHKeysCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("ssh-keys").SetParent(parent)

	cmd := &ff.Command{
		Name:      "ssh-keys",
		Usage:     "gigahost account ssh-keys COMMAND",
		ShortHelp: "Manage SSH public keys on the account.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		newAccountSSHKeysListCmd(c, fs, load),
		newAccountSSHKeysAddCmd(c, fs, load),
		newAccountSSHKeysDeleteCmd(c, fs, load),
	}

	return cmd
}

func newAccountSSHKeysListCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	return &ff.Command{
		Name:      "list",
		Usage:     "gigahost account ssh-keys list",
		ShortHelp: "List SSH keys stored on the account.",
		Flags:     ff.NewFlagSet("list").SetParent(parent),
		Exec: func(ctx context.Context, _ []string) error {
			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			acc, err := cl.Account.Get(ctx)
			if err != nil {
				return err
			}

			var rows []sshKeyRow
			for _, k := range acc.SSHKeys {
				rows = append(rows, sshKeyRow{
					ID:      k.ID,
					Name:    k.Name,
					AddedAt: k.AddedAt.Format("2006-01-02"),
				})
			}

			return c.Render(rows)
		},
	}
}

func newAccountSSHKeysAddCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("add").SetParent(parent)

	var name, data string

	fs.StringVar(&name, 'n', "name", "", "key label")
	fs.StringVar(&data, 'd', "data", "", "OpenSSH public key data (e.g. ssh-ed25519 AAAA...)")

	return &ff.Command{
		Name:      "add",
		Usage:     "gigahost account ssh-keys add --name NAME --data KEY",
		ShortHelp: "Add an SSH public key to the account.",
		Flags:     fs,
		Exec: func(ctx context.Context, _ []string) error {
			if name == "" {
				return errors.New("--name is required")
			}

			if data == "" {
				return errors.New("--data is required")
			}

			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			if err := cl.Account.AddSSHKey(ctx, name, data); err != nil {
				return err
			}

			// AddSSHKey returns nothing; look up via Account.Get to get the ID.
			// A lookup failure is non-fatal: the key was still created.
			if acc, lookupErr := cl.Account.Get(ctx); lookupErr == nil {
				for _, k := range acc.SSHKeys {
					if k.Name == name {
						return c.Render(sshKeyRow{
							ID:      k.ID,
							Name:    k.Name,
							AddedAt: k.AddedAt.Format("2006-01-02"),
						})
					}
				}
			}

			if c.format == outputTable {
				fmt.Fprintf(c.Out, "Added SSH key %q\n", name)
			}

			return nil
		},
	}
}

func newAccountSSHKeysDeleteCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	return &ff.Command{
		Name:      "delete",
		Usage:     "gigahost account ssh-keys delete KEY",
		ShortHelp: "Remove an SSH key (by name or ID) from the account.",
		Flags:     ff.NewFlagSet("delete").SetParent(parent),
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return errors.New("exactly one KEY argument is required (name or ID)")
			}

			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			ids, err := cl.Account.ResolveSSHKeys(ctx, []string{args[0]})
			if err != nil {
				return err
			}

			if err := cl.Account.DeleteSSHKey(ctx, ids[0]); err != nil {
				return err
			}

			fmt.Fprintf(c.Out, "Deleted SSH key %s\n", args[0])

			return nil
		},
	}
}

// apiKeyCreatedRow is the row rendered on api-key create/rotate.
// The secret is shown once and included in structured output for JSON consumers.
// ID is the internal key id (from the list endpoint) needed for terraform import;
// it is empty when the follow-up list call fails (e.g. insufficient token scope).
type apiKeyCreatedRow struct {
	ID     string `cli:"ID"`
	Prefix string `cli:"Prefix"`
	Secret string `cli:"Secret (store now)"`
	Label  string `cli:"Label"`
}

// ── account api-keys ──────────────────────────────────────────────────────

func newAccountAPIKeysCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("api-keys").SetParent(parent)

	cmd := &ff.Command{
		Name:      "api-keys",
		Usage:     "gigahost account api-keys COMMAND",
		ShortHelp: "Manage personal API keys.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		newAccountAPIKeysListCmd(c, fs, load),
		newAccountAPIKeysGetCmd(c, fs, load),
		newAccountAPIKeysCreateCmd(c, fs, load),
		newAccountAPIKeysUpdateCmd(c, fs, load),
		newAccountAPIKeysDeleteCmd(c, fs, load),
		newAccountAPIKeysRotateCmd(c, fs, load),
	}

	return cmd
}

func newAccountAPIKeysListCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	return &ff.Command{
		Name:      "list",
		Usage:     "gigahost account api-keys list",
		ShortHelp: "List API keys.",
		Flags:     ff.NewFlagSet("list").SetParent(parent),
		Exec: func(ctx context.Context, _ []string) error {
			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			keys, err := cl.Account.ListAPIKeys(ctx)
			if err != nil {
				return err
			}

			type keyRow struct {
				ID         string `cli:"ID"`
				Label      string `cli:"Label"`
				Prefix     string `cli:"Prefix"`
				Status     string `cli:"Status"`
				LastUsedIP string `cli:"Last Used IP"`
			}

			var rows []keyRow
			for _, k := range keys {
				rows = append(rows, keyRow{
					ID:         k.ID,
					Label:      k.Label,
					Prefix:     k.Prefix,
					Status:     k.Status,
					LastUsedIP: k.LastUsedIP,
				})
			}

			return c.Render(rows)
		},
	}
}

func newAccountAPIKeysGetCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	return &ff.Command{
		Name:      "get",
		Usage:     "gigahost account api-keys get KEY_ID",
		ShortHelp: "Show one API key.",
		Flags:     ff.NewFlagSet("get").SetParent(parent),
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return errors.New("exactly one KEY_ID argument is required")
			}

			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			key, err := cl.Account.GetAPIKey(ctx, args[0])
			if err != nil {
				return err
			}

			return c.Render(key)
		},
	}
}

func newAccountAPIKeysCreateCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("create").SetParent(parent)

	var label string

	fs.StringVar(&label, 'l', "label", "", "human-readable label for the key")

	return &ff.Command{
		Name:      "create",
		Usage:     "gigahost account api-keys create --label LABEL",
		ShortHelp: "Create a new API key. The secret is shown once; store it.",
		Flags:     fs,
		Exec: func(ctx context.Context, _ []string) error {
			if label == "" {
				return errors.New("--label is required")
			}

			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			created, err := cl.Account.CreateAPIKey(ctx, gigahost.CreateAPIKeyRequest{
				Label: label,
			})
			if err != nil {
				return err
			}

			// Resolve the internal key ID needed for terraform import.
			// The create response does not include it; look it up via List.
			// If the list call fails (e.g. insufficient token scope) degrade
			// gracefully — the key was still created; ID stays empty.
			var keyID string

			if keys, listErr := cl.Account.ListAPIKeys(ctx); listErr == nil {
				for _, k := range keys {
					if k.Prefix == created.Prefix {
						keyID = k.ID

						break
					}
				}
			}

			return c.Render(apiKeyCreatedRow{
				ID:     keyID,
				Prefix: created.Prefix,
				Secret: created.Secret,
				Label:  created.Label,
			})
		},
	}
}

func newAccountAPIKeysUpdateCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("update").SetParent(parent)

	var label string

	fs.StringVar(&label, 'l', "label", "", "new label for the key")

	return &ff.Command{
		Name:      "update",
		Usage:     "gigahost account api-keys update KEY_ID --label LABEL",
		ShortHelp: "Update an API key's label.",
		Flags:     fs,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return errors.New("exactly one KEY_ID argument is required")
			}

			if label == "" {
				return errors.New("--label is required")
			}

			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			if err := cl.Account.UpdateAPIKey(ctx, args[0], gigahost.UpdateAPIKeyRequest{
				Label: label,
			}); err != nil {
				return err
			}

			fmt.Fprintf(c.Out, "Updated API key %s\n", args[0])

			return nil
		},
	}
}

func newAccountAPIKeysDeleteCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	return &ff.Command{
		Name:      "delete",
		Usage:     "gigahost account api-keys delete KEY_ID",
		ShortHelp: "Revoke an API key.",
		Flags:     ff.NewFlagSet("delete").SetParent(parent),
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return errors.New("exactly one KEY_ID argument is required")
			}

			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			if err := cl.Account.DeleteAPIKey(ctx, args[0]); err != nil {
				return err
			}

			fmt.Fprintf(c.Out, "Revoked API key %s\n", args[0])

			return nil
		},
	}
}

func newAccountAPIKeysRotateCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	return &ff.Command{
		Name:      "rotate",
		Usage:     "gigahost account api-keys rotate KEY_ID",
		ShortHelp: "Rotate an API key's secret. The old secret stops working immediately.",
		Flags:     ff.NewFlagSet("rotate").SetParent(parent),
		Exec: func(ctx context.Context, args []string) error {
			if len(args) != 1 {
				return errors.New("exactly one KEY_ID argument is required")
			}

			if err := load(); err != nil {
				return err
			}

			cl, err := c.Client()
			if err != nil {
				return err
			}

			rotated, err := cl.Account.RotateAPIKey(ctx, args[0])
			if err != nil {
				return err
			}

			// Resolve the internal key ID needed for terraform import.
			// The rotate response does not include it; look it up via List.
			// If the list call fails degrade gracefully — the key was still rotated.
			var (
				keyID    string
				keyLabel string
			)

			if keys, listErr := cl.Account.ListAPIKeys(ctx); listErr == nil {
				for _, k := range keys {
					if k.Prefix == rotated.Prefix {
						keyID = k.ID
						keyLabel = k.Label

						break
					}
				}
			}

			return c.Render(apiKeyCreatedRow{
				ID:     keyID,
				Prefix: rotated.Prefix,
				Secret: rotated.Secret,
				Label:  keyLabel,
			})
		},
	}
}

package cli

import (
	"context"
	"errors"
	"fmt"

	gigahost "github.com/kradalby/gigahost-go/client"
	"github.com/peterbourgon/ff/v4"
)

// newAuthCmd builds the `gigahost auth` subtree.
func newAuthCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("auth").SetParent(parent)

	cmd := &ff.Command{
		Name:      "auth",
		Usage:     "gigahost auth COMMAND",
		ShortHelp: "Manage API credentials.",
		Flags:     fs,
	}

	cmd.Subcommands = []*ff.Command{
		newAuthLoginCmd(c, fs, load),
		newAuthLogoutCmd(c, fs, load),
		newAuthWhoamiCmd(c, fs, load),
	}

	return cmd
}

func newAuthLoginCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("login").SetParent(parent)

	var code int

	fs.IntVar(&code, 0, "code", 0, "2FA code (if enabled)")

	return &ff.Command{
		Name:      "login",
		Usage:     "gigahost auth login",
		ShortHelp: "Authenticate with username+password and store the token.",
		LongHelp: `Authenticates with the Gigahost API using the provided username and
password (from flags, environment or config). On success the resulting
bearer token is stored in your OS keyring and re-used by subsequent
commands. If the keyring is unavailable, the token is written to
$XDG_CONFIG_HOME/gigahost/token.json with 0600 permissions.`,
		Flags: fs,
		Exec: func(ctx context.Context, _ []string) error {
			if err := load(); err != nil {
				return err
			}

			if c.Config.Username == "" || c.Config.Password == "" {
				return errors.New("auth login: username and password are required")
			}

			client, err := gigahost.NewClient(
				gigahost.WithCredentials(c.Config.Username, c.Config.Password, code),
				buildBaseURLOption(c.Config),
			)
			if err != nil {
				return err
			}

			tok, err := client.Auth.Authenticate(ctx, nil)
			if err != nil {
				return fmt.Errorf("authenticate: %w", err)
			}

			location, err := storeToken(c.Config.Username, tok.Token)
			if err != nil {
				return fmt.Errorf("store token: %w", err)
			}

			fmt.Fprintf(c.Out, "Authenticated as %s (customer %s)\n", c.Config.Username, tok.CustomerID)
			fmt.Fprintf(c.Out, "Token stored in: %s\n", location)

			return nil
		},
	}
}

func newAuthLogoutCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("logout").SetParent(parent)

	return &ff.Command{
		Name:      "logout",
		Usage:     "gigahost auth logout",
		ShortHelp: "Remove stored token from keyring and fallback file.",
		Flags:     fs,
		Exec: func(_ context.Context, _ []string) error {
			if err := load(); err != nil {
				return err
			}

			if c.Config.Username == "" {
				return errors.New("auth logout: no username configured")
			}

			if err := clearToken(c.Config.Username); err != nil {
				return fmt.Errorf("clear token: %w", err)
			}

			fmt.Fprintf(c.Out, "Logged out %s\n", c.Config.Username)

			return nil
		},
	}
}

func newAuthWhoamiCmd(c *Context, parent *ff.FlagSet, load func() error) *ff.Command {
	fs := ff.NewFlagSet("whoami").SetParent(parent)

	return &ff.Command{
		Name:      "whoami",
		Usage:     "gigahost auth whoami",
		ShortHelp: "Show the currently authenticated account.",
		Flags:     fs,
		Exec: func(ctx context.Context, _ []string) error {
			if err := load(); err != nil {
				return err
			}

			client, err := c.Client()
			if err != nil {
				return err
			}

			acc, err := client.Account.Get(ctx)
			if err != nil {
				return err
			}

			return c.Render(acc)
		},
	}
}

func buildBaseURLOption(cfg *Config) gigahost.Option {
	if cfg.BaseURL == "" {
		return nil
	}

	return gigahost.WithBaseURL(cfg.BaseURL)
}

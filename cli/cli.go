package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	gigahost "github.com/kradalby/gigahost-go/client"
	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
)

// Options configures a [Command] tree.
type Options struct {
	// Version is shown by `gigahost version` and in help output.
	Version string
	// Commit is shown by `gigahost version`.
	Commit string

	// Stdout and Stderr receive output and diagnostics respectively.
	// Defaults are os.Stdout and os.Stderr.
	Stdout io.Writer
	Stderr io.Writer

	// clientBuilder is an optional override used in tests to inject a
	// mock client.
	clientBuilder func(cfg *Config) (*gigahost.Client, error)
}

// Context is the shared state each subcommand Exec function operates
// against. It is built lazily from the resolved Config.
type Context struct {
	Config *Config
	Out    io.Writer
	Err    io.Writer

	format outputFormat
	client *gigahost.Client
	build  func(cfg *Config) (*gigahost.Client, error)
}

// Client returns a configured gigahost.Client, constructing one on
// demand. Commands that don't call the API (e.g. `version`, `config`)
// can safely ignore this.
func (c *Context) Client() (*gigahost.Client, error) {
	if c.client != nil {
		return c.client, nil
	}

	if c.build == nil {
		c.build = buildClient
	}

	client, err := c.build(c.Config)
	if err != nil {
		return nil, err
	}

	c.client = client

	return client, nil
}

// Render writes v using the CLI's output format.
func (c *Context) Render(v any) error {
	return render(c.Out, c.format, v)
}

// NewCommand constructs the root command for the gigahost CLI.
func NewCommand(opts Options) *ff.Command {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}

	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}

	rootFlags := ff.NewFlagSet("gigahost")

	flagsValues := &CLIFlags{}

	rootFlags.StringVar(&flagsValues.Token, 't', "token", "", "API bearer token (GIGAHOST_TOKEN)")
	rootFlags.StringVar(&flagsValues.Username, 'u', "username", "", "Account email (GIGAHOST_USERNAME)")
	rootFlags.StringVar(&flagsValues.Password, 'p', "password", "", "Account password (GIGAHOST_PASSWORD)")
	rootFlags.StringVar(&flagsValues.BaseURL, 0, "base-url", "", "Override the API base URL")
	rootFlags.StringEnumVar(&flagsValues.Output, 'o', "output", "Output format", "table", "json", "yaml")
	rootFlags.StringVar(&flagsValues.ConfigPath, 'c', "config", "", "Path to config file")

	// Build context lazily once flags are parsed.
	var cctx Context

	rootCmd := &ff.Command{
		Name:      "gigahost",
		Usage:     "gigahost [FLAGS] COMMAND ...",
		ShortHelp: "Command-line client for the gigahost.no API.",
		LongHelp: `gigahost administers servers, DNS zones, BGP sessions and more
via the gigahost.no API.

Credentials are resolved in standard 12-factor order:
  1. Config file (~/.config/gigahost/config.hujson)
  2. Environment variables (GIGAHOST_*)
  3. Command-line flags

Run "gigahost auth login" to store a token in your OS keyring.`,
		Flags: rootFlags,
	}

	// Install a pre-exec step: every leaf command will first invoke
	// loadContext to resolve config.  We do this by wrapping each
	// subcommand's Exec below.
	load := func() error {
		cfg, err := LoadConfig(*flagsValues)
		if err != nil {
			return err
		}

		format, err := parseOutputFormat(cfg.Output)
		if err != nil {
			return err
		}

		cctx.Config = cfg
		cctx.Out = opts.Stdout
		cctx.Err = opts.Stderr
		cctx.format = format
		cctx.build = opts.clientBuilder

		return nil
	}

	// Sub-commands.
	rootCmd.Subcommands = []*ff.Command{
		newVersionCmd(opts, rootFlags),
		newAuthCmd(&cctx, rootFlags, load),
		newDeployCmd(&cctx, rootFlags, load),
		newServersCmd(&cctx, rootFlags, load),
		newDNSCmd(&cctx, rootFlags, load),
		newBGPCmd(&cctx, rootFlags, load),
		newAccountCmd(&cctx, rootFlags, load),
	}

	return rootCmd
}

// Run executes the command tree against os.Args, returning a process
// exit code. This is intended to be called from main.
func Run(ctx context.Context, args []string, opts Options) int {
	cmd := NewCommand(opts)

	if err := cmd.ParseAndRun(ctx, args); err != nil {
		switch {
		case errors.Is(err, ff.ErrHelp):
			fmt.Fprintln(opts.Stdout, ffhelp.Command(cmd))

			return 0
		case errors.Is(err, ff.ErrNoExec):
			fmt.Fprintln(opts.Stdout, ffhelp.Command(cmd))

			return 0
		default:
			fmt.Fprintf(opts.Stderr, "error: %v\n", err)

			return 1
		}
	}

	return 0
}

// buildClient is the default client constructor used outside of tests.
func buildClient(cfg *Config) (*gigahost.Client, error) {
	if cfg == nil {
		return nil, errors.New("nil config")
	}

	var clientOpts []gigahost.Option
	if cfg.BaseURL != "" {
		clientOpts = append(clientOpts, gigahost.WithBaseURL(cfg.BaseURL))
	}

	// Prefer token over credentials when both are present.
	if cfg.Token != "" {
		clientOpts = append(clientOpts, gigahost.WithToken(cfg.Token))
	} else if cfg.Username != "" && cfg.Password != "" {
		clientOpts = append(clientOpts, gigahost.WithCredentials(cfg.Username, cfg.Password, 0))
	} else if cfg.Username != "" {
		// Credentials not in config but a username is — try the keyring.
		tok, err := loadToken(cfg.Username)
		if err != nil {
			return nil, fmt.Errorf("load stored token: %w", err)
		}

		if tok == "" {
			return nil, errors.New("no token found; run `gigahost auth login` or set GIGAHOST_TOKEN")
		}

		clientOpts = append(clientOpts, gigahost.WithToken(tok))
	} else {
		return nil, errors.New("no credentials configured; set GIGAHOST_TOKEN or run `gigahost auth login`")
	}

	return gigahost.NewClient(clientOpts...)
}

package cli

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	kjson "github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	kenv "github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/tailscale/hujson"
)

// Config is the resolved CLI configuration. It captures the complete
// surface of knobs a user may set to connect to the API.
type Config struct {
	// Token is a bearer token. If both Token and Username/Password
	// are set, Token takes precedence.
	Token string `json:"token,omitzero" koanf:"token"`
	// Username is the Gigahost account email.
	Username string `json:"username,omitzero" koanf:"username"`
	// Password is the account password.
	Password string `json:"password,omitzero" koanf:"password"`
	// BaseURL overrides the default API base URL.
	BaseURL string `json:"base_url,omitzero" koanf:"base_url"`
	// Output is the default format for commands: "table", "json" or
	// "yaml". Commands may override on a per-invocation basis.
	Output string `json:"output,omitzero" koanf:"output"`
	// ConfigPath is the path to the config file that produced this
	// Config, if any. Empty when the config came purely from flags
	// and/or environment.
	ConfigPath string `json:"-" koanf:"-"`
}

const (
	envPrefix  = "GIGAHOST_"
	koanfDelim = "."
	defaultOut = "table"
	configDir  = "gigahost"
)

// CLIFlags mirrors the global flags that the root command exposes. It
// is populated by the command tree and fed into [LoadConfig] last so it
// takes precedence over file and environment values.
type CLIFlags struct {
	Token      string
	Username   string
	Password   string
	BaseURL    string
	Output     string
	ConfigPath string
}

// LoadConfig resolves the effective configuration by merging (lowest to
// highest):
//
//  1. The config file at flags.ConfigPath, or a standard default if
//     empty.
//  2. Environment variables prefixed with GIGAHOST_.
//  3. The values in flags that are non-empty.
//
// When the config file is the default path and does not exist, this is
// silently tolerated. When an explicitly specified config file is
// missing, an error is returned.
func LoadConfig(flags CLIFlags) (*Config, error) {
	k := koanf.New(koanfDelim)

	// 1. Config file.
	configPath, err := resolveConfigPath(flags.ConfigPath)
	if err != nil {
		return nil, err
	}

	if configPath != "" {
		if err := loadConfigFile(k, configPath); err != nil {
			return nil, err
		}
	}

	// 2. Environment variables.
	if err := k.Load(kenv.Provider(koanfDelim, kenv.Opt{
		Prefix: envPrefix,
		TransformFunc: func(key, value string) (string, any) {
			key = strings.ToLower(strings.TrimPrefix(key, envPrefix))

			return key, value
		},
	}), nil); err != nil {
		return nil, fmt.Errorf("load env: %w", err)
	}

	// 3. CLI flags (only those the user set).
	overrides := map[string]any{}

	setIfNonEmpty := func(key, value string) {
		if value != "" {
			overrides[key] = value
		}
	}

	setIfNonEmpty("token", flags.Token)
	setIfNonEmpty("username", flags.Username)
	setIfNonEmpty("password", flags.Password)
	setIfNonEmpty("base_url", flags.BaseURL)
	setIfNonEmpty("output", flags.Output)

	if len(overrides) > 0 {
		if err := k.Load(confmap.Provider(overrides, koanfDelim), nil); err != nil {
			return nil, fmt.Errorf("load flag overrides: %w", err)
		}
	}

	var cfg Config
	if err := k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{Tag: "koanf"}); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg.ConfigPath = configPath

	if cfg.Output == "" {
		cfg.Output = defaultOut
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Validate checks for a minimal set of coherent values.
func (c *Config) Validate() error {
	switch c.Output {
	case "", "table", "json", "yaml":
	default:
		return fmt.Errorf("invalid output format %q: want table, json or yaml", c.Output)
	}

	if c.Token == "" && (c.Username == "" || c.Password == "") {
		// Allowed: some commands (e.g. `help`, `version`) do not need
		// auth. The API-hitting command layer decides when to error.
		return nil
	}

	return nil
}

func loadConfigFile(k *koanf.Koanf, path string) error {
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".yaml", ".yml":
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return fmt.Errorf("load yaml config %q: %w", path, err)
		}
	case ".json", ".jsonc", ".hujson":
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return fmt.Errorf("read config %q: %w", path, err)
		}
		// Normalise HuJSON/JSONC into strict JSON first.
		normalised, err := hujson.Standardize(data)
		if err != nil {
			return fmt.Errorf("parse hujson %q: %w", path, err)
		}
		// Round-trip through json/v2 to validate.
		var out any
		if err := json.Unmarshal(normalised, &out); err != nil {
			return fmt.Errorf("invalid config json %q: %w", path, err)
		}

		if err := k.Load(confmap.Provider(flattenMap(out), koanfDelim), nil); err != nil {
			return fmt.Errorf("load config %q: %w", path, err)
		}
	default:
		// Let koanf's json parser try to interpret the file; if the
		// user passed an unusual extension they probably still mean
		// JSON.
		if err := k.Load(file.Provider(path), kjson.Parser()); err != nil {
			return fmt.Errorf("load config %q: %w", path, err)
		}
	}

	return nil
}

// flattenMap collapses nested `map[string]any` into a dotted key map
// compatible with koanf's confmap provider.
func flattenMap(in any) map[string]any {
	out := map[string]any{}
	flatten("", in, out)

	return out
}

func flatten(prefix string, val any, out map[string]any) {
	m, ok := val.(map[string]any)
	if !ok {
		out[prefix] = val

		return
	}

	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}

		flatten(key, v, out)
	}
}

// resolveConfigPath picks a config file to load. When an explicit path
// is given, its existence is required. Otherwise, well-known defaults
// are tried in order, and an empty string is returned if none exist.
func resolveConfigPath(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("config file %q: %w", explicit, err)
		}

		return explicit, nil
	}

	configHome, err := xdgConfigHome()
	if err != nil {
		// Determining XDG_CONFIG_HOME is best-effort; the absence of
		// a home directory is not a hard error when the user has
		// already supplied credentials via flags or env vars.
		return "", nil //nolint:nilerr // intentional best-effort fallthrough
	}

	dir := filepath.Join(configHome, configDir)

	for _, name := range []string{"config.hujson", "config.jsonc", "config.json", "config.yaml", "config.yml"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", nil
}

// xdgConfigHome returns $XDG_CONFIG_HOME if set, falling back to
// $HOME/.config.
func xdgConfigHome() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	if home == "" {
		return "", errors.New("could not determine user home directory")
	}

	return filepath.Join(home, ".config"), nil
}

// Package cli implements the gigahost command-line tool.
//
// The CLI uses peterbourgon/ff/v4 for command-tree parsing and knadh/koanf/v2
// for configuration, supporting three sources in standard 12-factor
// precedence:
//
//  1. Config file (lowest) — $XDG_CONFIG_HOME/gigahost/config.{hujson,jsonc,json,yaml,yml}
//  2. Environment variables — GIGAHOST_TOKEN, GIGAHOST_USERNAME, etc.
//  3. Command-line flags (highest)
//
// Configuration files support HuJSON / JSONC (JSON with comments and
// trailing commas) through github.com/tailscale/hujson, as well as plain
// JSON and YAML. HuJSON is the recommended canonical format.
package cli

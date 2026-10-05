# gigahost-go

[![License: BSD-3-Clause](https://img.shields.io/badge/License-BSD--3--Clause-blue.svg)](https://opensource.org/licenses/BSD-3-Clause)
[![Go Reference](https://pkg.go.dev/badge/github.com/kradalby/gigahost-go.svg)](https://pkg.go.dev/github.com/kradalby/gigahost-go)

Go API client, CLI, and Terraform provider for
[gigahost.no](https://gigahost.no/en/api-dokumentasjon) — a Norwegian hosting
provider offering servers, DNS, domain registration, BGP peering and more.

## Contents

- [`client`](./client) — strongly typed Go API client at
  `github.com/kradalby/gigahost-go/client`.
- [`cmd/gigahost`](./cmd/gigahost) — `gigahost` CLI binary.
- [`tfprovider`](./tfprovider) — OpenTofu / Terraform provider
  implementation.

## Quick start

### API client

```go
import "github.com/kradalby/gigahost-go/client"

c, err := client.NewClient(
    client.WithToken(os.Getenv("GIGAHOST_TOKEN")),
)
if err != nil { /* ... */ }

zones, err := c.DNS.ListZones(ctx)
servers, err := c.Servers.List(ctx)
```

Alternatively, authenticate with username + password (the client will fetch
and refresh the bearer token for you):

```go
c, err := client.NewClient(
    client.WithCredentials(os.Getenv("GIGAHOST_USERNAME"), os.Getenv("GIGAHOST_PASSWORD")),
)
```

### CLI

```console
$ gigahost auth login
$ gigahost servers list
$ gigahost deploy sizes
$ gigahost deploy create --type value --size 2c-4gb-40gb --os debian-12 --ssh-keys laptop
$ gigahost dns records create --zone example.no --name www --type A --value 1.2.3.4
$ gigahost servers reboot web01
```

### OpenTofu / Terraform

The provider is developed against [OpenTofu](https://opentofu.org)
first, with [Terraform](https://www.terraform.io) compatibility
maintained in CI. Both speak the same plugin protocol, so the same
provider binary works unchanged with either CLI.

```hcl
terraform {
  required_providers {
    gigahost = {
      source  = "kradalby/gigahost"
      version = "~> 0.0.1"
    }
  }
}

provider "gigahost" {
  token = var.gigahost_token
}

resource "gigahost_server" "web" {
  type     = "value"       # gigahost deploy types
  size     = "2c-4gb-40gb" # gigahost deploy sizes
  os       = "debian-12"   # gigahost deploy os
  hostname = "web01"
}

resource "gigahost_dns_zone" "example" {
  name = "example.no"
}

resource "gigahost_dns_record" "www" {
  zone_id = gigahost_dns_zone.example.id
  name    = "www"
  type    = "A"
  value   = gigahost_server.web.ip
  ttl     = 3600
}
```

```console
$ tofu init
$ tofu plan
```

## Coverage

The Terraform provider covers the full server lifecycle, DNS, BGP and account
management.

**Resources**

| Area    | Resources                                                                                                                                                                                                                       |
| ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Servers | `gigahost_server` (deploy hourly cloud VMs by slug — `type`/`size`/`os`; changing `os` reinstalls in place, keeping the IP), `gigahost_server_ipv4`, `gigahost_server_snapshot`, `gigahost_server_name`, `gigahost_server_rdns` |
| DNS     | `gigahost_dns_zone`, `gigahost_dns_record`, `gigahost_dns_redirect`, `gigahost_dns_dnssec`, `gigahost_dns_external_ds_records`, `gigahost_dns_nameservers`, `gigahost_dns_ptr_zone`                                             |
| BGP     | `gigahost_bgp_asn`, `gigahost_bgp_session`                                                                                                                                                                                      |
| Account | `gigahost_account_ssh_key`, `gigahost_account_api_key`                                                                                                                                                                          |

**Data sources**

`gigahost_server`, `gigahost_servers`, `gigahost_server_size`,
`gigahost_server_sizes`, `gigahost_server_catalog`, `gigahost_region`,
`gigahost_regions`, `gigahost_operating_system`, `gigahost_operating_systems`,
`gigahost_isos`, `gigahost_ssh_key`, `gigahost_ssh_keys`, `gigahost_dns_zone`,
`gigahost_dns_zones`, `gigahost_dns_records`, `gigahost_bgp`, `gigahost_account`.

No user-facing attribute takes an opaque catalog ID: servers are selected by
slugs (`type = "value"`, `size = "2c-4gb-40gb"`, `os = "debian-12"`,
`region = "sfj"`) derived from live API data, and the CLI lists every valid
value (`gigahost deploy types|sizes|regions|os|isos`). `*_id` attributes are
always cross-resource references wired through the dependency graph.

Registry docs, examples and the CHANGELOG live in
[kradalby/terraform-provider-gigahost](https://github.com/kradalby/terraform-provider-gigahost/tree/main/docs); the Go
client and CLI cover the remaining endpoints (power, ISO, IPMI, upgrades, dyndns,
invoices/billing, and account user management). Power and resize are not exposed
as Terraform resources because they are not supported on the hourly cloud VPS
product; `gigahost_server_ipv4` ships but the API rejects IP orders on the test
account and has no release endpoint, so it is governed by a `deletion_policy`
(see [`docs/terraform-lifecycle.md`](./docs/terraform-lifecycle.md) and
[`docs/upstream-issues.md`](./docs/upstream-issues.md) B18/B19).

## Development

All tooling is exposed as flake apps, pinned to a consistent
Go 1.27 + golangci-lint + OpenTofu toolchain. No `make`, no `nix develop`
wrapper needed:

```console
$ nix run .#test       # unit tests (both modules, race + -short)
$ nix run .#lint       # golangci-lint
$ nix run .#fmt        # gofumpt + golangci-lint --fix
$ nix run .#tidy       # go mod tidy
$ nix build .#gigahost # build the CLI

# Live tests against the API (need GIGAHOST_TOKEN):
$ nix run .#test-acc   # Terraform acceptance tests (TF_ACC, via OpenTofu)
$ nix run .#test-e2e   # Go SDK e2e + CLI smoke tests (-tags e2e)
```

### Fresh-install support matrix

`TestAccServer_perOS` covers Debian 12/13, Ubuntu 24.04/26.04 and the latest
cloud amd64 AlmaLinux, RockyLinux and Fedora in the live catalog. Each OS gets
separate key and password deployments. All cases share the same size, region
and generated SSH key; password cases omit `ssh_keys` and require the password
in provider state. Success means root SSH can run `hostname`. Each VM is
cancelled after its check, including failures, unless failed-VM retention is
explicitly enabled below. There are no reinstalls.

Run locally from this checkout: `.envrc` loads the test-account token from
`infra/gigahost/dev/api-token`. The matrix needs its own opt-in, so ordinary
acceptance runs and CI skip it. Save Go's JSON test events to compare runs:

```sh
umask 077
matrix_dir="$PWD/.direnv/deploy-matrix-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$matrix_dir"
GIGAHOST_TEST_MATRIX_DIR="$matrix_dir" direnv exec . nix develop --command sh -c '
  export TF_ACC=1 GIGAHOST_TEST_OS_MATRIX=1
  export TF_ACC_TERRAFORM_PATH="$(command -v tofu)"
  export GIGAHOST_TEST_DEPLOY_TYPE=performance
  export GIGAHOST_TEST_DEPLOY_SIZE=2c-4gb-40gb
  go test -json -count=1 -timeout 2h -parallel 4 ./tfprovider \
    -run "^TestAccServer_perOS$"
' > "$matrix_dir/events.jsonl"

jq -r '
  (.case | split("/")) as $case
  | [$case[1], $case[2], .server_id,
     (if .passed then "pass" else "fail" end), .retained] | @tsv
' "$matrix_dir"/*/*/share/result.json
```

`-parallel 1` runs serially. Add `/debian-13/key` to `-run` to rerun one cell.
To reuse a retained run's key, set `GIGAHOST_TEST_MATRIX_KEY_FILE` to its
`private/id_ed25519`. It must still be registered on the account; the rerun
leaves this supplied key intact.
Subtest names contain the resolved versions, so "latest" results remain
traceable when the catalog changes. Missing images are skipped, and missing
passwords fail explicitly rather than falling back to key authentication.

For console debugging, set `GIGAHOST_TEST_KEEP_FAILED=1` before running.
Failed VMs and their shared account SSH key remain until manually cancelled;
successful VMs are still cancelled. Each case saves its VM ID in
`<os>/<auth>/share/result.json` and its Terraform config, exact deploy request,
server response and API-provided installer details in the same `share/` folder.
These JSON files redact passwords and tokens. Only share the `share/` files
with Gigahost. The separate `private/` folders contain debug credentials and
the run's SSH private key, all written with mode 0600. Installer details are
captured while installing because the API removes them when installation ends.

Some acceptance tests need live prerequisites the standard test account
lacks and skip unless gated env vars are set:

| Variable                                                   | Unlocks                                                  |
| ---------------------------------------------------------- | -------------------------------------------------------- |
| `GIGAHOST_TEST_ZONE_APEX`                                  | DNS zone/record tests (zones created under it)           |
| `GIGAHOST_TEST_REGISTERED_ZONE`                            | `dns_dnssec`, `dns_nameservers` (registered .no zone ID) |
| `GIGAHOST_TEST_NS1` / `GIGAHOST_TEST_NS2`                  | external nameservers for `dns_nameservers`               |
| `GIGAHOST_TEST_EXTERNAL_ZONE` + `_DS_KEY_TAG`/`_DS_DIGEST` | `dns_external_ds_records`                                |
| `GIGAHOST_TEST_IP_PREFIX` + `GIGAHOST_TEST_PTR_ZONE_NAME`  | `dns_ptr_zone`                                           |
| `GIGAHOST_TEST_ASN`                                        | `bgp_asn` (submission cannot be withdrawn)               |
| `GIGAHOST_TEST_ASN_ID` + `GIGAHOST_TEST_IPV4_IP_ID`        | `bgp_session` (needs an approved ASN)                    |

For an interactive shell with the toolchain, `nix develop` (or direnv) still
works.

Pre-commit hooks are managed with [prek](https://prek.j178.dev/):

```console
$ prek install
$ prek run --all-files
```

## Releasing

This module is untagged. The provider ships from
[kradalby/terraform-provider-gigahost](https://github.com/kradalby/terraform-provider-gigahost),
which pins a commit of this module, regenerates its docs from the schema here,
and carries the version tags; see its README.

## Project design

- **JSON v2**: every `json` import is the standard library's
  `encoding/json/v2`; v1 `encoding/json` is banned by a `depguard` rule.
- **Flat package layout**: no `internal/` or `pkg/` directories. All packages
  are importable.
- **Idiomatic Go types**: API values that come as strings like `"0"`/`"1"` or
  `"1700000000"` are normalized into real `bool` and `time.Time` via custom
  `UnmarshalJSON`. API field names are mapped to idiomatic Go field names.
- **Strict linting**: `default: all` in golangci-lint with curated disables.

## Status

This project is under active development. The API surface covers the full
[Gigahost API v0](https://gigahost.no/en/api-dokumentasjon). Breaking changes
may occur before the first tagged release.

## License

BSD-3-Clause. See [LICENSE](./LICENSE).

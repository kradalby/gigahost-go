# Changelog

All notable changes to the Gigahost Terraform/OpenTofu provider are documented
here. The format follows [Keep a Changelog](https://keepachangelog.com/), and
the provider aims to follow semantic versioning.

## Unreleased

FEATURES:

- **New Resource:** `gigahost_server_ipv4` — orders an additional IPv4 address
  (`l2`/`l3`) onto a server. The API has no IP-release endpoint, so destroy is
  governed by a `deletion_policy` (`retain` drops it from state with a warning;
  `error` refuses). The assigned `id`/`address` wire into `gigahost_server_rdns`
  and `gigahost_bgp_session`.
- **New Data Source:** `gigahost_dns_records` — lists every record in a zone.
- **New Data Source:** `gigahost_ssh_keys` — lists all account SSH keys.
- **New Data Source:** `gigahost_server_catalog` — returns the full deploy
  catalog (tiers, products, regions) including raw `product_id`/`price_id`.

ENHANCEMENTS:

- `gigahost_server` — deploy lifecycle hardening: the deploy order id is now
  recorded in state (`order_id`) the moment the order is placed, so a deploy
  that fails or times out mid-provisioning is saved to state tainted and
  cancelled by `terraform destroy` instead of orphaning a billed server.
- `gigahost_server` — the create wait now tolerates transient `/deploy/status`
  errors, falls back to the durable server record once a server id appears,
  fails fast when a server disappears from both views, and detects an explicit
  failure status rather than waiting out the timeout.
- `gigahost_server` — refresh no longer drops a live server from state on a
  single transient `/servers` gap: absence is confirmed across repeated reads.
- `gigahost_server` — `terraform destroy` of a server that died during
  provisioning no longer fails forever: a refused cancellation is reconciled
  against the server list and a confirmed-gone server is cleared with a warning.
- `gigahost_server` — `ipv6` is stored as null rather than an empty string when
  no address is assigned, and a known address is preserved when the server list
  omits it.
- `gigahost_server` — a configurable `create` timeout via a `timeouts` block.
- `gigahost_server` — new read-only attributes: `location`, `vps_type`,
  `suspended`, `bandwidth`, `created_at`, `os_name`, `os_release`, and a nested
  `ips` block (per-IP `id`/`subnet_id`/`address` for wiring rDNS and BGP).
- `gigahost_server_size` / `gigahost_server_sizes` — new `category` attribute
  (`vm`, `dedicated`, or `auction`).
- CLI: `gigahost servers list` now shows virtualization type and suspended
  state.

BUG FIXES:

- `gigahost_server` — importing a server whose RAM the API reports in MB no
  longer falsely fails the size match; the comparison normalizes to GB.
- `gigahost_server` — a failure to read server details immediately after deploy
  is surfaced (with the server kept in state tainted) instead of being silently
  swallowed with catalog-fallback values.

## 0.0.1

Initial release.

# Gigahost Terraform Provider: Server Action Lifecycle Design

This document is the single decisive mapping of every server-action capability into the Terraform lifecycle. It supersedes the per-capability analyses and is binding for implementation.

## Guiding principles

1. **One resource = one API entity or one mutable surface.** This is already the codebase convention: `gigahost_server` owns deployment, `gigahost_server_name` owns the label (`PUT /servers/{id}/name`), `gigahost_server_rdns` owns rDNS (`PUT /servers/{id}/reverse`). New server-side mutable surfaces follow the same anchored-to-`server_id` pattern.
2. **Match the resource lifecycle to the API verb, not to the user's mental model.** If the API has only an in-place mutation, model in-place Update. If the API has only a create with no delete, model create-only with a no-op Delete. If the API is a one-shot grant with no readable/durable state, it does not belong in Terraform at all.
3. **`RequiresReplace` is reserved for "the only way to change this is destroy + redeploy".** Today every input on `gigahost_server` is `RequiresReplace`. That is **correct for some inputs and wrong for `os_id` and `product_id`/`price_id`**, which now have genuine in-place API paths (Reinstall, Upgrade). See section 2.
4. **Never silently plan a change the API cannot apply.** `rescue` is the canonical trap: it looks mutable-but-costly (`RequiresReplace`) but is actually immutable-by-API. We keep it as `RequiresReplace` and document it loudly rather than pretend a toggle exists.

---

## 1. Decision table

| Capability              | Terraform home                                    | Lifecycle                      | Rebuild vs in-place                                                     | One-line rationale                                                                             |
| ----------------------- | ------------------------------------------------- | ------------------------------ | ----------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| **Server name / label** | `gigahost_server_name` (exists)                   | in-place-update                | in-place (`PUT /name`)                                                  | Pure metadata mutation; already correctly modeled.                                             |
| **Reverse DNS**         | `gigahost_server_rdns` (exists)                   | in-place-update                | in-place (`PUT /reverse`); changing `ip_id`/`subnet_id` replaces        | Metadata on an IP; `dns` mutates, target change is a new resource.                             |
| **OS reinstall**        | `gigahost_server.os_id` (folded in)               | in-place-update                | in-place (`POST /reinstall`, same ID + IP)                              | os→os change reinstalls in place with a plan warning; iso/rescue transitions replace. See 3.1. |
| **Resize / upgrade**    | `gigahost_server_upgrade` (new)                   | in-place-update                | in-place (`POST /upgrade`, same ID + IP)                                | Hardware upsize on existing server; no new infra.                                              |
| **Power state**         | not shipped — see §6 and B23                      | create + update                | in-place (GET power/on\|off\|reboot)                                    | Declarative desired power state, continuously reconciled; never rebuilds.                      |
| **Snapshot**            | `gigahost_server_snapshot` (new)                  | create + delete                | replace on any input (immutable entity)                                 | Independent entity with own ID + state machine; no update path.                                |
| **Extra IPv4**          | `gigahost_server_ipv4` (new)                      | create-only                    | create-only; Delete governed by `deletion_policy` (no release endpoint) | Order-only API; IP persists on server. Order rejected on the test account (B19).               |
| **ISO mount**           | `gigahost_server_iso` (new)                       | in-place-update                | in-place (`POST /isos` re-mounts)                                       | Mutable mount on existing server; remount swaps ISO in place.                                  |
| **Backups**             | `gigahost_server.backups` attribute (exists)      | requires-replace               | destroy + redeploy                                                      | Deploy-time-only flag; no in-place toggle endpoint exists.                                     |
| **Rescue**              | `gigahost_server.rescue` attribute (exists) + CLI | create-only / requires-replace | destroy + redeploy                                                      | Deploy-time-only boot option; no runtime toggle endpoint. CLI for deploy-time rescue.          |
| **IPMI / KVM**          | CLI only (`gigahost servers ipmi`)                | imperative-action              | n/a                                                                     | Ephemeral single-use credentials, auto-expire, not readable state.                             |

---

## 1.5 Slug overhaul (2026-06-11)

`gigahost_server` no longer takes catalog IDs. The selectors are
human-readable slugs derived from live API data (never hardcoded):

| Attribute  | Example                                  | Source                                               |
| ---------- | ---------------------------------------- | ---------------------------------------------------- |
| `platform` | `cloud` (default) / `metal`              | product `type` field (`vm`/`dedicated`)              |
| `type`     | `value`, `performance`                   | tier `group_name` minus platform noise               |
| `size`     | `2c-4gb-40gb`                            | `specs` (cores/ram/disk; metal leads with CPU model) |
| `region`   | `sfj` (optional while one region exists) | `region_name_short`                                  |
| `os`       | `debian-12` (codenames also resolve)     | slugified `os_name`                                  |
| `iso`      | ISO display name                         | `GET /deploy/isos`                                   |

`product_id`, `product_hash`, `price_id` (1:1 with product — verified),
`region_id`, `os_id`, `iso_id` are gone from the user surface. Resolution
happens once at Create (Update for `os`); errors list every valid value.
The shared resolver lives in `client/slugs.go` and is contract-tested
offline (round-trip/uniqueness/fuzz) and live (`e2e/slugs_test.go`
trip-wires on catalog changes — it already caught the metal i5/i7 16GB
spec collision, hence the CPU-model disambiguator).

The sections below predate the rename and use `os_id`/`product_id`
vocabulary when describing API fields and design history; the resource
attribute names are now `os`/`type`/`size`/`region`.

## 1.6 Import reference for existing resources

All 15 shipped resources support `terraform import`. The table below is the
authoritative quick-reference; each resource's docs page (`docs/resources/`)
contains the same commands as rendered shell blocks.

| Terraform type                     | Import ID format                                           | CLI discovery                                                       | Caveats                                                                                                                                                                                                                                                                                                                                                                                          |
| ---------------------------------- | ---------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `gigahost_dns_zone`                | `ZONE_ID` or zone name                                     | `gigahost dns zones list`                                           | —                                                                                                                                                                                                                                                                                                                                                                                                |
| `gigahost_dns_record`              | `ZONE_ID/RECORD_ID` (zone part accepts name)               | `gigahost dns zones list` + `gigahost dns records list --zone ZONE` | —                                                                                                                                                                                                                                                                                                                                                                                                |
| `gigahost_dns_redirect`            | `ZONE_ID/SOURCE` (zone part accepts name; `@` = apex)      | `gigahost dns redirects list --zone ZONE`                           | —                                                                                                                                                                                                                                                                                                                                                                                                |
| `gigahost_dns_dnssec`              | `ZONE_ID` or zone name                                     | `gigahost dns zones list`                                           | —                                                                                                                                                                                                                                                                                                                                                                                                |
| `gigahost_dns_nameservers`         | `ZONE_ID` or zone name                                     | `gigahost dns zones list`                                           | `nameservers` is null after import (API has no GET); the first apply re-pushes the configured set to the registrar.                                                                                                                                                                                                                                                                              |
| `gigahost_dns_external_ds_records` | `ZONE_ID` or zone name                                     | `gigahost dns zones list`                                           | —                                                                                                                                                                                                                                                                                                                                                                                                |
| `gigahost_dns_ptr_zone`            | `ZONE_ID` or arpa zone name                                | `gigahost dns zones list`                                           | `prefix` is stored in canonical CIDR form; supply the matching config value.                                                                                                                                                                                                                                                                                                                     |
| `gigahost_bgp_asn`                 | `212345` or `AS212345`                                     | `gigahost bgp show`                                                 | —                                                                                                                                                                                                                                                                                                                                                                                                |
| `gigahost_bgp_session`             | `SESSION_ID`                                               | `gigahost bgp show`                                                 | —                                                                                                                                                                                                                                                                                                                                                                                                |
| `gigahost_server`                  | `SERVER_ID`                                                | `gigahost servers list`                                             | `password` and `ssh_keys` are not recoverable. `type`/`size` are not round-tripped from the API; the first apply verifies them against the live machine's cores/RAM — a genuine mismatch fails loudly. The computed deploy facts (`memory_gb`, `storage_gb`, `rate_hourly`, `rate_monthly`) are null right after import and are filled from the live catalog during that first apply (adoption). |
| `gigahost_server_name`             | `SERVER_ID`                                                | `gigahost servers list`                                             | —                                                                                                                                                                                                                                                                                                                                                                                                |
| `gigahost_server_rdns`             | `SERVER_ID/IP_ID` for IPv4, `SERVER_ID/SUBNET_ID` for IPv6 | `gigahost servers ips SERVER_ID`                                    | —                                                                                                                                                                                                                                                                                                                                                                                                |
| `gigahost_server_snapshot`         | `SERVER_ID/SNAPSHOT_ID`                                    | `gigahost servers snapshots list SERVER_ID`                         | —                                                                                                                                                                                                                                                                                                                                                                                                |
| `gigahost_account_ssh_key`         | `KEY_ID`                                                   | `gigahost account ssh-keys list`                                    | Public key is stored in trimmed canonical form; the semantic equality check handles trailing-whitespace differences.                                                                                                                                                                                                                                                                             |
| `gigahost_account_api_key`         | `KEY_ID`                                                   | `gigahost account api-keys list`                                    | The secret is shown once at creation and cannot be recovered by import; `secret` will be null in state after import.                                                                                                                                                                                                                                                                             |

---

## 2. `gigahost_server` input mutability

Today **all** inputs are `RequiresReplace`. That is a blunt default that is wrong for two inputs that now have in-place API paths. The corrected design keeps `gigahost_server` as the **deployment contract** but stops forcing a destroy where the API offers a real in-place mutation. Where an in-place path exists, the mutation is **delegated to the dedicated action resource**, and the corresponding `gigahost_server` input stays `RequiresReplace` to preserve a single source of truth (see the per-input notes).

| Input        | Today                            | Correct disposition                                                                                                                                                                 | In-place API call (if any)                                                                                                              | Notes                                                                                                                                                                                                                                                                                                                                                              |
| ------------ | -------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `product_id` | RequiresReplace                  | **RequiresReplace on `gigahost_server`**, but real changes go through `gigahost_server_upgrade`                                                                                     | `POST /servers/{id}/upgrade` (`pkg_id`) — same ID + IP                                                                                  | The deploy resource fixes the _initial_ product. Post-deploy resizes are an explicit `gigahost_server_upgrade` resource, not an edit to this attribute. Do **not** drop `RequiresReplace` here: an upgrade does not change `product_id` _at deploy time_, and making it mutable would create two writers for one field. Downgrade is likely API-blocked (confirm). |
| `price_id`   | RequiresReplace                  | **RequiresReplace** (correct)                                                                                                                                                       | none                                                                                                                                    | Billing/price selection is fixed at deploy; an upgrade re-derives price from the package. Keep immutable.                                                                                                                                                                                                                                                          |
| `region_id`  | RequiresReplace                  | **RequiresReplace** (correct)                                                                                                                                                       | none                                                                                                                                    | No live-migration endpoint. Region change is genuinely destroy + redeploy.                                                                                                                                                                                                                                                                                         |
| `os_id`      | **mutable (in-place reinstall)** | os→os reinstalls in place; null-involved transitions replace (decided in `ModifyPlan`)                                                                                              | `POST /servers/{id}/reinstall` (`os_id`) — same ID + IP, disk wiped                                                                     | Folded in 2026-06-11 (was a separate `gigahost_server_reinstall` resource). The data loss is surfaced by an explicit plan-time warning rather than a separate opt-in resource.                                                                                                                                                                                     |
| `iso_id`     | RequiresReplace                  | **Remove from `gigahost_server` deploy intent for post-deploy use; keep only as a deploy-time boot option (RequiresReplace).** Post-deploy ISO work lives in `gigahost_server_iso`. | `POST /servers/{id}/isos` (mount)                                                                                                       | Two distinct concerns: (a) boot-from-ISO at deploy time (immutable, RequiresReplace, mutually exclusive with `os_id`/`rescue`); (b) post-deploy ISO mounting (mutable, owned by `gigahost_server_iso`). Do not let `iso_id` on the deploy resource imply ongoing mount management.                                                                                 |
| `rescue`     | RequiresReplace                  | **RequiresReplace + documented as immutable-by-API.** No runtime toggle resource.                                                                                                   | none                                                                                                                                    | The API has no `PUT/POST /servers/{id}/rescue`. `RequiresReplace` is mechanically correct but semantically misleading; the schema description must state explicitly that exiting rescue requires destroy + redeploy, and that mid-lifecycle rescue is a CLI/deploy-time-only operation until Gigahost ships a toggle endpoint.                                     |
| `hostname`   | RequiresReplace                  | **RequiresReplace at deploy.** Human-readable label changes go through `gigahost_server_name`.                                                                                      | `PUT /servers/{id}/name` mutates the _label_ (`srv_label`), not necessarily the install hostname. Reinstall may also accept a hostname. | Keep deploy `hostname` immutable. If the user wants a different display label, that is `gigahost_server_name` (already in-place). Confirm whether reinstall's `hostname` updates `srv_hostname` vs. only the label.                                                                                                                                                |
| `ssh_keys`   | RequiresReplace                  | **RequiresReplace** (correct)                                                                                                                                                       | none                                                                                                                                    | Keys are injected at provisioning into the image; there is no "re-inject keys" endpoint. Changing the desired key set is a redeploy (or a reinstall, which re-runs provisioning — confirm whether reinstall re-injects keys).                                                                                                                                      |
| `backups`    | RequiresReplace                  | **RequiresReplace** (correct, keep on `gigahost_server`)                                                                                                                            | none (`POST /deploy/servers` only)                                                                                                      | Deploy-time-only flag with a +25% cost. No `PUT /servers/{id}/backups`. Do **not** make a separate resource — you cannot mutate it, only set-at-create. RequiresReplace is the honest model. Add drift detection once `GET /servers/{id}` exposes the backups flag (confirm).                                                                                      |

**Net change to `gigahost_server`:** every input except `os_id` stays `RequiresReplace`. `os_id` is mutable: an os→os change is an in-place reinstall with a plan-time data-loss warning (section 3.1); transitions involving `iso_id`/`rescue` replace. Resize would follow the same pattern via `product_id` if the upgrade API ever applies to deployable server types (B10).

**New computed attributes on `gigahost_server`:**

- `power_state` (computed bool) — populated in `Read` from `GET /servers/{id}/powerstate`, so the deploy resource surfaces live power state without owning it. Read-only here; `gigahost_server_power` is the writer.
- `cores`, `ram`, `disk` (computed ints) — already on the `Server` struct (`Cores`, `RAM`); expose them so an `gigahost_server_upgrade` apply is observable as drift on the deploy resource's computed hardware fields.
- `primary_ip_id` (computed string) — the `ip_id` of the primary IPv4, to make wiring `gigahost_server_rdns` and `gigahost_server_power` ergonomic without a separate data-source lookup.

---

> **Status.** Sections 3.2, 3.3 and 3.6 are unimplemented design. As of
> 2026-08-25 the shipped server resources are `gigahost_server`,
> `gigahost_server_ipv4`, `gigahost_server_snapshot`, `gigahost_server_name`
> and `gigahost_server_rdns` — see `tfprovider/provider.go`. Power (B23),
> resize (B10) and ISO mount have no Terraform surface.

## 3. New resources

All new resources are anchored to `server_id` (which is `RequiresReplace`), mirroring `gigahost_server_name` / `gigahost_server_rdns`. All `Read` implementations must tolerate a vanished server by calling `resp.State.RemoveResource(ctx)` (the pattern in `server_resource.go:264`).

### 3.1 In-place OS swap — `gigahost_server.os` (folded in, 2026-06-11; slugs 2026-06-11)

Originally shipped as a separate `gigahost_server_reinstall` resource; **folded
into `gigahost_server`** after live verification, then renamed `os_id` → `os`
in the slug overhaul (the attribute now takes `debian-12`-style slugs resolved
against the live catalog at create/update time). Changing `os` between two
OS slugs runs `POST /servers/{id}/reinstall` as an **in-place Update**: same
server ID and IP, disk wiped, SSH keys not re-injected, root `password`
rotated. Any transition involving `iso` or `rescue` (or clearing `os`)
still replaces the server.

Implementation lives in `serverResource.ModifyPlan` + `Update`:

- **ModifyPlan** classifies the transition: os→os = in-place (emits a loud
  `In-place OS reinstall … ALL DATA ON DISK IS WIPED` plan warning and marks
  `password`/`status` unknown); any null-involved transition appends
  `RequiresReplace`.
- **Update** resolves the slug via `Reinstall.ResolveOS`, calls `Reinstall`,
  polls `StatusInstall == false` (15m cap), stores the rotated password.
- **Read** never refreshes the slug selectors: the API cannot round-trip them
  (live servers report `product_id: "0"` and a different location namespace),
  so resolution is create/update-time only — an OS changed outside Terraform
  is not detected as drift. This is the price of slug-based config; the trade
  was made deliberately in the slug overhaul.

The full transition matrix is tested twice: offline
(`TestServerModifyPlanMatrix`, all 8 combos incl. iso-sourced ones the account
cannot reach) and live (`TestAccServerOSChangeMatrix`, one server chain:
no-op → in-place reinstall → replace classifications → real replace via
rescue).

The plan-time warning is what makes a mutable destructive attribute
acceptable: Terraform cannot mark an in-place update as destructive, so the
provider says it explicitly. If OpenTofu ships `action` support
(opentofu/opentofu#3309), reinstall could additionally become an invokable
action.

### 3.2 `gigahost_server_upgrade` — in-place resize

Backed by `UpgradesService.Apply(ctx, serverID, packageID)` / `List` and `Servers.Get`.

| Attribute                    | Type                              | Notes                                                   |
| ---------------------------- | --------------------------------- | ------------------------------------------------------- |
| `id`                         | string, computed                  | `<server_id>`.                                          |
| `server_id`                  | string, required, RequiresReplace | Owning server.                                          |
| `package_id`                 | string, required                  | Target `pkg_id`. Change triggers Update → apply.        |
| `product_id`                 | string, computed                  | Resulting `srv.ProductID` after upgrade (drift signal). |
| `cores`, `ram_mb`, `disk_gb` | int, computed                     | Resulting hardware from `Servers.Get`.                  |

- **Create / Update:** call `Apply`; re-read `Servers.Get` to hydrate computed hardware.
- **Read:** populate computed fields from the server record. Validate `package_id` is still a coherent state (best effort; the API gives no "current package" back, so we trust the resulting `product_id`/hardware).
- **Delete:** no-op (upgrades are permanent; the API has no downgrade-to-original). Document that destroying the resource does **not** shrink the server.
- **Import:** by `server_id`; `package_id` is unknown post-hoc, so an imported upgrade needs config supplied to match (same caveat as the deploy resource's `ImportState`).

### 3.3 `gigahost_server_power` — declarative power state

Backed by `Servers.PowerOn` / `PowerOff` / `Reboot` / `GetPowerState`.

| Attribute             | Type                              | Notes                                                                                        |
| --------------------- | --------------------------------- | -------------------------------------------------------------------------------------------- |
| `id`                  | string, computed                  | `<server_id>`.                                                                               |
| `server_id`           | string, required, RequiresReplace | Owning server.                                                                               |
| `state`               | string, required                  | `on` or `off`. Validated enum.                                                               |
| `reboot_trigger`      | string, optional                  | Changing this value forces a one-shot `Reboot` on Update (reboot is an action, not a state). |
| `current_power_state` | bool, computed                    | From `GetPowerState`.                                                                        |

This resource is intentionally **create + update** (not create-only): power is a declarative desired state that Terraform should continuously reconcile (e.g. `state = "off"` for cost savings). `reboot` is modeled as an imperative trigger (a changed `reboot_trigger`) rather than a `state` value, because reboot is not a stable state.

- **Create:** read current state; if it differs from desired, call `PowerOn`/`PowerOff`. Idempotency is assumed (confirm); if the API errors on "already in state", treat that error as success.
- **Read:** `GetPowerState` → `current_power_state`. Report drift if it diverges from `state`.
- **Update:** if `state` changed, apply power op; if only `reboot_trigger` changed, call `Reboot`.
- **Delete:** no-op (leaves the server in whatever state it is — we do not force power-on on teardown).
- **Import:** by `server_id`; `state` is read from `GetPowerState`.

### 3.4 `gigahost_server_snapshot` — immutable point-in-time image

Backed by `SnapshotsService.Create` (async, returns no ID) / `List` / `Delete`.

| Attribute                             | Type                              | Notes                                                                           |
| ------------------------------------- | --------------------------------- | ------------------------------------------------------------------------------- |
| `id`                                  | string, computed                  | `<server_id>/<snapshot_id>`.                                                    |
| `server_id`                           | string, required, RequiresReplace | Owning server.                                                                  |
| `name`                                | string, required, RequiresReplace | Snapshots are immutable; renaming = new snapshot.                               |
| `snapshot_id`                         | int, computed                     | Hydrated by post-create `List` poll (the API does **not** return it on create). |
| `display_name`, `state`, `created_at` | computed                          | From the `Snapshot` record.                                                     |

- **Create:** call `Create(serverID, name)`, then poll `List` to find the new snapshot by name and capture its `snap_id`. Block until `State == completed` (bounded timeout) so downstream dependencies are safe. Capture creation start time to disambiguate same-named snapshots if needed (confirm name uniqueness).
- **Read:** `List` and match by `snapshot_id`; remove from state if absent.
- **Update:** none — every input is `RequiresReplace`.
- **Delete:** `Delete(serverID, snapshotID)`.
- **Import:** by `<server_id>/<snapshot_id>`.

### 3.5 `gigahost_server_ipv4` — extra IP, order-only

Backed by `Servers.OrderIPv4(ctx, serverID, ipType)` and `Servers.Get`.

| Attribute   | Type                              | Notes                                                          |
| ----------- | --------------------------------- | -------------------------------------------------------------- |
| `id`        | string, computed                  | `<server_id>/<ip_id>`.                                         |
| `server_id` | string, required, RequiresReplace | Owning server.                                                 |
| `ip_type`   | string, required, RequiresReplace | `l2` or `l3` (`IPTypeL2`/`IPTypeL3`). Immutable at order time. |
| `address`   | string, computed                  | The ordered IPv4, discovered via `Servers.Get`.                |
| `ip_id`     | string, computed                  | Discovered post-order.                                         |

- **Create:** call `OrderIPv4`, then `Servers.Get` and diff the `IPs` list against the prior set to identify the newly added IP (the order endpoint returns no body — confirm). Persist `ip_id` + `address`.
- **Read:** `Servers.Get`; if the `ip_id` is gone, remove from state.
- **Update:** none (all inputs `RequiresReplace`).
- **Delete:** governed by `deletion_policy` (default `retain`: drop from state with a warning since there is no release endpoint and the IP keeps billing; `error`: refuse). Wire the handler to call a future `DELETE /servers/{id}/ipv4/{ip_id}` if/when it ships.
- **Import:** by `<server_id>/<ip_id>`.

### 3.6 `gigahost_server_iso` — post-deploy ISO mount

Backed by `ISOsService.Mount` / `List` and `Servers.Get` (`StatusMount`).

| Attribute   | Type                              | Notes                                           |
| ----------- | --------------------------------- | ----------------------------------------------- |
| `id`        | string, computed                  | `<server_id>`.                                  |
| `server_id` | string, required, RequiresReplace | Owning server.                                  |
| `iso_id`    | string, required                  | ISO to mount. Change triggers Update → remount. |
| `mounted`   | bool, computed                    | From the ISO record / `StatusMount`.            |

- **Create / Update:** call `Mount(serverID, isoID)`. Update with a different `iso_id` re-mounts in place.
- **Read:** `List`; find the mounted ISO, set `mounted`. Remove from state if the server is gone.
- **Delete:** if a real unmount endpoint exists, call it; otherwise no-op + document that the ISO stays mounted on the live server (matching API reality). **Gated on confirming whether an unmount endpoint exists.**
- **Import:** by `<server_id>`.

### 3.7 Not a resource: IPMI / KVM

Remains CLI-only (`gigahost servers ipmi`, backed by `IPMIService.Create`). Sessions are single-use, auto-expiring credentials with no readable or durable state — they cannot be Read, Updated, or meaningfully Deleted. Any Terraform resource would either churn on every apply or hold expired secrets in state. If access control ever becomes declarative, it would be a server attribute (`kvm_acl`), not a session resource.

---

## 4. Confirm-against-live-API checklist (gates the design)

These open questions block specific design points. Each is tagged with the decision it gates.

**Reinstall (gates `gigahost_server_reinstall`):**

- Does `GET /servers/{id}` immediately reflect the new `os_id` after `POST /reinstall`, or is there propagation lag? (Affects post-Create read + drift detection.)
- Is `ReinstallResult.Reboot` ever `false`? (Determines whether Create must always poll for reboot completion.)
- Is `RootPasswd` returned on every reinstall or only first/major-version installs? (Affects whether `root_password` is reliably populated.)
- Does `ReinstallRequest.Hostname` update `srv_hostname`, or only at first deploy? (Affects the `hostname` attribute semantics.)
- Does reinstall re-inject the deploy-time `ssh_keys`? (Affects whether `ssh_keys` must stay coupled to deploy.)
- Is reinstall idempotent / safe to call repeatedly?

**Upgrade (gates `gigahost_server_upgrade` + `product_id` disposition):**

- Confirm `POST /upgrade` keeps the same server ID and IP and does not recreate.
- After upgrade, does `GET /servers/{id}` reflect a new `product_id` and updated `cores`/`ram`/`disk`, and is a reboot required for hardware to take effect?
- Is downgrade (smaller package) blocked or allowed? (Determines whether to validate direction.)
- Is applying the same package twice idempotent or an error?

**Power (gates `gigahost_server_power`):**

- Are `PowerOn`/`PowerOff` idempotent when already in the target state, or do they error? (Determines error-swallowing in Create/Update.)
- Does `GetPowerState` reflect real-time state or is it cached? (Affects drift reliability.)
- Can power ops be called during `install`/`rescue`/`suspended` states?
- Does `Reboot` transition off→on (briefly reporting off), affecting `current_power_state` reads?
- Does billing continue while powered off? (Documentation/cost guidance.)

**Snapshot (gates `gigahost_server_snapshot`):**

- Confirm `POST /snapshot` returns no `snap_id` (requires post-create `List` poll).
- Are snapshot names unique per server? (Determines whether name-match in the poll is safe, or we must disambiguate by `snap_time`.)
- Typical create-to-`completed` time? (Sizes the Create polling timeout.)
- Do snapshots survive `POST /servers/{id}/cancel`, or are they deleted with the server?
- Confirm **no restore endpoint** exists (if it does, restore is a separate imperative action, not part of this resource).
- Can a `completed` snapshot be deleted immediately, and while another is pending?
- Does `StatusSnapshot` enforce one-operation-at-a-time per server? (May require a per-server lock like `locks.go`.)

**Extra IPv4 (gates `gigahost_server_ipv4`):**

- Does `POST /ipv4` return the new IP in the body, or only a status? (Confirms the diff-the-IPs-list approach for Create.)
- Is there any release/`DELETE` endpoint, now or planned? (Determines whether Delete stays a no-op.)
- Per-server IPv4 cap? (Schema validation upper bound.)

**ISO mount (gates `gigahost_server_iso`):**

- Can `POST /isos` switch the mounted ISO directly, or is an explicit unmount required first?
- Is there a `DELETE /servers/{id}/isos/{iso_id}` or unmount endpoint? (Determines Delete behavior.)
- What does `StatusMount` mean — "mounted and will boot" vs "mount in progress"?
- Does mounting require a specific server state (powered off, install/rescue)?
- Can multiple ISOs be mounted, or does a new mount auto-unmount the previous?

**Backups (gates `gigahost_server.backups` requires-replace):**

- Confirm no `PUT/PATCH /servers/{id}/backups` exists (any in-place toggle would change this from requires-replace to in-place-update).
- Does `GET /servers/{id}` expose the current backups flag? (Enables drift detection.)
- On redeploy, is the IP preserved or reassigned? (Affects the cost/impact framing of a backups change.)

**Rescue (gates `gigahost_server.rescue` immutability):**

- Probe `PUT`/`POST /servers/{id}/rescue` and `PATCH /servers/{id}` — expect 404/405. Confirm rescue cannot be toggled at runtime.
- Confirm `StatusRescue` semantics (boot mode vs active session).

**Reverse DNS (gates existing `gigahost_server_rdns`, already correct):**

- Confirm empty-DNS clears to default and that `dns` updates cause no IP/server churn.
- Confirm whether concurrent rDNS writes on the same server need a per-server lock (like the DNS-zone `locks.go`). If yes, add a `serverLocks` analog used by all server-mutating resources.

**Server name (gates existing `gigahost_server_name`, already correct):**

- Confirm `PUT /name` is idempotent, has no side effects, and works while powered off/installing.

---

## 5. Prioritized implementation order

Ordered by user value, risk of the current design being wrong, and dependency.

1. **Confirm-live spike (section 4).** Cheap to run, gates everything. Specifically nail down: reinstall propagation + reboot behavior, upgrade ID/IP preservation, power idempotency, snapshot create-returns-no-ID, and the existence (or not) of ISO-unmount and IPv4-release endpoints. Capture answers as test fixtures.
2. **`gigahost_server` computed attributes** (`power_state`, `cores`/`ram`/`disk`, `primary_ip_id`) and **documentation correction** of `os_id`/`product_id`/`rescue`/`backups` semantics. Low risk, immediately improves correctness of the most-used resource and unblocks ergonomic wiring of the new resources.
3. **`gigahost_server_reinstall`.** Highest-value correction (OS change no longer nukes the server). Reuses the `waitForReady` polling pattern.
4. **`gigahost_server_upgrade`.** Second-highest value (resize without redeploy). Straightforward in-place + re-read.
5. **`gigahost_server_power`.** High operational value, simple verbs; depends on power idempotency answer from step 1.
6. **`gigahost_server_snapshot`.** Self-contained entity; needs the create-poll + completion-wait logic and possibly a per-server lock.
7. **`gigahost_server_iso`.** Gated on the unmount-endpoint answer; Delete semantics depend on it.
8. **`gigahost_server_ipv4`.** Lower priority due to no-op Delete (billing trap); ship with prominent docs once the release-endpoint question is answered.
9. **Per-server serialization lock** (`serverLocks`, mirroring `tfprovider/locks.go`) if step 4/6 confirm `StatusSnapshot`/concurrent mutations conflict. Retrofit into all server-mutating resources.
10. **Register all new resources** in `tfprovider/provider.go` `Resources()`, add acceptance tests following `server_acc_test.go` / `integration_acc_test.go`, and run full `go test ./...`.

Relevant existing files this design builds on: `tfprovider/server_resource.go` (the all-RequiresReplace deploy resource to correct), `tfprovider/server_rdns_resource.go` and `tfprovider/server_name_resource.go` (the anchored-mutable-resource pattern to follow), `tfprovider/locks.go` (the per-entity serialization pattern), `tfprovider/provider.go` (registration), and the client services `client/{power,reinstall,upgrades,snapshots,isos,ipmi,servers}.go`.

---

## 6. Live findings (grand-tour, 2026-06-09)

Run against a real deployed server (`go test -tags e2e -run TestGrandTour`):

- **Reinstall preserves identity (confirmed).** After `POST /servers/{id}/reinstall`,
  the `server_id` is stable and the primary IP is unchanged
  (`185.125.169.54` → same). This validates modelling an OS change as an
  **in-place** `gigahost_server_reinstall`, not a destroy+create.
- **Rename is in-place (confirmed).** `PUT /servers/{id}/name` updates the label
  with no rebuild.
- **Definitive capability matrix for the hourly KVM VPS** (probed on a fully
  `all_ready` server, so this is not a not-yet-ready artefact):

  | Capability                                          | Result                             | Disposition                                                                                                                  |
  | --------------------------------------------------- | ---------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
  | reinstall (`POST /reinstall`)                       | ✅ works, keeps ID+IP              | folded into `gigahost_server.os_id` (in-place update) + matrix-tested                                                        |
  | **snapshot** (`POST /snapshot`, `List`, `Delete`)   | ✅ **works**                       | `gigahost_server_snapshot` shipped + acc-tested                                                                              |
  | power state **read** (`GET /powerstate`)            | ✅ 200                             | exposed via `gigahost_server.status`                                                                                         |
  | power on/off (`GET/POST/PUT /power/off`)            | ❌ 405/400                         | not supported on hourly VPS — no resource                                                                                    |
  | reboot (`GET /reboot`)                              | ✅ 200 (re-probed 2026-08-25, B23) | works; CLI and client only, no resource                                                                                      |
  | extra IPv4 (`POST /ipv4`, all body forms)           | ❌ 400                             | `gigahost_server_ipv4` shipped (correct per docs), but the order is rejected on this account — see B19                       |
  | resize/upgrade `Apply` (`POST /upgrade`, all forms) | ❌ 400/404                         | not applicable to hourly (server `product_id=0`); `Upgrades.List` decode fixed + unit-tested, no resource — see upstream B10 |

  The earlier "snapshot doesn't appear" was a **client-test bug**: the user's name is
  stored as `snap_display_name`, but the test matched the random `snap_name`. Fixed.
  For power/reboot/ipv4/upgrade-apply the client matches the documented contract, but
  the hourly cloud VPS rejects them (capability limitation, like the api-key 403s).
  They remain available via the Go client/CLI for server types that support them;
  no Terraform resources are shipped because they would be broken for the flagship
  hourly-cloud user base. Re-evaluate against a dedicated/managed server if one
  becomes available.

## 7. Gated acceptance tests (2026-06-11)

The precondition-heavy resources have env-gated acceptance tests in
`tfprovider/gated_acc_test.go`; each skips with a message naming its variable
(see the README Development section for the full table). Live probe finding:
enabling DNSSEC on an unregistered NATIVE zone fails with
`403 DNSSEC can only be enabled for registered domains`, so `gigahost_dns_dnssec`
and `gigahost_dns_nameservers` gate on `GIGAHOST_TEST_REGISTERED_ZONE` rather
than creating a throwaway zone.

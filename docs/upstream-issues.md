# Upstream issues & test skips

Living record of everything we skip or work around against the gigahost.no API,
so it can be reported upstream. Two buckets:

- **A. Blocked by our test token / account** — we skip these tests; likely not a
  bug, but worth confirming whether the restriction is intended.
- **B. API contract / behaviour** — quirks worth fixing or documenting upstream.

Verified against `https://api.gigahost.no/api/v0` with the test-account API-key
token. Each entry carries its own verification date; the most recent sweep was
2026-08-25. Keep this updated as new skips appear.

Legend: ✅ works · ⛔ 403 forbidden · ⏭️ test skipped · 🚧 deferred.

## A. Blocked by token / account

### A1. API-key management — ⛔ entire surface

- `GET /account/apikeys` → 403 "You do not have permission for this operation."
- `POST /account/apikeys` → 403
- Implies `PUT/DELETE /account/apikeys/{id}`, `…/rotate`, `…/log` are also 403.
- **Impact:** e2e `TestAPIKeyLifecycle` skips; the `gigahost_account_api_key`
  Terraform acc test is not runnable with this token.
- **Likely cause:** API-key tokens lack the `account` scope and so cannot manage
  API keys. Please confirm whether this is intended, and whether a key can be
  granted `account` to self-manage.

### A2. Legacy `/my/*` — ⛔

- `GET /my/account` → 403
- `GET /my/invoices` → 403
- **Workaround:** credit and invoices are read from `GET /billing` instead, which
  works with this token. `/my/invoices` is effectively unusable for API-key auth.

### A3. Other forbidden probes — ⛔

- `GET /account/activity` → 403
- `GET /credit`, `GET /balance` → 403

### A4. Partner / reseller features — account is not a partner (`cust_partner=false`)

- `/account/clients*` (sub-clients) and `/account/dpa*` (DPA) cannot be exercised.
- 🚧 Deferred in our implementation; untestable on this account.

## B. API contract / behaviour

### B1. Inconsistent JSON types in `GET /deploy/servers`

- `group_id`, `product_id`, `price_id` are JSON **numbers**, but `region_id` is a
  **string**; `region_ids` is an array of **numbers**.
- `vm_cores`, `vm_memory`, `vm_storage`, `vm_bw` are numeric **strings**.
- Mixed typing forces lenient client decoding (we coerce all IDs to strings).
- **Suggestion:** use consistent string IDs (or consistent numbers) throughout.

### B2. `price_id` required to deploy but only discoverable via the catalog

- `POST /deploy/servers` requires `price_id`, which is only present per-product in
  `GET /deploy/servers`. **Suggestion:** document the dependency, or accept
  `product_id`/`product_hash` alone.

### B3. `vm_memory` unit is GB, not MB

- e.g. `"4"` for a 4 GB plan. The field name implies bytes/MB; please make the
  unit explicit in docs.

### B10. Server upgrade docs are stale; Apply contract unclear

- `GET /servers/{id}/upgrade` works (resize _is_ supported on hourly VPS) but the
  response shape differs from the docs: it returns `product_id`, `product_name`,
  `product_vm_cores/memory/storage/bw`, `rate_monthly`, `currency_code` — and
  **no `pkg_id`**, `pkg_name`, `pkg_cores`, etc. The target is identified by
  `product_id`.
- The documented `POST /servers/{id}/upgrade` body field `pkg_id` is rejected, and
  so are every inferred form — `{pkg_id|product_id|upgrade_id|price_id}` and combos
  — returning 400/404 even on a **fully provisioned** server (waited for
  `all_ready`, ~4 min). Notably the hourly server reports `product_id: 0` in
  `GET /servers/{id}`, so the resize flow appears to be **not applicable to hourly
  cloud VPS** (it likely targets monthly/contract servers). **Suggestion:** update
  the upgrade docs to the live list shape and document the resize request body +
  which server/billing types it applies to.

### B8. Concurrent DNS record writes to one zone return 500

- Creating/deleting several records in the same zone _concurrently_ (as a parallel
  `terraform apply`/`destroy` does) intermittently returns
  500 "Unable to delete record: Unknown server error". The identical operations
  run **sequentially** all succeed. The API appears to serialize poorly on
  per-zone record mutations. We work around it by serializing record writes
  per-zone in the provider; a server-side fix would let clients parallelise.

### B9. DNS record IDs are content hashes

- The record ID (e.g. `0707ba2a68d3510412abdd6d277c264d`) is derived from the
  record content, so updating a record's value changes its ID. Not a bug, but
  worth documenting; clients must re-resolve the ID after an update.

### B6. Server termination endpoint is undocumented

- `POST /servers/{id}/cancel` → 200 "Server has been cancelled." is the only
  programmatic way to terminate a server (incl. hourly servers from
  `POST /deploy/servers`) and stop billing; afterwards the server leaves
  `GET /servers`. It is **not in the API documentation**.
- `DELETE /servers/{id}` and `DELETE /deploy/servers/{id}` → 405; order-level
  cancel (`/order/{id}/cancel`) → 403. **Suggestion:** document
  `POST /servers/{id}/cancel`.

### B7. `POST /deploy/servers` returns `order_ids` as JSON numbers

- The create response encodes `order_ids` (and `order_numbers`) as numbers, while
  the same IDs are strings elsewhere (`/billing`). We coerce to strings. Same
  class as B1.

### B5. `POST /dns/zones` returns an empty `data` array (no `zone_id`)

- Create succeeds (201, "Zone created successfully.") but `data` is `[]`, so the
  caller cannot learn the new zone's ID from the response.
- **Workaround:** after create, `GET /dns/zones` and match by name to resolve the
  ID (our `CreateZone` now does this). **Suggestion:** return the new `zone_id` in
  the create response `data`.

### B4. `POST /account/apikeys` response omits the key ID (unverified — see A1)

- Our client model maps `secret`/`prefix`/`label` but no `key_id`, implying the
  create response does not return the ID, forcing a follow-up
  `GET /account/apikeys` to resolve it by prefix. **Suggestion:** return `key_id`
  on create. Not runtime-verified because the surface is 403 (A1).

### B11. `GET /deploy/isos` wraps the list in an object

- The docs imply a bare list, but the live response is
  `data: {"isos": [...]}` — unlike `GET /servers/{id}/isos`, which returns a bare
  array. Caught live by the CLI smoke suite (2026-06-11); the client decoded the
  documented shape and failed on the real one. **Suggestion:** make the two ISO
  list endpoints consistent.

### B12. `GET /servers` carries no deploy-order linkage

- A server object exposes `srv_*`, `os`, and `ips`, but no `order_id`/`order`
  field, so there is no way to find a server in the list by the deploy order
  that created it. `GET /deploy/status` is the only bridge from order to server
  id, and it only lists an order while the server is provisioning. **Impact:**
  to adopt a server that appears late (after a failed create wait), the provider
  must capture the server id from `/deploy/status` early and re-poll it by order;
  once provisioning finishes and the order drops from status, a server that was
  never captured cannot be reattached by order. **Suggestion:** include the
  order id on the `/servers` record.

### B13. `GET /deploy/status` has no failure status and drops completed orders

- The status view lists an order only while its server is provisioning and has
  no terminal "failed"/"error" status — an order simply vanishes when the server
  finishes or when provisioning fails. **Impact:** completion and failure must
  both be inferred from the durable `/servers` record rather than signalled.
  The provider polls `/servers` as the completion source and treats a
  previously-seen server that disappears from both views as a failed deploy.
  **Suggestion:** expose a terminal status (and keep finished orders queryable
  briefly).

### B14. `GET /servers` transiently omits live servers

- A live, billed server is intermittently absent from the list (and 404s on
  `GET /servers/{id}`) for tens of seconds, then reappears (observed live).
  **Impact:** a naive refresh would delete the resource from Terraform state and
  the next apply would redeploy/double-bill. The provider confirms absence
  across repeated reads before treating a server as gone. **Suggestion:** make
  the server read strongly consistent.

### B15. `GET /servers` omits the IPv6 address reported at deploy time

- The IPv6 address returned in `/deploy/status` is not always present on the
  later `/servers` record. **Impact:** refresh would null a known IPv6 address.
  The provider keeps the deploy-time address when the list omits it.
  **Suggestion:** populate IPv6 consistently on the server record.

### B16. Cancelling a nonexistent server returns 400, not 404

- `POST /servers/{id}/cancel` for a server that died during provisioning is
  refused with HTTP 400 rather than 404. **Impact:** `terraform destroy` of a
  server that never finished provisioning would fail forever. The provider
  follows a refused cancellation with an absence check and clears a confirmed-
  gone server from state. **Suggestion:** return 404 for an unknown/absent
  server id.

### B17. `srv_ram` unit confirmed GB (`GET /servers`)

- The `/servers` `srv_ram` field is in GB (docs example `"2"`; catalog product
  names like "4GB" and catalog `ram_gb` agree — see B3). Verified 2026-06-12;
  the test account had no live servers to cross-check directly, so the provider
  also normalizes defensively in case any product type reports MB.
  **Suggestion:** document the unit explicitly.

### B18. No endpoint to release an ordered IPv4

- `POST /servers/{id}/ipv4` orders an additional IPv4 (`ip_type` `l2`/`l3`), but
  there is no corresponding release/unassign/delete endpoint — the only DELETE
  under `/servers` is for snapshots. The order response also does not return the
  new IP, so the assigned address must be discovered by diffing `GET /servers`
  before and after. **Impact:** `gigahost_server_ipv4` is create-only; its
  `deletion_policy` defaults to dropping the resource from state with a warning,
  because `terraform destroy` cannot free the address (it remains allocated and
  billed until released in the control panel, or until the whole server is
  cancelled). **Suggestion:** add `DELETE /servers/{id}/ipv4/{ip_id}` (or return
  the new IP from the order call) so additional IPs can be fully managed.

### B19. `POST /servers/{id}/ipv4` rejects the documented body

- The docs specify a single parameter `ip_type` with value `"l2"` or `"l3"`, but
  the live endpoint returns `400 "Invalid data received."` for every documented
  and reasonable payload shape — verified 2026-06-12 against a freshly deployed,
  fully-installed server (id 17643) with: JSON `{"ip_type":"l3"}`,
  `{"ip_type":"l2"}`, `{"ip_type":"l3","qty":1}`, `{"ip_type":"l3","amount":1}`,
  form-encoded `ip_type=l3`, and query `?ip_type=l3`. The project's own CLI
  (`gigahost servers ip-order`) fails identically, so it is not a provider-side
  encoding bug. Re-confirmed 2026-08-25 on server 18397. **Impact:** `gigahost_server_ipv4` cannot order an IP on this
  account; it surfaces the API error cleanly but cannot succeed until the real
  contract is known. This may be an account-level entitlement (like the partner
  features in A4) rather than a pure contract bug. **Suggestion:** document the
  exact request body (and any required entitlement) for ordering an IP, and
  return a more specific error than "Invalid data received".

### B20. `GET /billing` returns money fields as numbers or strings

- `inv_total`, `inv_vat` and `inv_total_vat` are quoted strings on some
  invoices and bare JSON numbers on others within a single response.
  **Impact:** a strictly-typed decoder fails on the whole payload; the client
  decodes all three leniently. Verified 2026-08-25. **Suggestion:** pick one
  representation — ideally a JSON number — and use it consistently.

### B21. `DELETE /dns/zones/{id}/records/{id}` ignores the record ID

- The record ID in the URL path is not used. The endpoint matches on the
  `name` and `type` query parameters, so a delete addressed at one record
  removes **every record sharing that name and type**. Verified 2026-08-25:
  three `multi A` records (192.0.2.1/2/3), a delete aimed at 192.0.2.2 alone,
  and all three disappeared.
- Passing `value` as well narrows it to the single record — but that parameter
  is undocumented, and the value it compares against is the backend's stored
  content, not the value `GET .../records` reports. They differ for **MX**: the
  list splits the priority into `record_priority` and drops the trailing dot,
  while the delete matcher wants both back, so `mail.example.com` must be sent
  as `10 mail.example.com.`. A, AAAA, CNAME, TXT, NS, CAA and SRV all match the
  value as listed.
- **Impact:** silent data loss. Destroying one `gigahost_dns_record` in an
  RRset would have taken its siblings with it. The client now requires
  `Value` on `DeleteRecordRequest` and renders the MX form itself.
  **Suggestion:** honour the record ID in the path, and document `value`.

### B22. Catalog `specs.ram_gb` disagrees with the product name and addons

- Several dedicated products report a `specs.ram_gb` that contradicts both
  their own `product_name` and their `addons` "Minne" entry. Verified
  2026-08-25:

  | product       | `product_name`                   | `addons`           | `specs.ram_gb` |
  | ------------- | -------------------------------- | ------------------ | -------------- |
  | 4879          | Intro - AMD Ryzen 3600 **64GB**  | 64GB DDR4          | **32**         |
  | (Intro i7)    | Intro - Intel Core i7 **16GB**   | 16GB DDR3          | **32**         |
  | (Hobby 5600)  | Hobby - AMD Ryzen 5600 **64GB**  | 32 GB              | **32**         |
  | (Elite 7313P) | Elite - AMD EPYC 7313P **256GB** | 256GB DDR4 ECC/REG | **0**          |

- **Impact:** products 4878 and 4879 both derive the size slug
  `ryzen-5-3600-32gb-500gb`, so slug-based selection cannot address the 64GB
  machine at all — `FindProduct` reports an ambiguous match and refuses,
  which is safe but leaves the product unreachable.
  **Suggestion:** make `specs` authoritative and consistent with the product
  name and addons.

### B23. `GET /servers/{id}/power/{on,off}` returns 405 on a KVM VPS

- The docs list all three power endpoints as GET: "`GET /servers/{id}/reboot`",
  "`GET /servers/{id}/power/on`", "`GET /servers/{id}/power/off`". Against a
  freshly deployed KVM Value VPS (server 18394, 2026-08-25) `reboot` answers
  `200 OK` as documented, but both `power/on` and `power/off` answer
  `405 Method not allowed`. POST and PUT on the same paths answer a bodyless
  `400 Bad Request`, so the route exists but no verb succeeds.
- If power on/off is simply not supported for virtual servers ("if supported"
  in the docs), 405 is the wrong signal — it reports the _method_ as wrong
  rather than the operation as unavailable, so a client cannot tell a routing
  bug from an unsupported product. Not retested on dedicated hardware.
  **Impact:** `client.Servers.PowerOn`/`PowerOff` and `gigahost servers power
on|off` fail on a VM; `reboot` works. This is why no Terraform resource
  exposes power at all — see `terraform-lifecycle.md` §3.3, which specifies one
  but is unimplemented. **Suggestion:** either accept GET as documented, or
  return a 4xx that names the reason.

## C. Test-skip ledger

| Item                                | Layer      | Status                 | Reason |
| ----------------------------------- | ---------- | ---------------------- | ------ |
| `TestAPIKeyLifecycle`               | e2e (Go)   | ⏭️ skip                | A1     |
| `gigahost_account_api_key` acc test | Terraform  | ⏭️ blocked             | A1     |
| invoices via `/my/invoices`         | client/CLI | replaced by `/billing` | A2     |
| sub-clients, DPA                    | all        | 🚧 deferred            | A4     |
| `TestSlugContractCatalog`           | e2e (Go)   | ⚠️ logs collision      | B22    |
| `PowerOn` / `PowerOff` on a VM      | client/CLI | ❌ 405                 | B23    |
| `TestAccServerIPv4_deploy`          | Terraform  | ⏭️ gated               | B19    |

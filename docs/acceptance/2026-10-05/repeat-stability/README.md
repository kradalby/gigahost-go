Three fresh-install Terraform acceptance rounds completed through gigahost-go on 2026-10-05. The matrix requires a running VM and successful root SSH executing `hostname`. All 42 cells used `performance / 2c-4gb-40gb / sfj / amd64`; key cases used account key **2954**, and password cases omitted keys. Every cell used its own Terraform state and fresh VM.

| OS | Key: passes / trials | Password: passes / trials |
|---|---:|---:|
| Debian 12 | 3/3 | 3/3 |
| Debian 13 | 3/3 | 3/3 |
| Ubuntu 24.04 | 3/3 | 3/3 |
| Ubuntu 26.04 | 3/3 | 3/3 |
| AlmaLinux 10 | 0/3 | 0/3 |
| RockyLinux 9 | 0/3 | 0/3 |
| Fedora 43 Server | 0/3 | 0/3 |

**Startup timing** separates allocation, API readiness, SSH verification and cancellation. These are observed transitions from polling, not exact guest boot timestamps. Cleanup is linked to the initial VM ID; the first matrix recorder used legacy stage events for its single deployment per case, identified by cleanup_timing_source. SSH checks retry for three minutes; a complete attempt is bounded to 15 seconds. The matrix uses a test-only 45-minute provisioning allowance; the provider default remains 15 minutes.

| Round | Allocation seconds: min–max | API-ready seconds: min–max | Successful SSH check seconds: min–max | API readiness >15 minutes |
|---|---:|---:|---:|---:|
| 1 | 20.2–980.2 | 160.2–2370.2 | 18.1–75.5 | 4 |
| 2 | 20.2–60.2 | 160.2–400.2 | 14.1–80.7 | 0 |
| 3 | 20.2–60.2 | 160.2–400.2 | 25.8–76.6 | 0 |

First-round orders **36004, 36005 and 36006** waited roughly 16 minutes for allocation and 38.5–39.5 minutes for API readiness. Their installer `waiting` steps lasted roughly 16.5–17.5 minutes after allocation; `stalled` and `failed` remained false. Later orders could allocate sooner. These delays can exceed the default provider create timeout even when installation eventually succeeds. The API reports `node_id: 0`, so this evidence cannot associate variation with a physical host.

**Other acceptance coverage** includes DNS zone/record/redirect CRUD and imports, account/catalog data sources, SSH key management, deploy/import/root SSH, disposable-server metadata tests and CLI-created server import. `test.headscale.no` was created, populated with A/AAAA/TXT records, checked for a no-op plan and removed in each full round. See [coverage.json](coverage.json) for actual logical test outcomes, excluding duplicate fixture skips. The additional standalone IPv4 and explicit-hostname cases ran in the third full suite and two focused rounds; they were not enabled in the first two full suites.

| Case | Pass | Fail | Scope of result |
|---|---:|---:|---|
| `TestAccDNSRecord_multipleInZone` | 3 | 0 | Exact test.headscale.no zone and A/AAAA/TXT records |
| `TestAccServer_deployAndSSH` | 3 | 0 | Deploy, root SSH, import and cancellation |
| `TestAccImportOOB_Server` | 3 | 0 | CLI creation, Terraform adoption and cancellation |
| `TestAccServerName_basic` | 0 | 3 | Provider name mapping and cleanup fail |
| `TestAccImportOOB_ServerName` | 0 | 3 | Provider imports the wrong name field |
| `TestAccServerRDNS_IPv4` | 0 | 3 | HTTP 400; validation contract unresolved |
| `TestAccServerIPv4` | 0 | 3 | Existing disposable VM, documented l3 request |
| `TestAccServerIPv4_deploy` | 0 | 3 | Fresh disposable VM, documented l3 request |
| `TestAccImportOOB_ServerSnapshot` | 1 | 4 | Three completion failures and one earlier premature delete check |
| `TestAccServerSnapshot` | 2 | 1 | Snapshot appearance/import; does not prove backup completion or restore |
| `TestAccServerOSChangeMatrix` | 0 | 3 | Existing default request; initial timeout or reinstall failure |
| `TestAccServerOSChangeWithHostname` | 0 | 3 | Explicit documented hostname; detailed exchanges retained |

API-key management was skipped because this API-key token cannot manage other API keys, as [Gigahost documents](https://gigahost.no/en/api-dokumentasjon). BGP mutations, registered-domain delegation/DNSSEC, external DS and reverse-zone tests lack owned prerequisites and remain skipped. Top-level existing-server tests also skip without a global VM ID; the repeat fixture runs their equivalents against its own disposable VM.

**Our code and test failures** are distinct from upstream observations. `PUT /servers/{id}/name` succeeds and `srv_name` changes. Our name resource reads `srv_label`, which stays at its original value; deletion uses empty `srv_hostname` and gets a valid rejection. The default reinstall test also submits an empty hostname although the API requires one. Neither belongs in a report claiming a valid name or reinstall request failed. Explicit-hostname trials provide a separate comparison.

Snapshot 7057 reached `completed` in about 50 seconds in round two; its import/convergence succeeded. The subtest still failed because its immediate destroy check saw `deleting` after a successful asynchronous DELETE. Later trials use a bounded condition check for actual absence. That earlier delete assertion failure is a test timing issue, separate from snapshot 7056 disappearing and snapshot 7055 missing the completion deadline.

With that deletion check corrected, snapshot completion/import/delete passed **1/3** trials. Snapshots **7061** and **7062** disappeared from their lists after about 60 seconds, before any DELETE, and remained absent through ten-minute completion deadlines. Their VMs were still running. Supplied-hostname reinstall failed **3/3** at the 60-second HTTP response deadline; follow-up reads on **18930** and **18933** showed the requested OS change and installation in progress despite the caller receiving an error. The report separates those side effects from a request that never ran.

**Debugging resources retained on 2026-10-05**: first-round matrix failures 18866 (Alma password), 18867 (Rocky key), 18870 (Rocky password), 18871 (Alma key), 18878 (Fedora password), 18879 (Fedora key), with key 2954. First-round snapshot failure VM 18884 was also preserved. Earlier debugging VMs 18846, 18848, 18849, 18852, 18853, 18854 and 18858, and key 2952, were protected during the experiment. Repeat-round VMs were cancelled after evidence collection; [resource-audit.json](resource-audit.json) records their status at the end of the experiment. Initial cancellation acknowledgements are HTTP 200; later negative probes check that a VM cannot be cancelled twice.

**Cleanup on 2026-10-09**: at the user's request, all 14 remaining running debugging VMs listed above were cancelled through gigahost-go. The active server list became empty at 08:31:37 UTC, approximately two minutes after cancellation, and a separate CLI query confirmed it. [Cleanup evidence](cleanup-2026-10-09.json) preserves the inventory, cancellation timestamps and removal observations. Saved acceptance artifacts and SSH keys were preserved; these VMs are no longer available for console investigation.

**Comparison limits**: the runtime baseline is commit `c12196b89badfacbaa602059261310f6c73cbf89` with uncommitted acceptance-only changes. Production client/provider code did not change. The earlier handover matrix ran on `9fe0170` plus working changes and is a different baseline. An initial malformed-HCL experiment was excluded. Two extra disposable VMs overlapped early round-two matrix startup from 14:21:24 to 14:27:13 UTC; [load-notes.json](load-notes.json) records that extra load. Suite recording was added after round one; complete API exchanges are available from round two. Three full suite rounds ran; two focused rounds additionally repeat the hostname, bounded snapshot and fresh IPv4 cases. Source hashes and instrumentation changes are preserved under [sources/](sources/).

The [Gigahost report draft](gigahost-report.md) identifies observations and concrete questions. [matrix.tsv](matrix.tsv) preserves VM/order IDs, timestamps and stage durations; [install-progress.tsv](install-progress.tsv) preserves observed step/progress/counter/flag history. [mutations.jsonl](mutations.jsonl) records request/response evidence. Per-case artifacts contain Terraform inputs, deployment JSON and installer evidence. Complete recorded API event histories are compressed as `events.jsonl.gz` (read with `gzip -dc`). Private credentials, private keys and raw private test logs are excluded. Nothing was sent to Gigahost.

Offline race checks, lint, formatting, timing reconciliation and export checks passed; [verification.json](verification.json) records their scope. No production client, CLI or provider changes were made. Git has no unresolved conflicts; these acceptance changes and results are uncommitted and unpushed.

Repeat with:

```sh
GIGAHOST_TEST_DEPLOY_TYPE=performance GIGAHOST_TEST_DEPLOY_SIZE=2c-4gb-40gb \
GIGAHOST_TEST_ZONE_APEX=test.headscale.no GIGAHOST_TEST_ZONE_NAME=test.headscale.no \
direnv exec . nix run .#test-acc-repeat -- --rounds 3 --parallel 4
```

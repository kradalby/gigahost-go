# Fresh-install acceptance evidence — 2026-10-05

Inputs: performance / 2c-4gb-40gb / sfj / amd64. Shared account SSH key 2952;
password deployments omit SSH keys. Acceptance requires root SSH to execute
`hostname`. No reinstalls were requested.

| OS               | Key                                                     | Password                                           |
| ---------------- | ------------------------------------------------------- | -------------------------------------------------- |
| Debian 12        | PASS 18845                                              | PASS 18851                                         |
| Debian 13        | PASS 18857                                              | Timed out before allocation; later VM 18858 booted |
| Ubuntu 24.04     | PASS 18850                                              | PASS 18856                                         |
| Ubuntu 26.04     | PASS 18855                                              | PASS 18847                                         |
| AlmaLinux 10     | Authentication rejected 18852                           | Authentication rejected 18853                      |
| RockyLinux 9     | Connection timeout 18854; later authentication rejected | Authentication rejected 18846                      |
| Fedora 43 Server | Authentication rejected 18849                           | Authentication rejected 18848                      |

Seven passed, six failed SSH, one deployment timed out. The six SSH failures
and late Debian VM 18858 are retained. Passed VMs were cancelled. These are
observations from one matrix, not a guarantee of consistent OS support.

`matrix.tsv` adds Go subtest elapsed seconds. This includes Terraform setup,
provisioning, SSH retries and cleanup; separate allocation/install/SSH phase
timing is not implemented yet. `test-events.jsonl` preserves test lifecycle
metadata for both batches, without raw log output.

Each `artifacts/<OS>/<auth>` directory contains the deployment JSON and test
HCL. Installer details, server snapshots, final errors and later SSH rechecks
are included where captured. JSON credentials and server keys are redacted.
Private SSH keys and root passwords are excluded. Evidence is API input and
API-provided post-install commands; the full autoinstall/kickstart file is not
exposed by the API.

The first batch's recorder did not decode compressed responses. A separate
observer recovered installer/server details for its three retained failures
and Ubuntu cases. Invalid recorder placeholders are excluded. The resumed
batch used the corrected recorder. Debian 12 key completed before installer
observation began, so that case lacks installer details.

Eight first-batch creates were billing-blocked. After the user resolved
billing, only those eight were rerun with the original key. Their results
supersede the blocked attempts. `earlier-matrix.tsv` records an older run
before fresh-install password capture was fixed; its VMs were all cleaned up.

Order 35991 was submitted at 10:51:25 UTC and still had no VM at 11:09:38 UTC.
The provider's 15-minute create timeout had already expired. User console
capture subsequently showed Debian VM 18858 installing; API observation at
11:43:28 UTC confirmed it ready. Its install-time password was missed, so no
late password SSH acceptance was possible. The original diagnostic claimed
Server 0 because the API's unassigned sentinel was not normalized; that
provider bug is now fixed. Original test failure and late observation remain
separate artifacts.

The user reported all other failed VMs were at login screens. At 11:52 UTC,
all six retained VMs still rejected the original credentials through SSH;
Rocky key had changed from connection timeout to authentication rejection.
No console-side SSH configuration diagnosis has been completed.

See the repository README for matrix setup and rerun instructions. Raw local
runs were named
`deploy-matrix-20261005T101843Z-debug` and
`deploy-matrix-20261005T104209Z-resume` under `.direnv/` on the source host.

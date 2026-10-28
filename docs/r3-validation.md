# R3 candidate validation

Run date: 2026-10-28. Module minimum remains **Go 1.26.0**. No library runtime
code, public signature or dependency changed in R3. One missing independent
`generic_nack` profile test was added to `codec/layout_test.go`.

This record distinguishes local candidate evidence from GitHub-hosted CI. The
implementation is still an uncommitted working tree. It was not pushed, tagged
or published, so there is no remote CI result for this candidate. R4 release
readiness and the owner's release instruction remain outstanding.

## Candidate identity and execution scope

- Initial repository HEAD: `161875d266d8eed7a0be5906d60e0adff86fcc8e`; this initial
  commit does **not** contain the implementation being tested.
- [Final source manifest](validation/r3-source.sha256): 87 Go/module/license/CI/
  ignore files. SHA-256 of the manifest:
  `16e5b842bbe06edab23205956cb0a4f837d7c033c5ab164e57af8808e74a7383`.
- The only change after campaign startup was `codec/layout_test.go`. All runtime
  sources remained byte-identical. Both toolchains' build/vet/full race checks
  and the affected Codec fuzz runs were repeated after that test addition.
  The root soak and legacy harness do not compile dependency package test files;
  their compiled source inputs were unchanged throughout the run.
- Environment: Linux/amd64 under WSL2, AMD Ryzen 7 6800H. Installed Go 1.26.4 is
  the baseline validation toolchain; cached Go 1.27.2 supplies the second existing
  CI-matrix series locally. This compatibility check does not upgrade go.mod or
  resume the deferred Go-baseline upgrade.
- Fuzz, ordinary checks and resource testing overlapped. Performance benchmarks
  run separately after those task processes have ended. No external SMSC is used.

## Ordinary checks and independent bytes

Formatting, build, vet and the full race suite pass with both Go 1.26.4 and
Go 1.27.2. The final source includes the new Generic NACK test, also repeated
20 times under race detection. All examples compile; the R2 execution/loopback
record remains separate historical evidence, not a newly executed R3 example run.

Existing evidence was reused rather than building a second test architecture:

| Contract | Candidate tests |
| --- | --- |
| Every advertised mandatory command layout in each supported profile | `TestIndependentMandatoryLayouts`, multi/notification literal vectors, new `TestIndependentGenericNACKProfiles` |
| Malformed, truncated, negative and boundary fields | Codec framing/ownership, multi, semantics, time, 53-tag inventory, direction and transaction suites |
| Owned decoding and original response context | RV20 append-isolation regressions; RV21 ACK policy; R1 response TLV and writer-join tests |
| Real Session traffic without a peer Codec | `TestLifecycleIndependentTCPVersions` (3.3/3.4/5.0), independent multi/data/notification/outbind peers |
| Multipart outcomes, capacity and shutdown | Existing R1/R2 messaging, receiver, lifecycle and resource tests |

The added NACK vectors are literal `80000000` headers with nonzero status 8,
sequence 0 or `7fffffff`; invalid OK status, excessive sequence and truncation
are rejected. The same literal congestion TLV is accepted only in 5.0. This
closes the previously missing direct 3.3 NACK sample without changing Codec.
Shared-Codec exchanges are supplemental evidence, not the sole wire oracle.

Raw ordinary/matrix output: [R3 checks](validation/r3-checks.txt).

## Timed fuzz campaign and the failed budget-exit run

Every existing target has a successful **30-second, two-worker** run on Go
1.26.4, with `-run=^$ -timeout=90s`. Existing seeds and the local generated cache
were retained; cache baselines and execution counts are in the logs. No new
crashing input or library assertion failure was produced. The 11 targets are:

`FuzzDecode`, `FuzzEncodeSM`, `FuzzSubmitMulti`, `FuzzStandardTLVSection`,
`FuzzSemanticTimes`, `FuzzDataDirection`, `FuzzDataResponse`, `FuzzUDH`,
`FuzzAdditionalUDH`, `FuzzPrepareVersionEncoding`, and `FuzzCharsets`.

Go 1.27.2 additionally passes the existing CI-equivalent five-second/two-worker
run for every target. The 1.26 campaign satisfies the longer local R3 time
budget; these are not claims that GitHub Actions ran on this working tree.

**One original Go 1.26.4 run failed and is retained.** FuzzSubmitMulti completed
403,236 executions and then returned `context deadline exceeded` at 30.09s,
without a failing input or test assertion. This was the fuzz budget, not the
90-second whole-test timeout. The campaign stopped at that failure; remaining
targets were subsequently run separately. A debug 30-second rerun passed, and
the final unmodified-toolchain 30-second run passed at 468,969 executions.
Neither successful rerun erases the failed run.

### Diagnostic attribution

A temporary Go `-overlay` added logging to the installed coordinator's existing
cancellation comparison; it did not change the decision or patch the installed
toolchain/library. Ten explicit five-second diagnostic runs produced nine passes
and then a failure with this captured state at the budget-exit branch:

```text
incoming=context deadline exceeded parent=context deadline exceeded child=<nil>
```

The Go 1.26.4 coordinator compares the incoming deadline with the worker child
context's error. The parent can finish before cancellation reaches that child,
so this branch retains a normal budget expiry as a test failure. This diagnostic
reproduces an actual coordinator failure, beyond R1's standalone context-window
probe. The original uninstrumented run did not record that internal state, so
its attribution is based on the matching reproduced failure, not an invented
trace of the original execution.

The upstream report is [Go issue #75804](https://github.com/golang/go/issues/75804).
The locally cached Go 1.27.2 coordinator checks the parent error as well and
references that issue. Installed 1.26.4 and cached 1.26.5 coordinator sources
have the same hash; no unverified claim of a 1.26 patch-level fix is made.

Classification: reproduced **toolchain fuzz shutdown defect**, no demonstrated
smppkit input defect. No automatic retry, stderr filter, assertion weakening,
Go-source patch or CI error suppression was added. Unpatched time-budgeted
Go 1.26 CI can still hit this defect; future failures must retain their logs and
be checked, not blanket-dismissed because their error text matches.

[Full timed fuzz records](validation/r3-fuzz.txt) retain the initial failure and
successful runs. [Diagnostic record](validation/r3-fuzz-diagnostic.txt) includes
commands, the logging diff, source hashes and the failing diagnostic's tail;
coverage-minimization debug noise is omitted as labelled. R1's earlier failure
remains a historical uninstrumented observation, not retroactively a pass.

## Resource and legacy-peer checks

The existing `SMPPKIT_SOAK_DURATION=10m` workload uses race detection, a 12-minute
test timeout and the finite budgets in [local validation](local-validation.md).
It checks every retired connection's maps, queues and admission slots, and
every receiver cycle's recovery. Snapshot metrics count Session read/write/
expiry goroutines, not all process goroutines; heap is post-GC, not RSS or
race-detector shadow memory. Warmup is excluded from reported timed batch counts.
See [raw resource output](validation/r3-soak.txt) for final totals and snapshots.

The run passes after **10m0.008s**, completing 27,944 timed batches:

| Observation | Result, excluding warmup |
| --- | ---: |
| TCP connection pairs, each fully joined | 111,776 |
| Successful submit_sm + deliver_sm requests | 7,153,664 |
| Notifications / control requests | 894,208 / 1,788,416 |
| Receiver completion / incomplete-group expiry checks | 223,552 / 447,104 |
| Expected capacity failures | 7,349,272 |
| Post-GC HeapAlloc, baseline → final | 554,232 → 988,912 bytes |
| Post-GC HeapInuse, baseline → final | 1,900,544 → 2,408,448 bytes |
| Post-GC HeapObjects, baseline → final | 1,268 → 2,685 |
| Session loops / Linux FDs, every sample | 0 / 7 |

Capacity counts are derived from the passed per-batch workload assertions:
256 group rejections, four completion-queue rejections and three expiry-event
backpressure returns per batch. Completion/expiry counts are likewise derived,
not additional telemetry. The first timed heap sample was 960,504 bytes; later
samples stayed near 0.97–0.99 MB rather than growing with completed connections.
Every connection/receiver cycle also passed exact retained-state checks. This
supports bounded behavior for this workload, not a production throughput claim.

The read-only legacy simulator harness passes with Go 1.26.4 and dependency
downloads disabled. Both its positive exchange and intentional bind-sequence
mismatch test pass. Git status and SHA-256 hashes of all 95 tracked/nonignored
reference files are unchanged. Its scope remains **3.4 TRX UDH only**, with
missing TLV advertisement and fresh-process bind-sequence limitations. This is
not 3.3/5.0/TLV or external-peer certification. No old review/regression material
was edited or rerun. [Raw legacy output](validation/r3-legacy.txt).

## Benchmark and remaining gate

Candidate benchmark results and allocation comparison are recorded in the
[benchmark report](benchmarks/r3-summary.md); copy baselines do not measure
actual copies inside Codec. There is no universal performance or TPS threshold.

Local execution does not satisfy the remaining remote-CI evidence gate. The
next release step must select/commit a candidate and run the configured CI
matrix on it under the owner's publishing permissions, retaining and triaging
any toolchain anomaly. Do not mark a workflow file or this local matrix run as
GitHub CI success. No new persistence, delivery system or external interop
requirement has been introduced.

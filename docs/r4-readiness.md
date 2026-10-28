# R4 local release-readiness review

Prepared 2026-10-28. **Local release preparation only; not approval to publish.**
The candidate still needs remote CI on a submitted revision and the owner's
explicit instruction to tag, push or publish. These gates were not bypassed.

## Correctness and validation disposition

The recorded RV01–RV21 findings are fixed with the regressions identified in
[RV01–RV16](review.md), [R1](review-gaps.md),
[RV20/RV21](review-final.md#rv20rv21-corrections), and the
[R2 contract audit](r2-contract-audit.md). R4 found no new confirmed correctness
blocker in that promised subset. This is a readiness reconciliation of those
records and the unchanged source, not a new exhaustive protocol/security audit.

The [R3 source manifest](validation/r3-source.sha256) still matches every listed
file. No Go source, API, module minimum, dependency or CI workflow was changed
in R4. R3's timed fuzz/resource/benchmark results therefore remain evidence for
the same implementation; they are not relabelled as newly executed R4 tests.

R3's initial Go 1.26.4 fuzz budget-exit failure remains visible. Its matching
coordinator cancellation failure was reproduced with diagnostic logging; no
smppkit input failure or race report remains untriaged in the recorded campaign.
The unpatched toolchain can still fail at fuzz-budget shutdown. CI must preserve
and investigate failures; there is no blanket error-text exemption or automatic
retry policy. Remote CI has not run on this uncommitted candidate.

## Frozen candidate API and public documentation

The six public packages and existing architecture remain unchanged. The
[API snapshot](api-v1.txt), generated reproducibly with Go 1.26.4 `go doc -all`,
has SHA-256:

```text
0eb60d025a7f7d1c2e43f8ab36a061271aecfd3febe9ca02e17078262bbb73c2
```

This is the locally frozen candidate surface. The
[compatibility policy](compatibility.md) defines stable packages, supported
behavior, extension-interface compatibility, error/ownership guarantees,
versioning, and the Go baseline. It is not a claim that v1.0.0 already exists.
Any subsequent candidate API change must update the snapshot and affected
evidence; the owner's publication decision remains separate.

The [changelog](../CHANGELOG.md) is explicitly **Unreleased**. It records the
initial SMS scope, requirements, known limitations, validation provenance and
outstanding release gates. README, the [support matrix](support.md),
[examples](../examples/README.md), GoDoc and the compatibility policy agree on:

- Explicit 3.3/3.4/5.0 profiles, no 3.3 TLVs, documented SMS commands/TLVs,
  optional private extensions and standalone Codec use.
- Ordinary acceptance versus transaction outcome versus final delivery;
  uncertain writes are not retried. No persistence or delivery system is implied.
- Unsupported standard 3.3 multipart, broadcast/historical commands, packed
  GSM/national language tables, receipt dialect parsing and aggregation.
- External SMSCs remain unverified; the legacy harness proves only its limited
  3.4 UDH path. Local examples and benchmarks are not production guarantees.
- Go 1.26 minimum and explicit AI authorship; no independent human audit or
  complete standards certification is claimed.

## License, dependency and distribution boundary

The Apache-2.0 `LICENSE` is byte-identical to the initial repository's license:

```text
c71d239df91726fc519c6eb72d318ec65820627232b2f796219e87dcf35d0ab4
```

Its `[yyyy]`/owner placeholders are in the standard explanatory appendix, not
an unfinished claim of a named copyright owner. The existing license was
preserved; this review does not invent a rights holder or provide a legal audit.
`go list -m all` lists only smppkit. Dependencies of the six public packages are
standard-library packages and this module's own packages, including internal/wire.

The optional legacy harness uses a separately installed reference checkout and
its dependencies in a temporary module. No peer source is vendored, no dependency
is added to the public go.mod, and no old review/regression material is copied
into this candidate. Public evidence contains synthetic protocol inputs and
tool/test observations. The protocol references are links, not bundled PDFs.

Distribution candidates are selected from tracked and nonignored untracked
files (`git ls-files -c -o --exclude-standard`), because the initial HEAD does
not contain the implementation. `/doc/` remains ignored and has no tracked
entries. `.git`, local design material, credentials/environment files, build
binaries and reference checkouts are not distribution inputs. The content check
also rejects symlinks and unexpected file extensions. A targeted scan found no
embedded private-key blocks or common token forms; this is not a secret-detection
or supply-chain certification.

The [distribution manifest](validation/r4-distribution.sha256) identifies the
complete proposed public file set, excluding only that manifest itself to avoid
self-reference. It supplements the immutable R3 source identity; a future
Git commit/tag must be recorded separately. Nothing was force-added or committed
to make this local inventory.

## Local packaging verification

See [R4 check output](validation/r4-checks.txt) for the executed staging checks.
The temporary staging directory contains only the proposed public distribution
files and has no Git metadata or `/doc/`. Go 1.26.4 build, vet and the ordinary
race suite validate that staged module, including compilation of all examples.
This does not rerun the optional ten-minute soak, timed fuzz campaign, legacy
interop or benchmarks; those remain the R3 evidence for the identical Go source.

## Remaining publication gates

| Gate | Status |
| --- | --- |
| Recorded correctness findings and local validation | Closed locally, with the explicitly classified toolchain caveat above |
| API freeze, compatibility policy, release-note draft, public-content checks | Local candidate preparation; see the staging evidence |
| Exact submitted commit/tag identity | Pending: the implementation remains an uncommitted working tree |
| Remote CI on that submitted candidate | Pending: local matrix checks are not GitHub Actions results |
| Owner instruction to tag/push/publish | Pending; continuing local work did not authorize these actions |

After the owner selects a submission/release action, record the actual commit,
compare it with these manifests, run the configured CI matrix and examine any
failure before making a release decision. If source changes, rerun affected
checks rather than borrowing older results. Do not add persistence, external
interop or a Go-baseline upgrade as a new gate.

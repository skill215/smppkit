# Public API and compatibility policy

Version 1.0 policy, 2026-10-28. The six public packages have a stable API
starting with v1.0.0. The candidate review snapshot remains the API baseline;
subsequent changes follow the compatibility rules below.

## Stable surface

The module is `github.com/skill215/smppkit`. Its root package identifier is
`smpp`; the other public packages are `codec`, `pdu`, `tlv`, `textcodec`, and
`message`. The [candidate API snapshot](api-v1.txt) records their exported
declarations and GoDoc. The [R2 audit](r2-contract-audit.md),
[support matrix](support.md) and package comments specify behavior.

The contract includes:

- Exported functions, methods, types, documented fields, constants and extension
  interfaces; explicit protocol profiles and the advertised command/TLV subset.
- Documented ownership, concurrency and error/result guarantees: owned decoded
  data, nonmutating encoding, bounded admission, per-segment outcomes, no implicit
  retry, original response identity and explicit memory ACK semantics.
- Configured limits and documented zero/default behavior. Constructors are
  required for Session, Server, Codec, Plan, Receiver and References; arbitrary
  zero runtime objects and nil interface implementations are not usable instances.
- Existing extension contracts. New required methods will not be added to
  `codec.CommandCodec` or `textcodec.Encoding` within v1. Optional new behavior
  must use a separate interface or an explicit API.

Use keyed literals for exported structs. New optional fields, constants, types
and methods may be added compatibly; positional struct literals and exhaustive
assumptions about open-ended peer statuses/tags are not compatibility promises.
New configuration fields must preserve existing behavior when omitted. Existing
fields will not be removed, renamed or made mandatory in a compatible release.

The following are not stable contracts: unexported implementation details,
`internal/` packages, object layout, exact allocation counts, timing/throughput,
error-message wording, test/benchmark names, example CLI flags, and local
diagnostic scripts/artifact paths. Examples demonstrate the public API but are
not a supported standalone SMSC product. Unknown private command payloads and
customer/network policy remain the caller's responsibility.

Use `errors.Is`/`errors.As` (or the supported Go `errors.AsType`) with documented
sentinels/types rather than parsing error strings. Codec structural errors,
protocol validation errors and transport errors are distinct; not every invalid
frame wraps `codec.ErrValidation`. Failed Decode output is diagnostic, not a
usable successfully decoded PDU. An unsuccessful Request can still have a
meaningful Result; do not discard it or infer retryability from error text.

## Versioning

Stable releases use `vMAJOR.MINOR.PATCH`:

| Change | Release policy |
| --- | --- |
| Correct an implementation defect against the documented contract; improve tests/docs | Patch release; explain observable corrections and retain regressions |
| Add compatible optional APIs, commands or policies | Minor release; update the support matrix and evidence |
| Remove/rename public APIs, add required interface methods, or break documented behavior for valid supported input | Major release; provide migration notes and the corresponding major module path |

Correcting acceptance of malformed or explicitly unsupported wire input can be
a patch fix; accidental permissiveness is not a supported peer dialect. Such
changes must be called out, with independent fixtures where applicable. A
stricter interpretation of previously documented **valid** input is not silently
treated as a patch correction: preserve compatibility through an explicit
policy or use a major release. Deprecation should precede removal where feasible;
deprecated APIs remain usable during v1.

Release 1.0.x requires Go 1.26 or newer (`go 1.26.0` in go.mod). Raising the
minimum Go series is not planned in this release and is not a patch-release
change. A future increase requires an explicit decision, release notes and
updated CI. Testing an existing CI job with Go 1.27 does not itself change the
minimum or resume the deferred upgrade. Tested patch versions are recorded as
observations, not a claim that every later compiler has already passed.

## Scope is part of the contract

Stable means stability of the documented SMS subset, not all commands in every
SMPP edition. SMPP 3.3 has no TLVs or standard high-level multipart support;
broadcast and historical excluded commands remain outside Release 1.0.
Packed GSM/national tables, receipt dialect parsing/aggregation, persistence,
durable ACK, routing and business delivery tracking are not implied commitments.

Ordinary submission acceptance is distinct from final delivery. Explicit
transaction responses report the peer's synchronous outcome; the application
owns its interpretation. Memory ACK is not a transaction result or crash
recovery promise. These boundaries cannot be weakened by an example or a
positive Codec-only test.

## Candidate review and publication

Regenerate `docs/api-v1.txt` with Go 1.26.4 `go doc -all` for each of the six
public packages, in the order listed there, before accepting a public API
change. Review the diff for signatures, fields, constants, ownership and behavior;
a text snapshot is not an automated compatibility proof. Update documentation,
examples and relevant tests together. Keep the source manifest and CI evidence
attached to the exact candidate rather than an earlier passing tree.

The [release gates](release-1.0.md) still require remote CI on a submitted
candidate and explicit owner authorization before tagging, pushing or
publishing. A local freeze, release-note draft or successful staging build does
not announce a released version.

# Go SMPP libraries: comparison and lessons

Reviewed: 2026-10-29 (Asia/Shanghai).

smppkit v1.1.0 emphasizes explicit protocol, ownership, failure and resource
contracts across Client, Server and standalone Codec use. Other Go libraries
offer useful advantages in client convenience, encoding breadth and longer
public histories. This review does not establish that smppkit is faster, more
reliable, or more widely interoperable.

## Evidence and limits

The smppkit baseline is the v1.1.0 implementation and its documented contracts.
The two [v1.2.0 enhancements](roadmap/v1.2.0.md) are planning items, not available
features. External evidence comes from the projects' public README files and
selected source files on their default branches as inspected on the date above.
Those links can change; default-branch capabilities need not be present in a
published release. Recheck and pin revisions before a reproducible cross-library
test or an implementation decision based on their source.

This was a documentation and selected-source review, not a complete audit of
the other libraries. No cross-library benchmark, production SMSC test or new
conformance test was run for this comparison. An unverified capability is not
an assertion that it is absent. Project claims are distinguished from inspected
behavior; stars, repository age and passing CI do not prove correctness.

## Capability comparison

| Area | smppkit v1.1.0 | fiorix/go-smpp | linxGnu/gosmpp | CursedHardware/go-smpp |
| --- | --- | --- | --- | --- |
| Primary scope | Client, Server and independent versioned Codec | SMPP 3.4 client APIs and test server | SMPP 3.4 client with event-based session management | Project describes itself as an SMPP v5 implementation |
| Version boundaries | Explicit 3.3/3.4/5.0 profiles and documented exclusions | README targets 3.4 | README targets 3.4 | v5 coverage claimed; completeness not audited here |
| Server use | Public Server API using the shared Session | smpptest test server | Client-focused public API | Server-related implementation exists; its complete contract was not reviewed |
| Multipart | UDH8/UDH16/SAR preparation, per-segment outcomes and bounded reassembly | SubmitLongMsg helper | Long-message construction and encoding-aware splitting | README advertises automatic splitting and concatenation |
| Encodings | Unpacked GSM7, Latin-1, strict UCS-2 and explicit UTF-16 | Also advertises packed GSM7 and ISO-8859-5 | Packed GSM7 and extensible encoding/splitting interfaces | Advertises a broader set including Japanese and Korean encodings |
| Connection convenience | Optional heartbeat; application owns reconnect | Heartbeat, request window and optional rate-limiter API | Optional automatic rebind, heartbeat and windowed request tracking | Session behavior not compared exhaustively |
| Validation emphasis | Versioned mandatory fields, standard/customer TLVs and contextual checks | README explicitly notes incomplete compliance | Validation equivalence not audited | README explicitly says PDU values are not range-limited |

Sources: [fiorix README][fiorix-readme], [Transmitter][fiorix-transmitter],
[linxGnu README][linx-readme], [Settings][linx-settings],
[Session][linx-session], [ShortMessage][linx-message], and
[CursedHardware README][cursed-readme].

smppkit's [support matrix](support.md) takes precedence over shorthand in this
table. In particular, 5.0 broadcast, standard 3.3 high-level multipart submission,
packed GSM7 and national-language table interpretation are not implemented.
National-language declaration protection prevents unsafe text interpretation;
it does not implement those alphabets. submit_multi is available through the
typed PDU/Session API, not a high-level multi-destination SendText operation.

## Where smppkit has useful strengths

- **Explicit partial and uncertain results.** Request outcomes distinguish
  not-written, partly-written, written-without-response and correlated-response
  states. Multipart sending retains every planned segment, including those not
  attempted. In the inspected fiorix SubmitLongMsg implementation, a `do` error
  returns `nil, err`, even if earlier parts succeeded. That is a concrete API
  difference; it does not imply that every error path or other library behaves
  the same way. See [source][fiorix-transmitter] and our [contracts](decisions.md).
- **Resource limits beyond the request window.** Reassembly bounds groups,
  charged bytes, queues, segments, reference leases and expiry handling. Memory
  admission ACKs are explicitly non-durable. These documented contracts help
  applications reason about long-lived servers and untrusted input.
- **Shared protocol implementation.** Concrete PDUs, versioned Codec and one
  Session runtime serve client, server and protocol-tool use without requiring
  applications to build a separate server protocol stack.
- **Receipt evidence remains separate.** DLR helpers preserve text/TLV evidence,
  opaque IDs and conflicts without silently selecting a delivery truth. The
  application owns correlation and final outcome decisions. See [receipts](receipts.md).
- **Small dependency boundary.** The core module has no third-party runtime
  module dependencies and uses optional standard-library slog diagnostics.
  This also leaves us responsible for maintaining the built-in encoding code.

These are useful design properties, not proof of superiority over every
configuration or fork of the comparison projects.

## Tradeoffs and remaining weaknesses

**Interoperability and independent review evidence are limited.** The existing
[legacy simulator harness](../interop/legacy/README.md) covers a specific local
3.4 TRX/UDH exchange and documents peer limitations. It does not establish broad
operator interoperability. fiorix documents use of SMPPSim and linxGnu documents
Melrose Labs simulator testing; neither statement is itself production
certification. Our AI-generated implementation and tests can share mistaken
assumptions. Independent wire fixtures avoid relying solely on our serializer,
but are not an independent human audit. See the [AI disclosure](../AI_DISCLOSURE.md).

**Strictness has a compatibility cost.** Missing sc_interface_version in bind_resp
leaves the selected Codec version intact but disables Session TLVs. A peer that
then sends TLVs can fail where a more permissive implementation might continue.
Any future exception needs a narrow, explicit policy and independent evidence;
this comparison does not authorize changing that default.

**Encoding coverage is narrower.** The broader encoding menus in other projects
are worth studying, but their boundary/error behavior was not validated here.
Adding packed GSM7 or national-language tables would require correct framing,
septet/fill-bit budgets, escape boundaries and independent fixtures, not just a
new charset name.

**Client setup requires more application code.** Automatic reconnection can be
convenient; smppkit deliberately leaves it to the application. Reconnecting a
transport is different from replaying an uncertain submission. The former can
be illustrated safely without introducing the latter or a delivery subsystem.

**Two high-level API gaps remain.** Binary bytes can be sent through typed PDUs,
but there is no SendText-equivalent binary segmentation entry point. Some
semantic errors still lack stable field/rule identifiers. Both are planned for
v1.2.0, not implemented capabilities.

**The toolchain floor is higher.** smppkit requires Go 1.26. The inspected default
branches declare Go 1.25 for [fiorix][fiorix-mod] and Go 1.24 for
[linxGnu][linx-mod]. Published versions can differ. Keep our current Go 1.26
decision; this observation does not reopen the deferred upgrade discussion.

**Performance leadership is unproven.** Owned decode results and transactional
encode behavior incur allocation/copy costs. Reusing an encode destination does
not eliminate the private intermediate frame. SendText waits for each part's
response before submitting the next part, increasing single-message completion
latency on high-latency links. Concurrent calls can share a Session window; that
does not make one multipart send internally parallel. See the existing
[benchmark methodology and copy paths](benchmarks/README.md).

## Lessons to adopt or evaluate

| Lesson | Proposed application | Status and boundary |
| --- | --- | --- |
| Explicit binary constructors, as in linxGnu | Accept bytes and explicit DCS without string conversion; retain owned plans, all-segment preflight and per-segment results | Design input to the already approved v1.2.0 binary feature |
| Encoding-aware splitting, as in linxGnu's Splitter | Check whether existing encoding contracts express safe boundaries before proposing any extension | API-review consideration; no new required interface method or packed-GSM feature authorized |
| Optional automatic rebind convenience | Consider an example with bounded backoff, context cancellation, a new Session per connection and shared application-owned References | Future example candidate; no core reconnect manager or automatic request replay |
| Separate window and rate limits, as in fiorix | Explain that outstanding-request count does not cap TPS; consider an application-level pacing example | Future documentation/example candidate; no new core limiter or dependency approved |
| Standalone protocol CLI/receiver tools | Improve existing examples' bind, DLR and timeout diagnostics with safe metadata | Future usability candidate; no gateway service or new CLI product committed |
| Comparable performance evidence | Use identical fixtures, profiles, ownership requirements and validation scope; measure allocations and transport separately | Future experiment, not an existing result or release requirement |

Borrow the interface intent rather than copying ownership behavior. In the
inspected linxGnu code, SetMessageDataWithEncoding stores the supplied slice and
GetMessageData returns it. Our proposed byte preparation must specify snapshot
timing and owned outputs instead of accidentally exposing mutable aliases.
Likewise, an encoding-specific splitter is a useful idea, but does not establish
that any particular implementation handles all multipart boundaries correctly.

A future reconnect example should preserve old-session outcomes, never replay
uncertain requests automatically, and demonstrate sharing References across
connection generations. A rate-limit example should state whether it charges
messages or SMPP segments, whether admission consumes the caller's deadline,
and how cancellation works. A small window alone is not a TPS limit when
responses arrive quickly.

Retain the lessons already embodied in our contracts: preserve partial results,
use bounded reference leases rather than treating random/incrementing values as
collision prevention, and avoid guessing encodings or claiming full compliance
from a command checklist. Before reusing any external implementation or adding
an encoding dependency, separately check its license, maintenance, invalid-input
behavior and relevant protocol boundaries. This review imports no source code.

## Effect on the roadmap

The approved [v1.2.0 scope](roadmap/v1.2.0.md) remains exactly two enhancements:
stable semantic field/rule identifiers and high-level binary segmentation.
Reconnect/pacing examples, richer tools, additional encodings, compatibility
profiles and cross-library benchmarks are evaluation candidates only. They are
not new release gates or implementation commitments.

Keep Go 1.26 and the current architecture. No persistence, delivery-state system,
connection pool, automatic business retry or external-interconnection requirement
is introduced. Reference repositories remain read-only; this document uses only
public external sources and the library's own published contracts.

[fiorix-readme]: https://github.com/fiorix/go-smpp/blob/main/README.md
[fiorix-transmitter]: https://github.com/fiorix/go-smpp/blob/main/smpp/transmitter.go
[fiorix-mod]: https://github.com/fiorix/go-smpp/blob/main/go.mod
[linx-readme]: https://github.com/linxGnu/gosmpp/blob/master/README.md
[linx-settings]: https://github.com/linxGnu/gosmpp/blob/master/pkg.go
[linx-session]: https://github.com/linxGnu/gosmpp/blob/master/session.go
[linx-message]: https://github.com/linxGnu/gosmpp/blob/master/pdu/ShortMessage.go
[linx-mod]: https://github.com/linxGnu/gosmpp/blob/master/go.mod
[cursed-readme]: https://github.com/CursedHardware/go-smpp/blob/master/README.md

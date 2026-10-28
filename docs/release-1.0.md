# Release 1.0 target and acceptance gates

Defined 2026-10-28. **Target, not a release announcement or completed checklist.**
The [R1 review](review-gaps.md) records three reproduced defects, now corrected
with formal regressions. R1's subsequent test-evidence follow-up is also complete;
R2's public contract audit is also complete locally. R3 local validation is
complete, and R4 local release preparation is complete. Submitted-candidate
identity, remote-CI evidence and publication authorization remain open.
Go upgrade work U1 is deferred and is not a release
prerequisite. Publishing or pushing still requires the owner's explicit request.

## Goal

Deliver a stable, general-purpose Go SMPP SMS library, usable independently by
clients, servers and simulators, with predictable wire behavior, bounded runtime
resources and explicit submission uncertainty. Current minimum Go version:
**1.26**, also the Release 1.0 target while the Go upgrade is deferred.
Keep one module, concrete PDU structs, a version-configured Codec, a shared
Session, and small explicit extension interfaces.

Release 1.0 is the initial SMS release (the usable M1–M4 scope after removing
persistence), not a declaration of complete coverage of every SMPP edition.
Broadcast remains the later M5 increment. This sequencing follows the agreed
SMS-first design. The command/version matrix is part of the release contract;
an unsupported command must fail clearly rather than silently use another profile.

## Required scope

| Area | Release 1.0 commitment |
| --- | --- |
| Versions | Explicit SMPP 3.3, 3.4 and 5.0 profiles; no implicit fallback. 3.3 rejects all TLVs. 3.4/5.0 support documented standard and customer TLVs. |
| Commands | Bind roles defined by each version, unbind, enquire_link, generic_nack, submit_sm, deliver_sm, query_sm, replace_sm, cancel_sm and submit_multi; data_sm, receiver outbind and alert_notification in 3.4/5.0. Request/response layouts, contextual checks and endpoint roles match the [matrix](support.md). |
| Standalone Codec | Bounded framing, exact version layouts, owned decode results, preserved unknown/customer parameters where allowed, scoped private request/response extensions. No Session required. |
| Endpoints | Client dial/bind; Server authentication/admission; TCP and supplied TLS connections/listeners; one reader/writer per Session; correlation, windows, cancellation, deadlines and shutdown. |
| Sending | One-call text preparation/submission for supported policies; automatic 3.4/5.0 UDH8/UDH16/SAR segmentation; explicit message_payload mode; every planned segment has a result, including unsent/uncertain/rejected ones. |
| 3.3 text | Correct single-part coding for supported policies. Reject incompatible built-in encoding choices before writing. Standard 3.3 multipart remains unsupported and documented; do not claim every text policy works in every version. |
| Reception | Bounded in-memory admission and reassembly, per-segment ACK, out-of-order/duplicate/conflicting segments, namespace/address/port isolation, completion backpressure, expiry events and bounded retention. |
| Text / metadata | Strict unpacked GSM7, Latin-1 where supported, UCS-2 and explicit UTF-16BE compatibility; safe boundaries, explicit fallback, opaque binary payloads and preserved metadata. Raw receipt classification remains available. |
| Public contract | Stable exported API, ownership/concurrency/error GoDoc, runnable client/server/codec/multipart/extension examples, clear capability matrix and limitations. Apache-2.0; any future dependency requires the agreed license/maintenance/boundary/error review. |

An ordinary OK submission response is acceptance, not final delivery. Explicit
transaction responses report a peer delivery outcome; applications own its
interpretation. `submit_multi` also
requires checking individual unsuccessful SMEs. Cancellation after writing can
leave the outcome uncertain. No automatic business-request retry, reconnect or
atomic multipart/exactly-once promise is part of this target.

## Explicit boundaries

- **Application responsibility, not a future library milestone:** persistence,
  durable admission/ACK, crash recovery, database adapters, routing, message
  storage semantics, distributed reference allocation and business delivery state.
  The low-level Request/Receive/Respond APIs let applications implement their own
  ACK policy; AckOnBuffered promises bounded memory admission only.
- **Later optional library work:** SMPP 5.0 broadcast; historical 3.3
  query_last_msgs/query_msg_details/param_retrieve; peer-specific 3.3 multipart;
  packed GSM/national-language tables and other encodings; receipt dialect parsers
  and aggregation; convenience asynchronous handles and a public smpptest package.
- **Deferred by owner:** external SMSC/consumer interoperability. Independent local
  bytes and the optional read-only legacy simulator harness remain the evidence
  sources. Release notes must explicitly state that external peers are unverified.
- No ygw replacement, sms-fw/DEI-specific core, mandatory service/UI framework,
  or private business/customer fixtures. Existing ygw can remain a consumer peer.

## Ordered gates

Unchecked items are work remaining, even when earlier tests cover part of them.

### R1 — Close the correctness review

- [x] Fix RV17: enforce negotiated TLV policy on correlated responses, preserving
  bind negotiation and enabled-TLV behavior.
- [x] Fix RV18: make the writer join observe cancellation/RequestTimeout while
  retaining observed response/byte counts and crossed-unbind semantics.
- [x] Fix RV19: version-aware text/DCS policy, including fallback; reject the
  incompatible built-in 3.3 Latin-1 case without restricting raw Codec unnecessarily.
- [x] Promote all three probes into normal tests with positive/boundary controls;
  remove the temporary overlay and mark findings fixed only after evidence exists.

The original fixes completed locally on 2026-10-28. See
[R1 evidence](review-gaps.md#r1-corrections-and-validation). The
[renewed R1 review](review-gaps.md#r1-reassessment-after-deferring-the-go-upgrade)
did not reopen those defects. Its narrowly scoped follow-ups are now complete:

- [x] Strengthen FuzzPrepareVersionEncoding's acceptance oracle: valid bounded
  inputs must not pass the test merely because Prepare rejected them. Demonstrate
  that the over-rejection mutation is caught; preserve expected 3.3 exclusions.
- [x] Add the selected-Latin1-fallback SendText preflight case alongside the
  existing direct-Latin1 case, asserting no sequence/queue/write admission.
- [x] Recheck and classify the observed fuzz-run deadline exit when validating
  these changes. Keep its failed run distinct from the successful rerun; do not
  claim an untriaged test-run failure has been fixed.

See [completion evidence](review-gaps.md#r1-evidence-follow-up-completed).
The timeout did not recur; a possible harness cancellation window was reproduced
separately, but the original failure's precise cause remains unconfirmed. No Go
toolchain fix or blanket timeout exemption is claimed.

These are validation tasks for existing contracts, not new capabilities. Do not
add persistence, recovery, retry/reconnect orchestration or delivery aggregation
to close R1. The general API audit remains R2; the longer campaign remains R3.

### R2 — Freeze the public SMS contract

- [x] Correct [RV20/RV21](review-final.md#rv20rv21-corrections) with ordinary
  regressions: decoded byte-field append isolation and transaction admission
  boundaries for automatic memory ACK.
- [x] Audit every exported entry point, role/version combination, owned/borrowed
  value and error/result guarantee against examples and GoDoc. Keep necessary
  refactoring local; justify any public API adjustment before freezing it.
- [x] Reconcile support.md with actual command, TLV, direction and transaction
  validation. Turn remaining normative gaps in the advertised subset into fixes
  or explicit limitations; do not require implementation of excluded commands.
- [x] Cover encoding/version/fallback combinations and SendText failures before
  write, during partial write, after write without response and after rejection;
  assert all segment outcomes and absence of implicit retry.
- [x] Verify receiver ACK order, duplicate/conflict/expiry behavior, capacity
  recovery and shared namespace/reference boundaries. Include queue saturation
  and slow consumers without any persistence requirement.
- [x] Compile every example and execute self-contained examples; exercise the
  client/server example together on loopback. Audit example cancellation and cleanup.

Completed 2026-10-28: [API/role/ownership inventory and gap closure](r2-contract-audit.md),
[fresh R2 validation](validation.md#r2-contract-validation). Existing tests were
reused; only uncovered public boundaries received new tests. No public signature
or library runtime behavior changed. Client example expiry now runs under busy
traffic too. This fixes the intended SMS contract for candidate validation;
R4 still owns the final stable API/versioning approval.

### R3 — Validate the release candidate

- [x] On the candidate tree, pass formatting, build, vet and the full race suite
  with the declared minimum Go series (1.26). Also execute the configured
  toolchain matrix's checks locally and record the actual versions/results.
- [ ] Run every configured toolchain CI job on the recorded submitted candidate.
  A workflow file alone is not evidence that remote CI has passed.
- [x] Run every checked-in fuzz target with a recorded corpus/time/worker budget
  (at least 30 seconds per target for the candidate); retain minimized failures
  as regressions. Fuzz success is not a conformance certificate.
- [x] Keep independent protocol fixtures for every advertised command/version,
  including malformed/truncated/negative/boundary cases. Exercise real local
  Session traffic against independent byte peers in all three profiles; include
  the newly discovered response-policy and cancellation cases.
- [x] Repeat the existing local resource workload for at least 10 minutes under
  race detection. Record successful work, capacity failures and post-join heap,
  goroutine and FD observations. Investigate sustained retained-resource growth;
  do not gate on a fictional universal TPS or exact heap-byte value.
- [x] Rerun the optional legacy simulator harness on the candidate, preserving
  its repository and documenting its limited 3.4 UDH scope. It cannot replace
  independent bytes or establish 3.3/5.0 compatibility.
- [x] Rerun encode/decode/copy benchmarks with `-benchmem`, plus existing message
  preparation benchmarks; record toolchain, CPU, fixtures and raw output. Explain
  material regressions. Allocation counts do not measure actual copy traffic.

Local execution completed 2026-10-28: [R3 evidence and candidate identity](r3-validation.md).
One Go 1.26.4 fuzz budget-exit failure is retained and attributed through a
reproduced coordinator cancellation window; it is not silently converted to a
pass. Every target also has a successful 30-second run. Installed toolchains and
CI were not patched to suppress failures. The source remains uncommitted and
was not pushed, so remote CI is still an explicit open gate. No external-peer
requirement or Go-baseline upgrade was added.

### R4 — Release readiness review

- [x] Close all recorded correctness blockers in the promised subset, with no untriaged
  race/fuzz failure. Document any remaining non-blocking limitations explicitly.
- [x] Record the exact local candidate file set and completed gate evidence;
  distinguish new results from historical local runs.
- [ ] Attach the submitted commit identity and its remote-CI results before
  publication; a local file manifest is not a submitted revision.
- [x] Finalize README, GoDoc, support matrix, examples, license and release notes;
  ensure `/doc/` and reference/private materials are not included in the release.
- [x] Review and freeze the local candidate public API for 1.0; compatibility changes after that
  must follow the published versioning policy. Stable does not mean all SMPP
  commands or all peer dialects are supported.
- [ ] Obtain the owner's explicit release instruction before tagging, pushing
  or publishing. Local implementation/testing remains authorized meanwhile.

Local preparation completed 2026-10-28: [R4 readiness record](r4-readiness.md),
[compatibility policy](compatibility.md), [API snapshot](api-v1.txt) and
[unreleased notes](../CHANGELOG.md). The proposed public distribution passes
Go 1.26.4 build/vet/full race checks independently of the ignored design directory.
R3 source hashes remain unchanged; its longer campaign is reused as historical
evidence for the same implementation, not claimed as a fresh R4 run.

## Deferred U1 — Upgrade the Go baseline

Deferred on 2026-10-28 at the owner's request; not a Release 1.0 gate or a
dependency of R1–R4. Keep the [assessment](go1.27-assessment.md) for a later
decision. No upgrade or new 1.27-only source change is currently scheduled.

If resumed, compare identical source under the old/new toolchains first, then
coordinate the module minimum, CI and public documentation. Review useful API
changes and rerun build/vet/race/fuzz/examples and allocation/copy benchmarks.
The original proposed target was Go 1.27.3; recheck the patch version at that time.

## Starting point

M0/M1 foundations and M2/M3 SMS paths exist, with later command/TLV/context checks,
local lifecycle/resource evidence and a narrow legacy simulator check. Go 1.26.4
build/vet/race pass on the R1 tree; RV17–RV19 are now covered by ordinary regressions.
R1's fixes and bounded evidence follow-up and R2's public SMS contract audit are
complete. R3 local checks are complete; its remote-CI gate still needs a submitted
candidate. R4 local preparation is complete; submission and publication gates
remain open. Local results do not authorize
tagging, pushing or publishing.
U1 is deferred, and no release date is claimed yet.

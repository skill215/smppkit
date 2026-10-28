# Implementation review, persistence rollback and R1 corrections

Completed 2026-10-28, after the owner's scope correction. This report describes
the current working tree, not the baseline commit or a released package.
RV17–RV19 are **fixed locally in R1**, with formal regressions and evidence below.
The [renewed R1 assessment](#r1-reassessment-after-deferring-the-go-upgrade)
identified two test-evidence gaps and a fuzz-run anomaly. The
[R1 follow-up](#r1-evidence-follow-up-completed) closes both gaps and records the
timeout investigation; no additional production defect was confirmed.
The historical findings describe the pre-fix behavior; their source line numbers
refer to the reviewed tree. RV01–RV16 in the [earlier review](review.md) are
separate, previously corrected findings.

## Scope correction

The persistence experiment was withdrawn: durable admission APIs, file storage,
snapshot/locking code, recovery tests, the durable example and its documentation
were removed. The existing bounded in-memory Receiver and AckOnBuffered remain.
Persistence, application crash recovery and durable ACK guarantees belong to the
consuming application; they are neither library features nor release blockers.
This supersedes the older persistence milestones in the ignored design material.
No storage interface is planned as a substitute for the removed implementation.

Earlier unrelated working-tree changes were preserved. No reference repository
was edited, no commit/push/publication was performed, and `/doc/` stays ignored.

## Historical confirmed findings (before R1)

### RV17 — P2: correlated responses bypass negotiated TLV policy

Location: [session_reader.go](../session_reader.go), `dispatchResponse`, lines
61–74. After a successful bind without `sc_interface_version`, Session explicitly
sets `TLVEnabled()` to false. Request, inbound-request dispatch and Respond enforce
that policy; correlated-response dispatch does not. An independently authored
5.0 `enquire_link_resp` with `congestion_state` is accepted as ResponseReceived
with nil error despite optional parameters being disabled.

This is an inconsistency with the library's documented negotiation policy, not
a claim that standalone Codec should reject this valid 5.0 frame. Check optional
parameters under the Session lock before publishing a non-bind response, while
preserving bind advertisement handling. Include normal, negative and generic
NACK responses, plus TLV-enabled positive controls, in the eventual regression.

Evidence: `TestR1ResponseTLVPolicy` (promoted from the review probe) performs a real TX bind using a raw
peer, checks literal outgoing bytes, and supplies this response for sequence 2:

```text
00000015 80000015 00000000 00000002 0428 0001 01
```

### RV18 — P2: cancellation is ignored while joining a write after its response

Location: [session.go](../session.go), `Request`, lines 331–340. A response can
arrive before the corresponding `net.Conn.Write` returns; the implementation
explicitly supports this ordering. Once the response has been claimed, the join
select waits on the write or Session closure, but not on the request context.
Cancellation (including RequestTimeout) therefore fails to interrupt an active
write in this branch. The call can remain blocked until WriteTimeout or an
unrelated Close, holding an outbound window slot.

Evidence: the original review probe used a transport wrapper
that exposes request bytes to a raw peer and delays returning Write. The wrapper
obeys Close and the write deadline. After confirming response dispatch under the
state lock, the test cancels the request; it remains blocked for the 200 ms probe
until explicit Close. The formal replacement, `TestR1ResponseBeforeWriteCompletion`,
uses virtual time instead of that 200 ms wait. These tests exercise an ordering,
not a claim of frequent stalls in ordinary TCP connections.

Handle context cancellation during the join and retire/join an active write as
the existing cancellation contract requires. Preserve the already observed
response and final byte count; document whether the completed response wins the
returned error. Add cancellation and RequestTimeout coverage without weakening
the crossed-unbind completion rules or racing write counters.

### RV19 — P2: built-in Latin-1 preparation uses the wrong 3.3 DCS meaning

Location: [message/prepare.go](../message/prepare.go), `Prepare`, lines 204–208;
the coding assignment comes from `textcodec.Alphabet.DataCoding`. The planner
copies the encoding's version-independent DCS value into all profiles. Thus
`Prepare(V33, Text{Text: "@"}, Policy{Encoding: textcodec.Latin1})` succeeds with
DCS 3 and payload `40`; Codec accepts the resulting low-level PDU.

[SMPP 3.3 Table 6-4](https://smpp.org/SMPP-v3.3.pdf) defines this field as GSM
DCS. In the general coding group, bits 3..2 select the alphabet: value 3 does
not select Latin-1. A GSM7 interpretation turns payload `40` into `¡`, rather
than the intended `@`. See [3GPP TS 23.038 §§4 and 6.2.1](https://www.etsi.org/deliver/etsi_ts/123000_123099/123038/16.00.00_60/ts_123038v160000p.pdf).
The 3.4/5.0 Latin-1 assignment must not be silently reused for 3.3.

Reject that built-in 3.3 policy unless an explicit compatible peer mapping is
provided. Check the selected encoding after fallback as well. Keep raw 3.3 DCS
available to low-level Codec users and retain Latin-1 for 3.4/5.0; a blanket
Codec ban would prevent legitimate peer policies.

Evidence: the original Latin-1 probe checked later-version controls and the
exact emitted 3.3 frame against hand-authored bytes. Its formal replacements are
`TestR1EncodingVersionPolicy` and `TestR1RawV33CodingRemainsAvailable`:

```text
00000022 00000004 00000000 00000001
00 00 00 00 00 00 00 00 00 00 00 00 00 00 03 00 01 40
```

## Formal regression tests

The temporary overlay and .txt probes have been removed. Their cases now live
in [session_r1_test.go](../session_r1_test.go) and
[message/r1_test.go](../message/r1_test.go), included in ordinary `go test ./...`.
The broadened tests reproduced all three defects before the implementation fix.
Run them separately from the repository root:

```sh
go test -race . ./message -run '^TestR1' -count=20 -timeout=90s
```

RV17 covers 3.4/5.0 data responses with customer TLVs, 5.0 standard response TLVs,
positive/negative/NACK responses, advertised/absent negotiation and TLV-free
controls. Literal frames are checked for standalone Codec validity independently
of Session's stricter policy; bind negotiation uses an independent byte peer.

RV18 uses `testing/synctest` fake time and a transport gate to establish response
publication before cancellation. Cases cover explicit cancellation, request and
caller deadlines, retained PeerError, a completed-write control and an unfinished
crossed-unbind ACK. Every result retains the observed response and byte count;
request slots are released. The fake-time cases replace the original timed probe.

RV19 covers built-in values/pointers, empty input, selected/unused Latin-1 fallback,
Latin-1 falling back to UCS-2, later-version controls, explicit custom mapping,
raw 3.3 Codec preservation and SendText rejection before sequence/queue admission.
`FuzzPrepareVersionEncoding` adds version/encoding/fallback/content invariants.

## Review coverage and remaining gaps

The review inspected framing and ownership, version/command/TLV checks, extension
boundaries, Client/Server setup, Session correlation/cancellation/shutdown,
notification queues, text preparation, references, reassembly, ACK ordering,
resource accounting, examples and validation claims. It is a source review with
targeted reproductions, not exhaustive conformance or a security certification.

| Area | Assessment / remaining work |
| --- | --- |
| Architecture | Concrete PDUs, versioned immutable Codec and shared Session remain suitable. No broad rewrite or storage abstraction is warranted. |
| SMS protocol | Existing command/layout and standard TLV checks have independent-byte tests. Review gaps RV17/RV19 show that handshake-to-response and version-to-text boundaries required additional tests, now present in R1. |
| Concurrency | Existing cancellation, crossed unbind, bounded queues and cleanup tests pass. R1 now covers the response-before-Write-return ordering from RV18. |
| Multipart | Memory admission, per-segment outcomes, UDH/SAR assembly, conflict/expiry/capacity behavior exist. 3.3 multipart remains explicitly unsupported; memory ACK is not durable. |
| Receipts | Raw classification/provenance exist. Dialect parsing and per-segment delivery aggregation do not. Submission acceptance remains distinct from final delivery. |
| Historical / broadcast | 3.3 query_last_msgs/query_msg_details/param_retrieve and 5.0 broadcast commands remain explicitly unsupported. |
| Public API / usability | Stable API review and release-candidate acceptance are still required. No standalone smpptest package; internal fixtures and the codec simulator example cover current use. |
| Evidence | Local independent TCP bytes cover all three profiles; the legacy simulator covers a narrower 3.4 UDH path. External SMSC interoperability is deferred, not presumed. |

## Historical validation before R1

Go 1.26.4, linux/amd64. Build, vet and the ordinary full race suite passed after
the rollback. The interrupted fuzz processes' uncollected results were not used;
fresh completed smoke runs on 2026-10-28 are recorded below.

| Check | Observed result |
| --- | --- |
| `go build ./...` | PASS, including examples |
| `go vet ./...` | PASS |
| `go test -race -count=1 -timeout=90s ./...` | PASS; loopback TCP/TLS enabled, no external service |
| `FuzzDecode`, 5s, 2 workers | PASS, 204,747 executions |
| `FuzzUDH`, 5s, 2 workers | PASS, 205,092 executions |
| `FuzzCharsets`, 5s, 2 workers | PASS, 22,448 executions; harness finished in about 6s |
| Open-review overlay, race, count=3 | Expected FAIL on RV17–RV19 in each repetition |

Fuzz counts are observations, not coverage or reliability guarantees. Existing
benchmark, 60-second resource and legacy-simulator records were not rerun in this
review. Those passing ordinary tests did not close the three findings on the
pre-fix tree; the later R1 evidence is separate.

## R1 corrections and validation

Completed locally on 2026-10-28, Go 1.26.4, linux/amd64. Public signatures and
architecture are unchanged; no dependency was added.

| Finding | Correction |
| --- | --- |
| RV17 | Check negotiated optional-parameter permission under the Session lock before publishing a non-bind correlated response. Invalid responses retire the Session and do not become ResponseReceived. Bind negotiation and standalone Codec remain distinct. |
| RV18 | Observe ctx.Done while joining the write, retire an active writer and join before reading counters. An observed response still wins nil/PeerError; unfinished crossed-unbind obligations still return an error. A completed writer does not trigger retirement. |
| RV19 | Validate the selected built-in encoding after fallback. Reject Latin-1 for 3.3 with ErrMetadata before constructing a plan. Leave custom peer mappings and low-level raw DCS behavior explicit. |

| Executed check | Result |
| --- | --- |
| `go build ./...` / `go vet ./...` | PASS, all examples compile |
| `go test -race -count=1 -timeout=90s ./...` | PASS, with local TCP/TLS enabled |
| R1 formal tests, race, count=20 | PASS in root and message packages |
| `FuzzPrepareVersionEncoding`, 10s, 2 workers | PASS, 32,070 executions |
| `FuzzCharsets`, 5s, 2 workers | PASS, 10,541 executions |
| `FuzzDecode`, 5s, 2 workers | PASS, 147,346 executions |
| `FuzzStandardTLVSection`, 5s, 2 workers | PASS, 120,673 executions |
| `FuzzUDH`, 5s, 2 workers | PASS, 145,162 executions |

The first combined R1/lifecycle test invocation in the restricted sandbox failed
only where loopback socket creation was denied. The full race run above used the
required local-socket permission and passed. This was an environment restriction,
not an ignored test failure. Fuzz runs are bounded smoke checks; none substitutes
for the longer R3 candidate campaign. The new preparation fuzz target is added
to the README commands and CI configuration; remote CI was not run here.

Next is R2: audit/freeze the public SMS contract and examples. R1 completion does
not close [R2–R4 release gates](release-1.0.md). No external service, persistence,
reference-repository change, push or release is part of this correction.

## R1 reassessment after deferring the Go upgrade

2026-10-28, Go 1.26.4 linux/amd64, current working tree after the standard-library
modernization. The owner deferred Go 1.27 work and returned the focus to R1.
This is a targeted reassessment of RV17–RV19 and adjacent request/text boundaries,
not another whole-library conformance claim. Production code and test sources
were not changed by this reassessment.

### Existing corrections

| Finding | Source and regression reassessment |
| --- | --- |
| RV17 | Negotiated TLV permission is checked under the Session lock before publishing correlated non-bind responses. Current standard optional-bearing response structs are covered by hasOptional; bind establishment stays separate. Existing independent peer fixtures cover absent/present advertisements, plain/customer/standard fields, negative responses and NACK. No new defect confirmed. |
| RV18 | Response publication, cancellation and writer joining retain the shared Session architecture. The join still observes cancellation and deadlines; counters are read after completion; observed peer responses remain available. Existing fake-time tests cover cancellation/deadlines, PeerError, completed-write and unfinished crossed-unbind cases. No new defect confirmed. |
| RV19 | Prepare checks the actual selected built-in encoding after fallback, before constructing a plan. Raw Codec DCS and explicit custom encodings remain available. Existing tables cover values/pointers, empty input, selected/unused fallback and version controls. No new defect confirmed. |

### Bounded evidence gaps

Historical findings from the reassessment, before the follow-up below.

1. **R1-E1: the encoding fuzz oracle accepts over-rejection.** In
   [message/r1_test.go](../message/r1_test.go), FuzzPrepareVersionEncoding skips
   every error outside the specific 3.3 Latin-1 branch. Consequently it cannot
   detect valid short input being rejected. This is a test weakness, not evidence
   that the real Prepare currently rejects those inputs.

   Confirmed using a review-only overlay under `/tmp`: add an early
   `return nil, ErrMetadata` for every valid version to Prepare. All seven
   checked-in fuzz seeds still pass with that deliberately wrong implementation.
   The ordinary 3.4 UCS-2 positive unit cases fail against the same overlay, so
   the existing regression suite does have independent positive checks. The
   overlay never modified repository source.

   Follow-up: require acceptance for an independently bounded subset of valid
   inputs, accounting for selected encoding, the 3.3 Latin-1 exclusion and 3.3
   multipart limits. Verify that the over-rejection mutation fails the strengthened
   fuzz oracle. Do not replicate all of Prepare inside the test.

2. **R1-E2: selected fallback is not checked at the SendText admission boundary.**
   [TestR1V33Latin1SendPreflight](../session_r1_test.go) checks direct Latin-1
   rejection before sequence and queue admission. The selected-fallback case is
   tested in Prepare, but not at this public entry point. Add one GSM7-to-Latin1
   fallback case, such as `ÿ`, with the same no-admission assertions. Both paths
   currently share Prepare, so this is a small regression hardening task, not a
   reproduced transmission bug. No new sending API or encoding policy is needed.

Do not expand R1 into every possible command/role test permutation. Broader API
and example review belongs to R2, while sustained workloads and candidate-wide
validation belong to R3. No architecture refactor is justified by these findings.

### Checks executed in this reassessment

| Check | New observation |
| --- | --- |
| `go build ./...` and `go vet ./...` | PASS, includes example compilation. |
| `go test -race . ./message -run '^TestR1' -count=20 -timeout=90s` | PASS. |
| Adjacent R1/Review, partial-write/queued-cancel, close/cancel, bind-advertisement, crossed-unbind and terminal-unbind tests, race, count=10 | PASS in root and message packages. |
| Over-rejection overlay, `-run '^FuzzPrepareVersionEncoding$'` | PASS for the fuzz seeds despite deliberate invalid behavior; confirms R1-E1. |
| Same overlay, 3.4 UCS-2 positive unit cases | Expected FAIL; confirms ordinary tests reject the mutation. |
| Real source, FuzzPrepareVersionEncoding, 5s / 2 workers, first run | FAIL after 130,797 executions with `context deadline exceeded`; no crashing input or failing assertion was reported. |
| Real source, same fuzz command, separate rerun | PASS after 67,349 executions; about 6 seconds including harness shutdown. |

The first fuzz-run failure is retained, not counted as a pass or diagnosed as a
library bug. Its cause remains unclassified; the successful rerun is insufficient
to call it fixed. Recheck it when closing the evidence follow-up, preserving logs
and any future reproducer. Fuzz execution counts are observations, not coverage.
No full all-package race campaign, benchmark, external interop or long soak was
rerun in this pass; earlier records remain historical.

### Public-library scope

R1 remains limited to advertised wire behavior, version/encoding checks,
request correlation, bounded cancellation and accurate result/error semantics.
Persistence, durable ACK, crash recovery, database adapters, delivery tracking,
automatic uncertain-request retry and reconnect orchestration are not added as
requirements. Explicit unsupported features stay in the support matrix and do
not become R1 blockers merely because they are absent.

## R1 evidence follow-up completed

2026-10-28, Go 1.26.4 linux/amd64, with `GOTOOLCHAIN=local` for the final build,
vet, full race and fuzz runs. Only test code and documentation changed. The
module still requires Go 1.26.0; Codec, Session, exported APIs and dependency
scope are unchanged. R1-E1 and R1-E2 are closed.

### Corrections and negative controls

- **R1-E1:** FuzzPrepareVersionEncoding now checks both sides of the contract.
  Encoding failure must return ErrEncoding with no plan; unsupported 3.3
  Latin-1 or multipart input must return ErrMetadata with no plan. Every other
  encodable input in the bounded domain must succeed and preserve selected
  encoding, DCS and bytes. At most 4096 input bytes produce at most 8192 encoded
  bytes with the built-ins, below default input/encoded/segment limits. The
  oracle uses explicit 160-septet/140-octet single-part bounds and does not call
  Prepare's splitting or budget helpers to decide acceptance.
- The 256 checked-in seeds exercise all 16 primary/fallback pairs across 16
  strings, including malformed UTF-8, empty input, GSM extension characters,
  supplementary characters, single-part boundary pairs and the input bound.
  Hand-authored expected bytes in the existing unit tests remain independent
  checks of the encoder itself; this fuzz oracle checks planner policy.
- **R1-E2:** TestR1V33Latin1SendPreflight now covers direct Latin-1 and GSM7
  falling back to Latin-1 for `ÿ`. Both assert no plan segments, sequence,
  pending request, queue/window admission or transport Write. An atomic write
  counter and writer join prevent an already-dequeued job from evading the
  observation. No wall-clock sleep or peer response is needed.
- Two `/tmp`-only mutation overlays validate the assertions. Rejecting every
  valid-version Prepare call now fails the first fuzz seed. Checking only the
  primary encoding instead of the selected fallback fails the SendText fallback
  case. Both failures are expected evidence; neither mutation entered the source
  tree, and no toolchain source was modified.

### Fuzz timeout investigation

The earlier `context deadline exceeded` exit remains a failed historical run.
It reported no failing assertion or crashing input. In this pass, three separate
5-second diagnostic runs, a default 30-second run and a 100,000-execution run
all passed. Debug output shows deadline shutdown and coverage minimization
(`crasher: false`), which also explains why elapsed time can exceed fuzztime and
why displayed execution counts may pause between worker results.

Inspection of the installed Go 1.26.4 source found a plausible coordinator race:
`internal/fuzz.CoordinateFuzzing` handles the parent deadline with
`stop(ctx.Err())`, but suppresses cancellation by comparing that error with the
worker child's `fuzzCtx.Err()`. `context.cancelCtx.cancel` closes the parent's
Done channel before cancelling children. A standalone probe using those context
operations observed the parent reporting DeadlineExceeded while the child still
reported nil in **5 of 20,000 trials**. That window can cause the coordinator to
retain the deadline as an error rather than normal budget exhaustion.

This reproduces the possible cancellation window, **not the exact historical
fuzz failure**. Classification: suspected fuzz-harness deadline/shutdown anomaly;
no smppkit input failure was reproduced. The precise original cause remains
unconfirmed, and this pass does not claim to fix Go's harness. Do not suppress
future nonzero exits or automatically retry them into success. The count-limited
run is supplementary evidence, not a replacement for time-budgeted validation.
Reopen investigation if the failure recurs, retaining diagnostics and any input.

### Executed validation

| Check | Result |
| --- | --- |
| Formatting, `go build ./...`, `go vet ./...` | PASS; examples compile. |
| Root/message `TestR1*` and all preparation fuzz seeds, race, count=20 | PASS. |
| `go test -race -count=1 -timeout=90s ./...` | PASS, including local TCP/TLS with loopback permission. |
| Both mutation checks | Expected FAIL at the new acceptance/preflight assertions. |
| FuzzPrepareVersionEncoding, 5s, 2 workers, `GODEBUG=fuzzdebug=1`, three runs | PASS: 78,256 / 71,590 / 229,468 executions. |
| Same target, 30s, 2 workers, default GODEBUG | PASS: 649,954 executions; about 31s including shutdown. |
| Same target, 100000x, 2 workers, default GODEBUG | PASS: exactly 100,000 executions. |

[Diagnostic evidence and commands](validation/r1-evidence-go1.26.4.txt) include
the fuzz logs, expected mutation failures, standalone context probe and inspected
toolchain source hashes. Debug seed enumeration lines are omitted as labelled;
failure and shutdown diagnostics are retained. Execution counts are observations,
not coverage guarantees. No benchmark, soak, legacy simulator or remote CI run
is claimed for this test-only follow-up. It completes R1's bounded follow-up;
R2–R4 remain open and Go 1.27 remains deferred.

## R3 follow-up to the fuzz budget-exit investigation

On 2026-10-28 the [R3 campaign](r3-validation.md#diagnostic-attribution) reproduced
an actual Go 1.26.4 fuzz coordinator failure with the parent deadline set and the
worker child error still nil. A temporary logging overlay captured the mismatch;
the installed toolchain was not modified. This provides stronger evidence than
the earlier standalone context-window probe. The earlier uninstrumented R1 run
is not retroactively a pass or an exact internal trace. R3 retains its own failed
run, diagnostic reproduction and successful timed runs separately.

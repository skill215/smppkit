# Implementation review and corrections

2026-10-27, review of the local implementation preview. All findings below were
fixed locally; no push or release was performed. The existing architecture
(concrete PDUs, versioned Codec, shared Session, standalone message preparation
and reception) remains appropriate. No three-stack rewrite is warranted.

The earlier passing test suite missed these cases. Each finding now has a
regression test or measured reproducer; earlier validation is not evidence that
these paths were already correct. Priorities describe the pre-fix impact.

For the later 2026-10-28 review and its R1 corrections, see
[findings RV17–RV19](review-gaps.md). Their evidence is recorded separately from
this historical report.

| ID | Priority | Reproduced behavior | Correction / evidence |
| --- | --- | --- | --- |
| RV01 | P1 | Closing after the final bind response octet was read, before dispatch, let the reader change Closed back to Bound. Its next stop panicked while closing the done channel again. | Keep Closed terminal under the state lock; unbind transitions use the same lock as admission. `TestReviewCloseIsTerminalForLateBindResponse` uses a literal response and a deterministic read barrier. |
| RV02 | P1 | Two SAR messages with identical addresses/reference but different application ports completed as one mixed message. | Include ordered, presence-sensitive source/destination port identity in the assembly key; validate widths, duplicates and UDHI conflicts. `TestReviewSARPortNamespacesDoNotMix` checks both completed payloads independently. |
| RV03 | P1 | A customer TLV validator received an empty value despite declaring MinLength=1. A callback indexing its declared field could panic on peer input. | Validate declared width, applicability and cardinality before invoking it. `TestReviewCustomerTLVBoundsBeforeCallback` exercises encode and independent decode bytes. Extension callbacks remain trusted Go code. |
| RV04 | P2 | Encoding about 4 MiB of supplied TLVs with MaxFrame=1024 allocated about 22 MB before rejection. | Bounded Writer checks growth before copying; preflight optional-section size before callbacks/validation maps; opaque bodies use the same bound. Writer limit tests and `BenchmarkReviewRejectOversizedFrame` cover the fix. Allocations inside a private CommandCodec remain its responsibility. |
| RV05 | P2 | Cancellation while a custom encoder was blocked still consumed a sequence and admitted a request when encoding returned. Response matching also raced the cancellation decision. | Recheck context before admission and before a queued write starts; retire pending correlation under the same lock as response dispatch. `TestReviewCancelDuringEncodingDoesNotQueueOrWrite` plus existing queued-cancel/concurrency tests. |
| RV06 | P2 | Editing the owned Inbound PDU changed the command/sequence used by Respond, preventing the original request from completing. | Capture immutable response identity in the handle. `TestReviewInboundResponseIdentitySurvivesPayloadEdits`. Concurrent mutation is still forbidden. |
| RV07 | P2 | data_sm accepted UDHI together with SAR, in both encoding and decoding. | Share the UDH/SAR/port exclusion check across submit/deliver/data. Independent malformed data_sm bytes and port vectors cover both versions. |
| RV08 | P2 | Invalid local text addressing consumed a reference lease before codec validation; repeated mistakes exhausted the reference pool without sending. | Preflight every segment and current session eligibility before reserving. `TestReviewInvalidTextRequestDoesNotConsumeReference`. A later cancellation/transport race can still retain a reserved lease conservatively. |
| RV09 | P2 | Mutating a custom encoding after Prepare changed plan Encoding/DataCoding, while materialized PDUs retained the old DCS. | Snapshot metadata; Plan no longer retains the encoding provider. `TestReviewPreparedPlanFreezesEncodingMetadata`. |
| RV10 | P2 | Standalone ParseSegment accepted a 65,536-byte TLV value, impossible to represent with the wire length. | Check each value's 16-bit length bound before cloning. `TestReviewParseRejectsUnrepresentablePayloadLength`. |
| RV11 | P2 | SAR preparation used the full single-part radio budget for each concatenated part and ignored application-port TLV overhead. | Reserve a conservative 16-bit concatenation header and optional 16-bit port element in the radio budget, without emitting UDH alongside SAR. `TestReviewSARReservesRadioHeaderBudget` checks single/multipart and port cases. |

## Refactoring decisions

- Separate reader dispatch into `session_reader.go` to make state ownership and
  response correlation reviewable; keep one Session runtime and one mutex for
  protocol transitions. Cancellation and correlation now have one decision point.
- Centralize bounded output growth in `internal/wire.Writer`; command encoders
  cannot append directly around the cap. Share UDH/TLV conflict checks.
- Keep response identity and prepared-plan metadata independent of mutable
  application-owned values. No new public framework or generalized field map.

## Protocol basis and deliberate policy

The UDH/SAR/port exclusion and port widths follow
[SMPP 3.4 §§5.2.12, 5.3.2.20–24](https://smpp.org/SMPP_v3_4_Issue1_2.pdf).
The high-level SAR radio-budget choice uses the header layouts in
[3GPP TS 23.040 §§9.2.3.24.4 and 9.2.3.24.8](https://www.etsi.org/deliver/etsi_ts/123000_123099/123040/16.00.00_60/ts_123040v160000p.pdf).
This is a conservative library policy for a peer mapping SAR to GSM radio SMS,
not a claim that SMPP requires every SMSC to map SAR identically. With a
140-octet budget, unpacked GSM multipart SAR allows 152 septets, or 145 with
16-bit port addressing. Single-part SAR without ports still allows 160.
Low-level PDUs remain available for explicitly different peer behavior.

## Review limits

This was a correctness-focused source review with deterministic reproductions,
independent bytes, race tests and short fuzz runs, not an exhaustive protocol or
security certification. There is no independent live SMSC test. Outstanding
feature/conformance work remains listed in [support](support.md) and
[validation](validation.md). At the time of this review it included
submit_multi/historical operations, standard-field/TLV semantics, one-way
endpoints, 3.3 multipart compatibility,
durable reception and receipt tracking. Broadcast remains later work.
The owner subsequently removed persistence/durable reception from library scope;
it is no longer a pending capability or release gate.
Subsequent increments implemented submit_multi and expanded field/TLV checks;
their evidence and remaining limits are recorded separately in those documents.

Further refactoring should follow those concrete requirements. Extracting a
storage interface, replacing the PDU model or splitting by protocol version
would add complexity without resolving a demonstrated problem at this stage.


## Follow-up local refactoring and comments

A further pass on 2026-10-27 separates the writer/job lifecycle into
`session_writer.go` and the response handle into `session_inbound.go`.
`session_reader.go` now has distinct response correlation, bind completion,
inbound admission and automatic-control-response helpers. Inbound state checks,
map insertion and queue publication happen under one lock, preventing Close or
an outbound unbind from slipping between eligibility and publication.

Additional GoDoc and internal comments cover state/lock ownership, channel
publication of write results, response-slot lifetime, timeout uncertainty,
reference retention, raw/owned TLV slices, charset boundaries, reassembly
accounting, expiry events and caller responsibilities. Public signatures remain
unchanged. ErrOverloaded retains its sentinel identity; its text now refers to
session capacity because outbound admission can also return it.

RV12 (P2): Prepare accepted malformed or repeated application-port elements in
AdditionalUDH, producing payloads that ParseSegment would reject. A regression
first reproduced this behavior. The new prepareUDH helper checks 8/16-bit port
widths, duplicate/mixed port representations, reserved concatenation elements
and the aggregate header bound before copying. Unknown UDH elements remain
opaque. Independent expected payload bytes verify valid port/unknown elements
and ownership; FuzzAdditionalUDH checks successful preparation against parsing
and the radio budget. This pass adds no command/version or durability capability.

## Session lifecycle follow-up

The following failures were reproduced before correction in this increment:

| ID | Priority | Reproduced behavior | Correction / evidence |
| --- | --- | --- | --- |
| RV13 | P1 | Crossed unbind closed after one obligation, losing the local response or peer ACK. | Track both obligations; `TestLifecycleCrossedUnbind` checks both completion orders with independent bytes; missing-half tests verify bounded failure. |
| RV14 | P2 | Direct Request(Unbind) returned with state still Unbinding. | Successful admitted unbind retires the connection; `TestLifecycleRequestUnbindIsTerminal`. |
| RV15 | P2 | A closed, retained Session still held unread inbound payloads/map entries. | Drop terminal reservations and unread inputs; `TestLifecycleCloseReleasesUndeliveredInput` and 40-cycle cleanup assertions. Completed/cancelled job buffer/capture cleanup has a separate regression check. |
| RV16 | P1 | Concurrent shutdown sometimes reported EOF/closed pipe after the peer consumed its ACK but before the local writer recorded completion. | Narrow EOF deferral to the ACK writer once required incoming unbind data is present; simultaneous shutdown stress and deterministic completion-state tests. |

Public signatures and Codec validation are unchanged. These are fixes with local
regression evidence, not claims that earlier passing tests covered these races.
The [lifecycle contract](session-lifecycle.md) and [validation record](validation.md)
separate independent TCP byte-peer coverage from external interoperability.

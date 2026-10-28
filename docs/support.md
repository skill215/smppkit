# Implementation support matrix

Status: v1.0.0 SMS support; R2 audit completed 2026-10-28. This inventory distinguishes command
layouts from endpoint support. “Implemented” is not a full conformance claim;
semantic restrictions and unvalidated areas below still apply.

R1 update, 2026-10-28: [RV17–RV19](review-gaps.md) are corrected with formal
regressions for response TLV negotiation, cancellation during a write join, and
3.3 Latin-1 preparation. The [release record](release-1.0.md) links the
submitted candidate's successful remote CI and the authorized publication steps.
[R4 preparation](r4-readiness.md) preserves the historical API freeze and
public-distribution checks.
[R3 local validation](r3-validation.md) records the timed fuzz, ten-minute resource
workload, unchanged legacy peer and local two-toolchain checks separately.
The [R2 public contract audit](r2-contract-audit.md) maps the exported API and
role/version policy to existing evidence and the narrow regression additions.

[RV20/RV21](review-final.md#rv20rv21-corrections) now have formal regressions:
decoded standard byte fields are independently appendable, and automatic memory
ACK rejects explicit transaction submissions before admission. Low-level
Receive/Respond remains available for application-owned transaction outcomes.

Each request row includes its response except one-way outbind/alert operations.
Standard/reserved command IDs in `0x00000000..0x000001ff` (and their response
range) cannot bypass validation through an opaque body or private registration.

| Command | SMPP 3.3 | SMPP 3.4 | SMPP 5.0 | Session |
| --- | --- | --- | --- | --- |
| bind_receiver / bind_transmitter | Implemented | Implemented | Implemented | Client bind and server admission |
| bind_transceiver | Not defined | Implemented | Implemented | Client bind and server admission |
| unbind / enquire_link | Implemented | Implemented | Implemented | Automatic responses; explicit outbound requests |
| generic_nack | Implemented | Implemented | Implemented | Explicit response and outbound correlation |
| submit_sm / deliver_sm | Implemented | Implemented | Implemented | Direction/role validation, Request/Respond and text helpers |
| query_sm / replace_sm / cancel_sm | Implemented layouts | Implemented layouts | Implemented layouts | Explicit Request/Respond; no application storage semantics |
| submit_multi | Implemented | Implemented | Implemented | ESME TX/TRX Request, server Receive/Respond; per-SME failures retained |
| query_last_msgs | Unsupported historical command | Removed | Removed | Not implemented |
| query_msg_details | Unsupported historical command | Removed | Removed | Not implemented |
| param_retrieve | Unsupported historical command | Removed | Removed | Not implemented |
| outbind | Not defined | Implemented, RX flow | Implemented, RX flow | Server.Outbind bootstrap; ESME ReceiveNotification then explicit receiver bind |
| alert_notification | Not defined | Implemented | Implemented | MC Notify on RX/TRX; ESME ReceiveNotification, no ACK |
| data_sm | Not defined | Implemented | Implemented | Request/Respond and admission with [direction](data-direction.md) and [transaction-context checks](data-transaction.md) |
| broadcast_sm / query_broadcast_sm / cancel_broadcast_sm | Not defined | Not defined | Unsupported (M5) | Not implemented |
| Registered private request/response pairs | Implemented without TLVs | Implemented | Implemented | Bound sessions, both directions |
| Unregistered private command | Opaque codec preservation | Opaque codec preservation | Opaque codec preservation | Rejected by session |

For bound traffic, submission/query/replace/cancel/multi requests require an
ESME TX/TRX role; delivery and alert requests require RX/TRX. data_sm applies
those roles to its respective direction. Enquire/unbind and registered private
pairs work in either direction for any supported bound role. One-way commands
have no response handle. See the [role table](r2-contract-audit.md#version-role-and-validation-reconciliation).

The transaction-context checks above apply to data_sm. This is not a claim of
complete transaction/receipt validation for submit_sm or submit_multi. Raw
protocol operations retain peer/business semantics; no delivery system is
provided. An ordinary submission OK is acceptance, while explicit transaction
responses must be interpreted as the peer's reported synchronous outcome.

SMPP 3.3 uses exact `0x33` binds, TX/RX roles, 8-character message IDs and a
160-byte short-message limit. No TLVs of any kind are supported. Legacy bind
aliases are not inferred. Its standard `submit_sm.esm_class` is unused, so the
high-level sender rejects text requiring UDH concatenation before writing.
Single-part text works. Peer-specific 3.3 multipart compatibility requires a
future explicit profile; it is not advertised as standard support.
Built-in Latin-1 is rejected in 3.3, including when selected by fallback, because
its DCS=3 assignment belongs to the later SMPP table. Raw Codec DCS fields remain
available; a custom Encoding must supply an explicit compatible peer mapping.

[One-way operations](notifications.md) use a separate bounded notification queue
and the shared writer. outbind uses a configured profile and explicit application
credential validation; its receiver-only bootstrap scope is documented there.

The [shared Session lifecycle](session-lifecycle.md) handles crossed unbind,
terminal request completion and bounded retirement cleanup. Independent local
TCP byte peers exercise bind, submit, late/duplicate responses, control traffic
and shutdown in all three profiles; external SMSC interoperability is unverified.
The [optional legacy simulator harness](../interop/legacy/README.md) additionally
checks 3.4 TRX UDH messaging with old go-smpp; its bind-sequence and missing-TLV
advertisement limitations are explicit. The [resource workload](local-validation.md)
checks sustained local TCP traffic and receiver capacity/recovery boundaries.

SMPP 3.4 short_message is bounded at 254 octets; 5.0 permits 255. Command-specific address
limits are 21 C-octets for submit/deliver/query/replace/cancel and 65 for
data_sm/alert_notification. C-octet bounds include NUL. Payload bytes and message
IDs remain raw/opaque. Omitted negative-response bodies are distinct from
present empty fields; header-only success responses missing mandatory fields
are rejected. 3.3 responses with mandatory fields must include them.

[Mandatory field semantics](field-semantics.md) now cover ASCII C-octets,
TON/NPI, message flags, coding assignments, priority/default-message bounds,
time grammar and query/delivery response constraints. SMPP 5.0 deliver_sm
forwarding fields are accepted; the unused-field restriction applies to 3.4.
Message IDs remain opaque ASCII strings, without numeric normalization.

`submit_multi` accepts 1..255 destinations in 3.3/5.0 and 1..254 in 3.4.
Each destination explicitly selects an SME address or distribution-list name;
the inactive variant must be empty. The response retains the opaque message ID
and up to 255 individual failures, including full 32-bit statuses. An OK header
does not imply every destination was accepted. Distribution-list expansion,
routing and storage belong to the SMSC application. `SendText` remains a
single-destination multipart API; `RunReceiver` does not expand submit_multi.
Use explicit `Request` / `Receive` / `Respond` for this operation.
See the [annotated independent wire samples](wire-samples.md).

Ordered TLVs, including unknown/duplicate raw fields, are retained on commands
with optional sections. The [53 standard SMS TLV definitions](tlv-support.md)
enforce version, command, width, cardinality, defined scalar values/reserved bits
and dependencies. Network-specific content remains opaque where described in
that matrix. Customer tags in `0x1400..0x3fff` can have per-codec definitions;
standard/reserved tags cannot be replaced. Unknown does not mean malformed.
SMPP 5.0 supports `congestion_state` on every implemented response, including
otherwise header-only responses. Broadcast commands and their TLVs remain out
of scope for this increment.

| Message capability | Current implementation |
| --- | --- |
| Encodings | Strict unpacked GSM7 default/extension alphabets, ISO-8859-1, UCS-2BE, explicit UTF-16BE |
| Preparation | Owned immutable plan, safe character boundaries, explicit fallback, input/segment limits |
| Concatenation | UDH8, UDH16, SAR; additional UDH elements and application ports retained; SAR reserves conservative radio header overhead |
| message_payload | Explicit single-request policy, distinct from multipart |
| Sending | Client submit/server deliver; sequential segments through shared window; full per-segment results |
| References | Bounded 8/16-bit leases, retention and explicit cross-session sharing |
| Reception | Out-of-order assembly, duplicate suppression, conflict errors, namespace/address/port isolation |
| ACK | Explicit bounded-memory admission per segment; no wait for whole message. Automatic ACK excludes explicit transaction submissions; mode 0 needs a non-transaction MC default. |
| Resource control | Groups, retained byte charges, message size, segments, completion queue, tombstones, events, deadlines |
| Receipts | Classified before reassembly and retained raw with per-segment metadata |
| Persistence / durable admission / crash recovery | Application responsibility; outside the public library scope |
| Receipt text dialects / tracking / aggregation | Not implemented; final delivery is unknown |
| Packed GSM septets / national language tables | Not implemented; no heuristic decoding |

Protocol authorities: [SMPP 3.3](https://smpp.org/SMPP-v3.3.pdf) §§6–7,
[SMPP 3.4 issue 1.2](https://smpp.org/SMPP_v3_4_Issue1_2.pdf) §§4–5,
[SMPP 5.0](https://smpp.org/SMPP_v5.pdf) §§3–4,
[3GPP TS 23.038](https://www.etsi.org/deliver/etsi_ts/123000_123099/123038/16.00.00_60/ts_123038v160000p.pdf)
§6 and [TS 23.040](https://www.etsi.org/deliver/etsi_ts/123000_123099/123040/16.00.00_60/ts_123040v160000p.pdf)
§9.2.3.24. Fixtures are synthetic and authored from field layouts, not customer
traffic. Historical commands are listed even where implementation has not
assigned public constants for their obsolete IDs.

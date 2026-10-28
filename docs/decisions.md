# API contracts and implementation decisions

Recorded 2026-10-27. The Release 1.0 candidate API was frozen locally during
[R4 preparation](r4-readiness.md); publication remains gated. The
[compatibility policy](compatibility.md) applies upon the first stable release.

## Module and ownership

One module, root package `smpp`, concrete PDU structs, one immutable versioned
`codec.Codec`, and one shared Session runtime. `message` never imports the root
package. Private command codecs and text encodings are small trusted interfaces;
authentication is one function. No global registration or universal field map.

A body determines its command ID. Lengths are computed, never caller supplied.
Encoding returns the original destination length on failure and never mutates
inputs. Decoding returns owned payloads/TLV values. Standard byte-field slices
share an owned body allocation but expose no capacity beyond their own length,
so ordinary append cannot overwrite sibling fields (RV20). Plan previews/materialized
parts are copies; encoding name and DCS are snapshots, not live provider calls. Applications must not mutate inputs concurrently with an
operation. A custom codec or encoder must obey the same contract and be safe
for concurrent invocation; extension Go code is trusted, not sandboxed.

`pdu.Omitted` represents absent negative-response bodies; it cannot bypass a
mandatory success body. `pdu.Opaque` preserves unregistered private bytes only.
`codec.FieldError` contains safe field/offset diagnostics, never field values.

## Version policy

The Codec fixes wire version. Server bootstrap reads a bounded first frame and
accepts only exact version bytes, selects a configured profile and authenticates
before returning a bound Session. Unknown versions do not fall back. A bind
response's version advertisement must match the requested profile; absence
preserves that profile but disables outbound/inbound TLVs at the Session layer.
This includes correlated positive/negative responses and Generic NACK; the
policy is checked before publishing a response. Bind responses establish the
policy and are handled separately. Standalone Codec validity alone does not
establish negotiated permission to receive optional fields.
No live profile replacement is supported. Standalone Codec use applies its
explicit configured profile without handshake state.

The 3.3 standard leaves submit_sm.esm_class unused. Rather than introduce an
undocumented extension to the agreed version rules, the initial high-level
sender rejects multipart 3.3 text. This is an explicit compatibility gap, not a
claim that long messages cannot ever work with a 3.3 peer. Exact historical
aliases and multipart compatibility need independent peer fixtures.

## Session lifecycle and concurrency

`NewSession` starts an unbound client on an existing net.Conn; `Bind` establishes
TX/RX/TRX. `Dial` combines transport and bind. `Server.Accept` performs bounded
bind admission on a supplied connection. `Server.Serve` bounds concurrent
connections including authentication; callbacks must honor their context.
Existing TLS connections and tls.Listener compose with these APIs.

One reader and one serialized writer run per connection, with one bounded
expiry ticker. Authentication, application handlers and reassembly run outside
the reader. Trusted CommandCodec and TLV validation callbacks execute
synchronously inside Codec, including on the Session reader; they must bound
their work and must not wait for that Session's traffic. Outbound and
inbound sequence spaces are separate. Pending requests are registered before
write queue admission. Responses match both sequence and command, with generic
NACK allowed. Wrong-command responses close the connection. Late/unmatched and
duplicate responses are ignored. Sequence numbers are never reused within a
connection; exhaustion retires it. Reconnect explicitly creates a new Session.

Outbound queues/windows and outstanding inbound handles are bounded. Reader
pressure closes the session with ErrOverloaded; it does not block response
correlation waiting for application handlers. Responses have a separate bounded
queue; at most eight consecutive responses precede a queued application frame.
Request and notification queues alternate when both are busy. Control
requests are answered automatically; heartbeat initiation is explicit via
Request(EnquireLink). Applications must have one inbound consumer and must not
combine RunReceiver with a competing Receive loop.

Request cancellation before writing prevents that write. Cancellation during an
active write retires the connection and joins the writer to obtain a final byte
count. This may make other requests uncertain. A fully written request can time
out without closing an otherwise healthy session; its sequence is never reused.
Results distinguish not attempted, not written, partly written, written without
a response, and a correlated response. Negative responses produce PeerError
while retaining the response in Result. No business request is retried.
If the response arrives before Write returns, the join still observes request
cancellation/RequestTimeout and retires an active transport when necessary.
An already correlated response wins the request outcome (nil or PeerError);
its bytes and response are retained even if cancellation closed the Session.
Unbind still requires completion of both obligations during crossed shutdown.

Closed is terminal even when a decoded bind response races Close. Inbound
handles capture original response identity separately from their owned PDU.

Close aborts; Wait joins runtime goroutines; Shutdown requests unbind and closes.
An asynchronous request-handle API is deferred; concurrent callers can use the
same bounded synchronous Request API. There is no automatic reconnect manager.

## Finite defaults

| Scope | Default |
| --- | --- |
| Codec | 64 KiB frame cap, configurable 16 bytes..16 MiB |
| Session | 16 outbound requests, 32 outstanding inbound requests |
| Notifications | 32 queued inbound notices; 32 concurrent Notify calls and 32 outbound queue positions |
| Timers | Request/Notify 30s including admission; inbound response 30s; frame read 2m; write 10s |
| Server | 128 connections; bind deadline 10s |
| Preparation | 64 KiB UTF-8 input; 256 KiB encoded data; at most 255 parts |
| Radio budget | 140 octets; GSM transmitted as unpacked septets |
| Reference leases | session scoped; 256 8-bit or 4096 16-bit leases; 5m retention |
| Receiver | 1024 collecting groups; 8 MiB retained byte charges; 128 KiB/group; 255 parts |
| Completion / retention | 32 queued messages; 1024 tombstones; 128 expiry events; 5m group and tombstone lifetime |

Frame/count caps bound queue bytes implicitly by cap × MaxFrame. Receiver byte
charges include raw payload, extracted data, TLV/UDH values and fixed metadata
allowances; they are not a process heap measurement. Map/object overhead and
bounded tombstone signatures additionally depend on count/part limits. Data
transferred by Receiver.Receive becomes the application's responsibility.

Expired groups have a fixed deadline from first admission. Expire records an
event before releasing a group; if its bounded event queue is full it returns
ErrCapacity and retains remaining state. RunReceiver checks a one-second expiry
ticker between admissions; an ACK write can delay that check. It closes the
session on admission or expiry failure, attempting a negative segment response
for failed admission where the automatic ACK policy applies. A direct Receiver
user must call Expire and drain Events.
Duplicates never extend deadlines or requeue completion within tombstone
retention. After retention, indistinguishable remote retransmissions can be
admitted again. Share References and an explicit receive namespace only when the
application's sending/tenant scope permits cross-connection association.

## Messaging and encodings

`Session.SendText(ctx, message, policy)` keeps peer policy explicit; a variadic
References override supports coordinated multi-bind allocation. An immutable
pure `message.Prepare` plan is independently usable by simulators. This adjusts
the design sketch to avoid silently choosing a peer alphabet. Segments execute
sequentially, stop on the first failure, and report unattempted parts. Distinct
messages can execute concurrently within the Session window. Pipelining within
one logical message is a later performance option.

Before reserving a reference, SendText validates all materialized segment bodies
and checks current session eligibility and cancellation. Leases remain retained
if a later scheduling/cancellation race occurs; uncertain sends never release a
reference early.

SAR preparation reserves seven radio octets for a 16-bit concatenation header,
plus six for a 16-bit application-port element when port TLVs are present.
Port addressing alone reserves seven octets including UDHL. These budgets
model a peer mapping SAR to GSM SMS; the SMPP payload itself contains no UDH.
Port TLVs cannot coexist with generated UDH. Use AdditionalUDH for ports in UDH
mode, or SAR for TLV-based multipart ports. Payload mode delegates segmentation
to the peer. This policy and its protocol basis are recorded in [review](review.md).

The receiver is a pull-based bounded store, with RunReceiver as its session
adapter. There is no hidden unbounded callback pool. Applications choose their
own bounded consumer workers. AckOnBuffered is deliberately explicit.
Persistence, durable admission and
application crash recovery belong to the consuming application and are outside
the public library scope (owner clarification, 2026-10-27). Receipts are classified before
reassembly and retained raw; no final-delivery aggregation is fabricated.

RV21 narrows the automatic memory ACK adapter: a server-side submit_sm/data_sm
with explicit mode 2 returns ErrState before admission, ID generation or ACK.
The original inbound command and ESM class determine eligibility, even after
application edits to the owned PDU. The handle remains available for Respond;
RunReceiver follows its existing error-exit/close behavior. Mode zero is allowed
only under an application-configured non-transaction default. Delivery direction
is not restricted by these submission bits. No new public interface is required.

The custom encoding interface returns encoded bytes plus safe character end
offsets in one pass. This preserves GSM extension sequences and UTF-16 pairs
without repeatedly encoding growing prefixes. UnitBits explicitly separates
unpacked GSM septets from octet encodings. Strict UCS-2 and UTF-16BE are distinct
policies even though DCS is shared. Empty text prepares one empty request.
Built-in Latin-1 uses the 3.4/5.0 DCS=3 assignment and is rejected for 3.3 with
ErrMetadata after the actual encoding/fallback has been selected. An unused
Latin-1 fallback does not invalidate a GSM plan; a primary Latin-1 encoder that
cannot represent the input can still fall back to UCS-2. Version incompatibility
itself does not trigger an implicit fallback. Custom encodings remain trusted
peer policies responsible for their own DCS/version mapping, and low-level Codec
preserves raw DCS values. No new exported interface or profile framework is needed.

## submit_multi and standard TLVs (next messaging increment)

`SubmitMulti` has explicit fields and a discriminated `MultiDestination` slice.
It does not embed `SMFields`, which would expose an unused single destination.
The Codec shares the message suffix and its validation with submit_sm instead
of creating a separate protocol stack. Counts come from slices; Clone owns all
mutable values. Response failures carry full 32-bit opaque statuses and are not
mapped to retry advice. Request returns nil error for an OK header even when
`SubmitMultiResp.Unsuccessful` is nonempty. Applications must inspect both.
Failure counts are not compared to request list length: distribution lists may
expand into multiple SMEs, and a standalone Codec has no request context.

The version choices follow [3.3](https://smpp.org/SMPP-v3.3.pdf) §6.3.4,
[3.4 issue 1.2](https://smpp.org/SMPP_v3_4_Issue1_2.pdf) §4.5/§5.2.24, and
[5.0](https://smpp.org/SMPP_v5.pdf) §4.2.3/§4.7.19:

- Destination count is 1..254 in 3.4 and 1..255 in 5.0. 3.3 specifies an octet
  count without the later 254 cap; this profile chooses 1..255. External legacy
  peer acceptance of that upper bound has not been tested.
- 3.3 permits replacement only for one explicit SME; 3.4 reserves the multi
  replace flag at zero; 5.0 permits zero/one. The shared short_message bound is
  corrected to 160/254/255 for 3.3/3.4/5.0, including replace_sm.
- 3.3 never accepts TLVs. submit_multi_resp gains an optional section only in
  5.0. Its layout explicitly references §4.2.5; this implementation follows that
  reference although that section's introductory prose names only submit/data.
- `dest_subaddress` is rejected for submit_multi: the field-specific prohibition
  in 3.4 §5.3.2.16 and 5.0 §4.8.4.28 takes precedence over its inclusion in a
  general command TLV table.

The fixed [TLV inventory](tlv-support.md) preserves raw ordered Fields and adds
no global registration or public value-interface hierarchy. The standard
definitions cannot be replaced by customer definitions. Callback repetitions
retain wire occurrence order; any supplied alpha-tag/presentation family must
match the callback-number count. Network IDs require corresponding node IDs;
ported-number resolution requires information and country. Unknown TLVs remain
raw, including duplicates, on commands with optional sections. Known broadcast
tags are rejected on implemented SMS commands.

Ordinary Codec encoding/decoding cannot infer a data_sm's direction, so its TLV
applicability is the union of submission and delivery tables. The subsequent
direction increment below adds explicit context without changing that contract.
Receipt classification, transaction
mode policy, wireless-network-specific alphabets/error meanings and optional
parameter recommendations are not a complete semantic conformance check.
Node IDs and NP information use decimal digit octets; NP country stays raw
1..5-byte integer data. No BCD address conversion is inferred.

5.0 §4.8.4.18 permits congestion_state on any response. Accordingly QuerySMResp
and otherwise empty response structs now expose Optional, while older profiles
reject a nonempty optional section. Prefer keyed struct literals in this preview
API. This field addition preserves shared response dispatch and makes congestion
visible without enabling automatic throttling, retry or final-delivery claims.

## Mandatory field semantics increment

The [field inventory and decisions](field-semantics.md) record the added checks
and protocol ambiguities. Shared address and C-octet primitives enforce the
same rules across command layouts. `codec/semantics.go` owns small scalar and
response checks; `codec/time.go` handles wire time grammar without a clock or
new public date API. Query response checks receive header status to distinguish
reported message states from negative-response placeholders. No reflection
dispatch, mutable schema registry or per-version stack is added.

This tightens existing acceptance of malformed strings and reserved fields.
It also corrects an overly strict restriction: 3.4's unused deliver_sm fields
were previously rejected in every version, including valid 5.0 forwarding and
3.3's shared submission layout. Public struct signatures remain unchanged.
Operator policy, opaque error/status codes and uncertain-request behavior remain
separate from syntactic/protocol validation. Previous test results do not
establish these newly checked semantics; fresh evidence is in validation.md.

## data_sm direction increment

Add `codec.DataDirection` and `Codec.ValidateDataDirection` as a supplemental
context API. Existing Append/Decode signatures and their directionless union
remain unchanged. A mutable direction on Codec.Config would prevent sharing one
Codec between endpoints and concurrent directions, so direction is supplied per
check. Session translates endpoint/traffic direction and invokes the same Codec
method at all four request/response boundaries. It contains no duplicate field
catalog. Responses use the original request direction, not their transport
direction or application-editable inbound packet.

5.0 reuses the existing submission/delivery TLV masks. 3.4 has a common data_sm
table and only its explicit receipt/DPF/more-message direction descriptions are
enforced; the later 5.0 delivery exclusions are not backported. Likewise 5.0's
submission ESM types remain broader than 3.4. The [direction inventory](data-direction.md)
records sources, examples and remaining transaction/network-policy limits.

The contextual method never calls customer validators, allowing response
dispatch to keep its existing locked correlation step. Ordinary Codec validation
still owns all value/dependency/custom checks. Incoming violations follow the
existing invalid-frame close policy. Outgoing validation precedes sequence
allocation and response-handle claim. No retry, receipt-completeness guarantee
or final-delivery interpretation is added.

## data_sm transaction context increment

Add `Codec.ValidateDataResponse(response, requestESMClass, direction)` to apply
response direction and messaging-mode rules without storing mutable state in
Codec. A scalar ESM class is sufficient; retaining the whole original request
would prolong payload ownership and allow application edits to change the
meaning of an ACK. Session therefore snapshots one byte in each existing
pending/inbound entry. It uses that byte for live correlated responses and
Respond, before consuming the response handle. The maps, bounds, timers and
single reader/writer architecture remain unchanged.

Datagram submission rejects nonzero registered_delivery through the existing
direction API. Explicit datagram/store-and-forward submissions cannot receive
the transaction-only response TLVs. The 3.4 footnoted subset differs from 5.0's
submission-response table: additional_status_info_text is unrestricted in 3.4,
transaction-only in 5.0. Universal congestion and customer parameters do not
inherit that restriction. An explicit transaction OK response cannot carry
delivery_failure_reason.

Mode zero is an unresolved peer default, so it is not classified as either
transaction or store-and-forward. Delivery mode bits do not describe the prior
submission's mode. These are deliberately conservative boundaries, not hidden
fallback defaults. The [transaction inventory](data-transaction.md) records the
protocol sources and distinction between validation and delivery guarantees.
No submit_sm/submit_multi mode behavior, retry, peer-status whitelist or receipt
correlation is introduced by this data_sm increment.

## One-way endpoint increment

Add `Session.Notify`, `Session.ReceiveNotification` and
`Config.NotificationLimit`. A separate bounded notification stream preserves
the existing Receive/RunReceiver request contract and avoids exposing meaningless
Respond handles. Outbound notifications share the writer and sequence allocator
but have separate admission capacity, so a full request window does not block
them. No response correlation, automatic ACK or ACK timer is allocated.

Outbind is a pre-bind bootstrap, so `Server.Outbind(ctx, conn, version, body)`
reuses Accept's admission code rather than constructing a second MC Session
runtime to perform the same handshake. Its explicit profile prevents guessing a
version from a PDU that contains none. ESMEs receive the credentials as an owned
notification and decide whether to bind; no authentication callback runs in the
reader. The implemented bootstrap is receiver-only in both 3.4 and 5.0, with
other flows left explicit rather than silently inferred. The MC's used bootstrap
sequence is initialized in Session before runtime goroutines start.

The [notification contract](notifications.md) records sources, failure outcomes,
queue/resource bounds and remaining scope. Private one-way definitions and reverse
TLS automation are outside this increment. Durable notification replay belongs
to the application, outside the library scope.

## Session lifecycle increment

Keep the single reader/writer and existing state mutex. Four internal flags
separate local unbind request/response and peer unbind request/ACK completion.
Combining them into only Unbinding lost information during crossed shutdown.
Both obligations must finish before closing; Request(Unbind) now also waits for
that terminal decision. No public API or version-specific runtime is introduced.
EOF after all required incoming unbind information defers to the ACK writer so
peer closure cannot race its successful completion hook; other failures retain
the existing close policy. A small `session_shutdown.go` owns these predicates.

Release unread inputs and correlation maps at retirement, and write-job payloads
and captures at completion. Caller-owned results/handles remain valid objects,
but stale handles cannot write. Wait joins runtime loops only; applications join
their own calls. The [lifecycle contract](session-lifecycle.md) records these
boundaries and the independently authored local TCP peer matrix.

## Toolchain and dependency decision

Go 1.26 is the minimum; CI targets maintained Go 1.26/1.27. The
[official release history](https://go.dev/doc/devel/release) was checked on the
recorded date. Local validation uses installed Go 1.26.4, not both CI toolchains.

Prefer current standard-library APIs within that minimum, after checking
ownership, nil/empty semantics and lifecycle behavior. The
[Go usage audit](go-modernization.md) records the modernization choices and
retained forms; this is not permission to raise the minimum Go version implicitly.

On 2026-10-28 the owner first scheduled, then deferred, the Go 1.27 upgrade and
returned the focus to R1. The [upgrade assessment](go1.27-assessment.md) is retained
for later use; U1 is not a release prerequisite. The minimum remains 1.26 and no
Go 1.27 execution is claimed by the assessment. Any eventual upgrade must align
the module, CI, documentation and validation.

No third-party runtime dependency is adopted in this increment. Latin-1 and
UTF-16/UCS-2 use small strict conversions and standard-library Unicode helpers;
GSM's basic/extension alphabets are implemented from protocol definitions.
This keeps the initial narrow GSM scope within the library maintenance boundary.
The library owns maintenance of these conversions and rejects unknown mappings
and malformed escapes/surrogates instead of replacing characters. This decision
is not a ban on dependencies: additional alphabets/packing should reconsider
x/text or another pinned candidate only after license, maintenance, boundary,
error and Go-version review. BCD is not applied to SMPP address strings.

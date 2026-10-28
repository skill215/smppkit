# R2 public SMS contract audit

Completed locally on 2026-10-28 with Go 1.26.4. This is the R2 audit of the
existing SMS subset, not a 1.0 release or a full SMPP conformance certificate.
No exported signature, wire behavior, dependency or architecture changed.
The only executable change is periodic expiry in the client example during busy
receive traffic. Library changes clarify comments; new tests cover gaps below.
R3 candidate validation and R4 final release/API approval remain separate gates.

## Exported entry points and value contracts

The inventory covers the six public packages. PDU marker methods are grouped by
their command family rather than treated as separate transport operations.
Struct fields, enum constants and sentinels are read together with their entry
points. `internal/wire` is not a public API; `codec.FieldError` is its public alias.

| Surface reviewed | Contract and existing evidence |
| --- | --- |
| `smpp.Dial`, `ClientConfig`; `NewSession` | Dial owns and closes newly opened transports on failure; NewSession transfers an existing connection only on success. TLS configuration is cloned. Explicit bind/profile, no reconnect/fallback. `TestTLSClientServer`, `TestAllVersionBindRoles`, `TestBindRejectionTimeoutAndUnboundRequest`. |
| `NewServer`, `Server.Accept`, `Outbind`, `Serve`; `ServerConfig`, `BindInfo`, `BindDecision` | Profiles are frozen, authentication is concurrent trusted code and must honor context. Accept/Outbind take transport ownership even on failure; direct callers bound concurrency. Serve bounds connections including handshakes and joins handlers. Outbind supports RX in 3.4/5.0 only. `TestOutbindPublicFlow`, `TestOutbindIndependentESME`, `TestOutbindFailureBoundaries`, TCP/TLS tests. |
| `Session.Bind`, `Request`; `Role`, `State`, `Config`, `Result`, `Outcome`, `PeerError.Error` and session sentinels | Role/version policy below; borrowed inputs must not change until return. Result preserves final writer byte count and a correlated response even on error. A response can win cancellation; crossed unbind also needs the ACK write. Request never retries. `session_r1_test.go`, `session_lifecycle_test.go`, `TestPartialWriteAndQueuedCancellation`, `TestGenericNACKAndRespondOnce`. |
| `Session.Receive`, `Inbound.Respond` | One inbound consumer; returned PDU is owned. Validation/cancel-before-claim permits correction; claimed response handles cannot be retried. Original command, sequence and data mode survive edits. Close discards unread inputs. `TestResponseSemanticsLeaveHandleCorrectable`, `TestReviewInboundResponseIdentitySurvivesPayloadEdits`, `TestDataTransactionSessionSnapshot`, lifecycle tests. |
| `Session.Notify`, `ReceiveNotification` | One independent notification consumer; owned notices and no ACK. Notify uses a separate bounded admission queue and shared sequence/writer; success means Written. Outbind credentials require application validation. `session_notification_test.go`, `server_outbind_test.go`. |
| `Session.Codec`, `State`, `TLVEnabled`, `Done`, `Err`, `Close`, `Wait`, `Shutdown` | Immutable Codec, synchronized snapshots, first terminal cause. Done signals shutdown start; Close aborts, Wait joins runtime only, Shutdown attempts unbind then closes. They do not drain application work. `TestR1ResponseTLVPolicy`, bind and lifecycle suites. |
| `Session.SendText`, `TextMessage`, `SendResult`, `SegmentResult` | Whole-input and all-part preflight precedes writes and reference reservation. Sequential parts, stop at first error, every planned part retained. Preparation failure has no segment plan; after preparation, unattempted entries have zero sequence/bytes and nil per-part error, with the top-level error explaining the stop. Existing messaging/R1/review tests plus new transport/reference tests below. |
| `AdmitMessage`, `Session.RunReceiver` | Commit bounded memory before OK; write failure does not roll back content. Capacity rejection returns Throttled; other metadata/conflict rejection uses InvalidOptionalParameter. A response-write error takes precedence over admission error. Explicit transaction submissions are refused before admission/ACK. RunReceiver is the sole consumer and closes on exit; expiry ticker is checked between admissions, so ACK writes may delay it. RV21 and new ACK/namespace tests below. |
| `codec.New`, `Config`, `Codec.Version`, `MaxFrame`, `ResponseCommand` | Required exact version, frozen metadata, frame cap includes header. ResponseCommand excludes one-way, unsupported and unregistered private commands. `TestVersionRules`, `TestScopedPrivateCommand`, mandatory layouts and notification tests. |
| `Codec.Read`, `Decode`, `Append`, `ReadBind`; `FieldError`, codec sentinels | Read acquires one bounded frame; caller owns deadlines. Decode requires exactly one frame and copies input. Standard byte fields have cap equal to len (RV20). Append preserves the existing destination prefix/length on error. Structural field errors, validation/unsupported errors and transport errors are distinct; not every malformed input wraps ErrValidation. Failed decode output is diagnostic only. `TestBoundariesAndOwnership`, `TestRV20DecodedFieldIsolation`, independent layouts, bootstrap/semantics tests. |
| `Codec.ValidateDataDirection`, `ValidateDataResponse`, `DataDirection` | Supplemental checks, not replacements for Codec parsing. Direction is the original request direction, including responses. Ordinary Codec accepts the union; Session supplies original direction/mode automatically. Unknown defaults and customer semantics are not guessed. Both `codec/data_*_test.go` and `session_data*_test.go`. |
| `CommandCodec`, `CommandDefinition`, `TLVDefinition` | Private request/response pairs and customer tags are scoped; standard/reserved definitions cannot be overridden. Metadata copied, callbacks shared and concurrency safe, no retention/mutation of borrowed input. Arbitrary extension Go code must bound its own work and allocation; the library cannot enforce internal payload semantics. Extension tests and compilable examples. |
| `message.Prepare`, `GSMText`, `Text`, `Policy`, `Mode`, `InformationElement` | Explicit peer encoding/radio policy, no I/O. Fallback handles encoding failure, not incompatible version policy. Prepare is not final Codec validation. Custom DCS mapping is trusted; built-in Latin1 is excluded in 3.3. `TestR1EncodingVersionPolicy`, `TestPreparationIndependentBoundaries`, additional UDH/SAR tests and `FuzzPrepareVersionEncoding`. |
| `Plan.Encoding`, `DataCoding`, `Mode`, `Count`, `Multipart`, `ReferenceBits`, `Preview`, `Materialize` | Immutable snapshots; owned preview/materialized copies. Empty text has one part. Materialize does not reserve references. Only multipart plans require a lease; UDH8 rejects references above 255. `TestReviewPreparedPlanFreezesEncodingMetadata`, preparation/ownership tests and new reference boundary test. |
| `NewReferences`, `References.Bits`, `Reserve` | Concurrent bounded allocator; caller uses a consistent clock. Shared only within the application's chosen sending identity. No early release on ACK/cancel; expiry at the retention deadline. Existing lease test plus new full 8-bit space and cross-session capacity tests. |
| `ParseSegment`, `Segment`, `Concatenation` | Copies fields and extracts payload/metadata; receipts bypass concatenation. It is not versioned wire validation. Raw payload and ordered provenance remain accessible. Existing malformed metadata, port isolation, ownership and UDH fuzz cases. |
| `NewReceiver`, `ReceiveConfig`, `AckPolicy`; `Receiver.Admit`, `Receive`, `Expire`, `Events`, `Stats`; `Admission`, `Message`, `Event`, `Stats` and message sentinels | Concurrent bounded memory admission; fixed first-admission expiry, bounded completion/tombstone/event queues, explicit namespace. No automatic goroutines. Receive transfers charges and decodes outside the lock; DecodeError preserves raw content, and an available message precedes cancellation. Encoding map copied, providers shared. Existing duplicate/conflict/capacity/concurrency/resource tests plus new raw-decode and namespace tests. |
| `textcodec.Encoding`, `Encoded`, `Alphabet.Name`, `DataCoding`, `UnitBits`, `Encode`, `Decode`, `Validate`, `ErrEncoding` | Strict built-ins, owned encoding output, borrowed decode input without retention/mutation. UCS2 rejects supplementary characters; UTF16BE explicitly permits pairs. Validate checks offsets, not alphabet/DCS validity. Custom callbacks are synchronous trusted code. `TestIndependentCharsets`, `TestStrictErrors`, preparation tests and `FuzzCharsets`. |
| `tlv.Fields.First`, `All`, `Clone`, `Append`, `ReplaceFirst`, `DeleteAll`; `Field.Uint16`, `Uint16`, `Field`, `Fields`, tag constants | Order and duplicate preservation. First/All borrow values; Clone/Append/ReplaceFirst copy. DeleteAll reuses remaining values. Mutation requires caller synchronization. Uint16 requires exactly two network-order bytes. New focused container tests and existing Codec TLV inventory tests. |
| `pdu.Version.Valid`, `CommandID.IsResponse`, `Body`, `PDU`, `Address`, `Status`, command/version/status constants | Values do not validate themselves. Command identity comes from the body; Codec derives lengths and checks fields/version. IsResponse tests the bit, not command support. Request/Notify inputs borrow slices; decoded values are owned. Independent version/layout/semantic fixtures cover the supported command families. |
| PDU `CommandID` methods | BindReceiver/Transmitter/Transceiver and their responses; SubmitSM/DeliverSM/DataSM and their responses; SubmitMulti/Resp; QuerySM/Resp, ReplaceSM/Resp, CancelSM/Resp; Unbind/Resp, EnquireLink/Resp, GenericNACK, Outbind, AlertNotification; Opaque and Omitted. Concrete standard bodies only; opaque private bytes and omitted negative bodies cannot bypass standard checks. Layout, multi, notification, extension and negative-body tests. |
| `BindFields`, `BindResponseFields`, `SMFields.Clone`, `MessageResponseFields`, `MultiDestination`, `DestinationKind`, `UnsuccessfulSME`, `SubmitMulti.Clone`, `SubmitMultiResp.Clone` | Plain value structs; ordinary assignment is shallow. Clone methods deep-copy mutable fields. IDs remain opaque ASCII; counts/lengths derive from contents. Destination discriminator is explicit; header OK may coexist with per-SME failures. `TestSubmitMultiOwnership`, independent multi vectors and clone/ownership tests. |

Construct runtime/profile/planner objects through their constructors; zero values
of Session, Server, Codec, Plan, Receiver and References are not ready to use.
Contexts and interface implementations must be usable/non-nil. No blanket
nil-receiver or arbitrary faulty-extension recovery contract is advertised.
Value structs and slices are not implicitly synchronized. Session operations
may run concurrently, subject to the one-consumer rules and input ownership.
The earlier decision text overstated callback isolation: authentication and
message consumers run outside the reader, but trusted Codec extension callbacks
run synchronously during parsing, including on Session's reader. Their GoDoc
and the decision record now explicitly require bounded work without waiting for
traffic on the same Session. Frame limits do not sandbox extension allocations.

## Version, role and validation reconciliation

The [command matrix](support.md) agrees with `Codec.supported`, typed body
encoders/decoders and `Session.allowed`. The [53-tag inventory](tlv-support.md)
matches the standard registry's tags, widths, command masks and repeatability;
special value/dependency rules remain in its accompanying text and tests.

| Bound request direction | ESME role | Versions / constraints |
| --- | --- | --- |
| ESME → MC: submit_sm, submit_multi, query_sm, replace_sm, cancel_sm | TX or TRX | All profiles; TRX itself is absent in 3.3 |
| MC → ESME: deliver_sm | RX or TRX | All profiles |
| data_sm submission / delivery | TX/TRX / RX/TRX respectively | 3.4/5.0, contextual direction and original-mode checks |
| alert_notification, MC → ESME | RX or TRX | 3.4/5.0, Notify only; no response |
| enquire_link / unbind | Any supported bound role, either direction | All profiles; received control requests handled automatically |
| Registered private pair | Any supported bound role, either direction | Only versions activated in that Codec; extension owns payload semantics |

Bind and receiver outbind are bootstrap operations rather than bound messaging.
Generic NACK is a response, not an outbound Request operation. Version/layout
checks still run even when a role permits an operation. A missing 3.4/5.0 bind
advertisement disables optional fields in Session, including correlated
responses; it does not downgrade the standalone Codec's version.

The advertised subset deliberately excludes full wireless-network validation,
unknown peer defaults, general transaction/receipt business relationships and
provider-specific extensions. The contextual transaction checks are for data_sm;
they are not a general delivery engine or complete transaction validation for
submit_sm/submit_multi. Automatic memory ACK refuses explicit transaction
submissions, including 3.4 submit_sm mode 2; mode 0 requires a configured
non-transaction default. Low-level Receive/Respond remains available.
Ordinary submission OK means acceptance. Explicit transaction responses report
the peer's synchronous outcome; applications interpret that claim themselves.

No excluded command was promoted to supported, and no API rename or extra
service abstraction was needed. Broadcast, historical 3.3 commands, standard
3.3 multipart, packed GSM/national tables, receipt dialect parsing/aggregation,
persistence and external interoperability retain their documented boundaries.

## Gaps closed and validation

Existing role/version, byte-vector, TLV, R1, RV20/RV21, reassembly and lifecycle
tests were reused. New cases are limited to uncovered public boundaries:

- `messaging_contract_test.go`: accepted first segment followed by zero-byte,
  partial or fully-written/no-response failure; all three segment results and
  exactly two writes. Shared allocator exhaustion across two bound sessions.
  ACK-time queue observation, no rollback after failed ACK, slow-consumer
  Throttled response/recovery, default versus explicit cross-session namespaces.
- `message/message_test.go`: all 256 references concurrently reserved including
  zero, exhaustion/expiry and UDH8 255/256 boundary; copied encoding map and
  admitted data, raw payload on decode error, queue-before-cancellation behavior.
- `tlv/tlv_test.go`: borrowed accessors, copying mutation helpers, duplicate/order
  preservation, nil/empty Clone distinction, exact endian and width checks.
- `examples/client/main_test.go`: expired groups are swept and events drained
  before accepting ready live traffic. The example previously swept only after
  an idle Receive timeout; it now sweeps between receives, including busy traffic.

Two existing tests were strengthened in place: invalid-field SendText preflight
now asserts that every prepared part stays NotAttempted without sequence/queue
admission; peer-rejection coverage checks the accepted/rejected/unsent results
and proves no third request or retry is scheduled. The idle-only example behavior
was also restored in a temporary overlay: the new regression fails there and
passes on the fixed source, without reverting the working tree.

The independent 19-byte success response in the send-failure test is
`00000013 80000004 00000000 00000001 6f6b00` (message ID `ok`). Namespace tests
use independently composed submit_sm bytes with UDH8 `05 00 03 07 02 01/02`.
These extend the existing [independent fixtures](wire-samples.md); shared-Codec
example exchanges are supplemental integration evidence.

See [R2 validation](validation.md#r2-contract-validation) for commands and actual
results. The example audit covers finite operation/cleanup deadlines, ACK write
completion before publishing delivery, separate server completion consumption,
Close/Wait and worker joins, and server cancellation. The example does not add
crash recovery or claim that echoed text is a delivery receipt.

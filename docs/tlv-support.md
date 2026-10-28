# Standard SMS TLV support

Implementation preview, 2026-10-27. The table covers 53 standard, non-broadcast
SMS parameters (44 from 3.4 and nine introduced in 5.0). Both encoding and
decoding enforce these bounds and applicability. **3.3 accepts no TLVs**,
including customer and unknown tags. This inventory is not a claim of full
wireless-network or message-mode conformance.

Sources: [SMPP 3.4 issue 1.2](https://smpp.org/SMPP_v3_4_Issue1_2.pdf) command
tables in §4 and definitions in §5.3; [SMPP 5.0](https://smpp.org/SMPP_v5.pdf)
§§4.2.4–4.2.5, 4.3.3–4.3.4 and 4.8.4. Protocol discrepancies and API choices
are recorded in [decisions](decisions.md#submit_multi-and-standard-tlvs-next-messaging-increment).

Command keys: S = submit_sm, M = submit_multi, D = deliver_sm, A = data_sm;
a lowercase r suffix denotes the response. B = bind responses; N =
alert_notification; R = query/replace/cancel/unbind/enquire responses and
generic_nack. `—` means rejected. Ordinary Codec encoding/decoding accepts the
DataSM submission/delivery union. Session narrows it automatically; standalone
users supply explicit context with `Codec.ValidateDataDirection`. See
[data_sm direction rules](data-direction.md), including the 3.4/5.0 differences.

Widths count **value bytes**, excluding the four-byte TLV header. C-octet widths
include the terminating NUL. Repeated callback families are marked `*`; all
other listed tags permit only one occurrence. Raw unknown tags retain order and
duplicates. Customer definitions are scoped to a Codec, cannot override standard
or reserved tags, and are applied after framing and width limits.

| Tag | Go constant in tlv | Value bytes (3.4 / 5.0 where different) | 3.4 commands | 5.0 commands |
| --- | --- | --- | --- | --- |
| `0x0005` | `DestAddrSubunit` | 1 | S, M, A | S, M, D, A |
| `0x0006` | `DestNetworkType` | 1 | A | S, M, A |
| `0x0007` | `DestBearerType` | 1 | A | S, M, A |
| `0x0008` | `DestTelematicsID` | 2 | A | S, M, A |
| `0x000d` | `SourceAddrSubunit` | 1 | S, M, A | S, M, D, A |
| `0x000e` | `SourceNetworkType` | 1 | A | S, M, A |
| `0x000f` | `SourceBearerType` | 1 | A | S, M, A |
| `0x0010` | `SourceTelematicsID` | 1 | A | S, M, A |
| `0x0017` | `QOSTimeToLive` | 4 | A | S, M, A |
| `0x0019` | `PayloadType` | 1 | S, M, D, A | S, M, D, A |
| `0x001d` | `AdditionalStatusInfoText` | 1..256 | Ar | Sr, Mr, Dr, Ar |
| `0x001e` | `ReceiptedMessageID` | 1..65 | D, A | D, A |
| `0x0030` | `MSMsgWaitFacilities` | 1 | S, M, A | S, M, A |
| `0x0201` | `PrivacyIndicator` | 1 | S, M, D, A | S, M, D, A |
| `0x0202` | `SourceSubaddress` | 2..23 | S, M, D, A | S, M, D, A |
| `0x0203` | `DestSubaddress` | 2..23 | S, D, A | S, D, A |
| `0x0204` | `UserMessageReference` | 2 | S, M, D, A | S, M, D, A |
| `0x0205` | `UserResponseCode` | 1 | S, D, A | S, M, D, A |
| `0x020a` | `SourcePort` | 2 | S, M, D, A | S, M, D, A |
| `0x020b` | `DestinationPort` | 2 | S, M, D, A | S, M, D, A |
| `0x020c` | `SARMsgRefNum` | 2 | S, M, D, A | S, M, D, A |
| `0x020d` | `LanguageIndicator` | 1 | S, M, D, A | S, M, D, A |
| `0x020e` | `SARTotalSegments` | 1 | S, M, D, A | S, M, D, A |
| `0x020f` | `SARSegmentSeqnum` | 1 | S, M, D, A | S, M, D, A |
| `0x0210` | `SCInterfaceVersion` | 1 | B | B |
| `0x0302` | `CallbackNumPresInd` * | 1 | S, M, A | S, M, D, A |
| `0x0303` | `CallbackNumATag` * | 1..65 | S, M, A | S, M, D, A |
| `0x0304` | `NumberOfMessages` | 1 | S, A | S, M, A |
| `0x0381` | `CallbackNum` * | 4..19 | S, M, D, A | S, M, D, A |
| `0x0420` | `DPFResult` | 1 | Ar | D, A, Sr, Mr, Ar |
| `0x0421` | `SetDPF` | 1 | A | S, M, A |
| `0x0422` | `MSAvailabilityStatus` | 1 | N | N |
| `0x0423` | `NetworkErrorCode` | 3 | D, A, Ar | D, A, Sr, Mr, Dr, Ar |
| `0x0424` | `MessagePayload` | 0..65535 | S, M, D, A | S, M, D, A |
| `0x0425` | `DeliveryFailureReason` | 1 | Ar | Sr, Mr, Dr, Ar |
| `0x0426` | `MoreMessagesToSend` | 1 | S, A | S, M, A |
| `0x0427` | `MessageState` | 1 | D, A | D, A |
| `0x0428` | `CongestionState` | 1 | — | Sr, Mr, Dr, Ar, B, R |
| `0x0501` | `USSDServiceOp` | 1 | S | S, M, D, A |
| `0x060b` | `BillingIdentification` | 1..1024 | — | S, M, A |
| `0x060d` | `SourceNetworkID` | 7..65 | — | S, M, D, A |
| `0x060e` | `DestNetworkID` | 7..65 | — | S, M, D, A |
| `0x060f` | `SourceNodeID` | 6 | — | S, M, D, A |
| `0x0610` | `DestNodeID` | 6 | — | S, M, D, A |
| `0x0611` | `DestAddrNPResolution` | 1 | — | S, M, D, A |
| `0x0612` | `DestAddrNPInformation` | 10 | — | S, M, D, A |
| `0x0613` | `DestAddrNPCountry` | 1..5 | — | S, M, D, A |
| `0x1201` | `DisplayTime` | 1 | S, M, A | S, M, A |
| `0x1203` | `SMSSignal` | 2 | S, M, A | S, M, A |
| `0x1204` | `MSValidity` | 1 / 1 or 4 | S, M, A | S, M, A |
| `0x130c` | `AlertOnMessageDelivery` | 0 / 0..1 | S, M, A | S, M, A |
| `0x1380` | `ITSReplyType` | 1 | S, A | S, M, D, A |
| `0x1383` | `ITSSessionInfo` | 2 | S, D, A | S, M, D, A |

## Value and dependency checks

- Scalar enumerations and reserved bits are checked where the SMPP definition
  specifies them: subunit, network/bearer type, payload type, message waiting,
  privacy, callback digit mode/TON/NPI/presentation, mailbox count, DPF flags,
  availability, delivery failure, congestion, display, validity, alert and ITS
  reply type. Source telematics ID is **one byte**; destination telematics ID
  is two, with a reserved zero second byte.
- `message_state` is 1..8 in 3.4, 0..9 in 5.0. `network_error_code` is three
  bytes; its network type is 1..3 in 3.4 and 1..8 in 5.0, while the actual error
  code remains opaque. Interface advertisements accept only 0x33/0x34/0x50;
  Session additionally requires the advertised version to match its profile.
- `alert_on_message_delivery` is empty in 3.4, empty or one byte (0..3) in 5.0.
  `ms_validity` uses one byte (0..3), or in 5.0 a four-byte relative form starting
  with 4, followed by unit 0..6 and a two-byte number. Unused intermediate
  lengths are rejected. Billing identification requires its format flag's MSB.
- Diagnostic text and network IDs must be NUL-terminated ASCII without embedded
  NULs. Receipt IDs are opaque ASCII C-octets. Node IDs are six decimal digit octets;
  NP information is ten decimal digit octets. Country is a raw 1..5-byte integer.
  A source/destination network ID requires its corresponding node ID. NP
  resolution 2 requires both NP information and country.
- SAR reference/total/sequence must occur together, with total and sequence
  nonzero and sequence no larger than total. SAR and port TLVs cannot accompany
  UDHI. `message_payload` conflicts with nonempty `short_message`. A 65,535-byte
  payload needs a raised MaxFrame to leave room for SMPP and TLV headers; the
  default 64 KiB cap still applies.
- Callback numbers may repeat. If alpha tags or presentation indicators are
  supplied, that family's count must equal the callback-number count. Within
  each family, wire occurrence order establishes association; fields are never
  sorted or coalesced. The alpha-tag DCS octet must have a defined SMPP coding
  assignment; alpha-tag text and callback digit content are not decoded by Codec.
- USSD vendor values 32..255 and externally assigned language values remain
  usable. Signal codes, user response codes, references, ports, payloads, network
  error meanings, subaddress contents and ITS session bytes remain opaque
  beyond their documented widths and discriminators. Network IDs are not
  resolved or verified against carrier directories. These checks do not enforce
  every transaction-mode or delivery-receipt recommendation. The additional
  [mandatory DCS/ESM-class checks](field-semantics.md) apply to message fields.

Known broadcast TLVs (0x0600..0x060a) are rejected on these commands; broadcast
commands remain unimplemented. Receiving congestion information does not start
an automatic rate controller or retry. A parameter being legal on a command
does not mean a particular peer supports or acts on it.

Independent tag/length/value literals for every listed tag, malformed cases,
version differences and a response-command sweep live in
[`codec/tlv_standard_test.go`](../codec/tlv_standard_test.go). Submit-multi
request/response literals are in [`codec/multi_test.go`](../codec/multi_test.go),
and a fragmented literal peer in [`session_multi_test.go`](../session_multi_test.go)
checks the full exchange independently of Codec.

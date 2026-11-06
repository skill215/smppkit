# Binary messaging (v1.2.0 development)

Added in **v1.2.0**. Requires Go 1.26. The existing Codec,
Session, Plan, References, SendResult and Receiver remain the shared foundation.
There is no separate binary transport, reference allocator or receive cache.

## Prepare and send

```go
plan, err := message.PrepareBytes(pdu.V50, message.Binary{
    Source: pdu.Address{Value: "example"},
    Destination: pdu.Address{TON: 1, NPI: 1, Value: "12345"},
    Data: []byte{0x00, 0xff, 0x80}, DataCoding: 4,
}, message.BinaryPolicy{Mode: message.UDH16})
```

Preparation performs no I/O and reserves no reference. `plan.Encoding()` is
`"binary"`; `plan.DataCoding()` reports the explicit numeric DCS. NUL, invalid
UTF-8 and every other octet are preserved. There is no string intermediary,
encoding, fallback or character-safe splitting promise for pre-encoded text.
`Preview` returns owned raw chunks; `Materialize(ref)` returns owned fields with
UDH/TLVs. Standalone users must validate those fields with the relevant Codec.

For a bound client or server Session:

```go
result, err := session.SendBinary(ctx, smpp.BinaryMessage{
    Source: pdu.Address{Value: "example"},
    Destination: pdu.Address{TON: 1, NPI: 1, Value: "12345"},
    Data: data, DataCoding: 4,
}, message.BinaryPolicy{Mode: message.UDH16}, references)
```

The optional `references` argument is an application-owned `*message.References`.
Omit it to use the Session's allocator. At most one override is accepted; for
multipart it must be non-nil and match the plan's reference width. Single-part
plans consume no reference. Share one allocator across text/binary sends and
connections in the same sending domain. Choose retention/capacity for the peer;
acceptance, cancellation and uncertain outcomes do not release a lease early.

The client uses `submit_sm`; the server uses `deliver_sm` under the same role and
bind rules as SendText. Preparation, all-segment Codec preflight and role/TLV
admission precede leasing and the first write. Requests execute sequentially
through the shared window. A failure stops later scheduling, retaining every
planned `SegmentResult`, including `NotAttempted` entries. `Encoding` in the
result is `"binary"`. Per-part IDs are opaque acceptance identifiers, not final
delivery. No automatic retry or mode fallback is performed.

## Version and mode policy

| Profile | Explicit DCS | High-level modes |
| --- | --- | --- |
| 3.3 | `0x04` | One header-free request; UDH8/UDH16 selection emits no header when single-part. No TLVs, SAR, Payload, AdditionalUDH or multipart |
| 3.4 / 5.0 | `0x02`, `0x04` | UDH8, UDH16, SAR, Payload, subject to negotiated TLV and command rules |

Zero DCS is rejected, not defaulted. The [DCS table and sources](binary-dcs.md)
explain the small whitelist. Raw Codec acceptance is unchanged.

With the default 140 radio-octet budget and no additional metadata:

| Mode | Single-part capacity | Multipart capacity | Reference width |
| --- | --- | --- | --- |
| UDH8 | 140 | 134 bytes per part | 8 |
| UDH16 | 140 | 133 bytes per part | 16 |
| SAR | 140 | 133 bytes per part | 16 |
| Payload | Up to 65535 bytes in one TLV | Peer owns radio segmentation | None |

UDH8/UDH16 emit concatenation only when needed. Additional UDH contributes its
IE bytes plus one shared UDHL octet; it can set UDHI on a single-part message.
Caller concatenation and national-language declarations are rejected; validated
8/16-bit application ports and opaque other IEs follow the existing UDH policy.
AdditionalUDH is not accepted with SAR or Payload. Port TLVs and additional UDH
cannot be combined. Multipart port TLVs require SAR, budgeting a further six
radio octets; a single-part port TLV conservatively budgets seven. A SAR header
is budgeted at the peer, not emitted into short_message.

## Limits, ownership and errors

| BinaryPolicy field | Zero/default | Accepted explicit range |
| --- | --- | --- |
| Mode | UDH16 | UDH8, UDH16, SAR, Payload |
| PayloadOctets | 140 | 1..140 |
| MaxInputBytes | 64 KiB | 1 byte..16 MiB |
| MaxOutputBytes | 256 KiB | 1 byte..16 MiB |
| MaxSegments | 255 | 1..255 |

Empty input produces one request, including an empty message_payload for Payload.
Limits reject rather than truncate. MaxInputBytes checks the original Data slice
before copying. MaxOutputBytes is an accounting budget for one materialization:

- 256 bytes per part plus the source, destination and service string lengths;
- all payload and encoded header bytes;
- 32 bytes plus value length for each caller/generated TLV in every part.

For example, 141 bytes split with UDH16 and empty addressing cost
`141 + 2*(256+7) = 667` accounted bytes. Adding a two-byte customer TLV costs
another `2*(32+2) = 68`. Repeated metadata and generated SAR/payload TLVs therefore
cannot bypass the budget. Checks occur before copying Data or optional values.
The small additional UDH (at most 248 IE bytes) may be copied during validation.
This accounting is not an exact heap/RSS ceiling: allocator rounding, the Plan,
temporary Codec/send buffers and multiple caller-retained results are separate.

PrepareBytes snapshots Data once and stores private chunk views into that copy;
it clones optional values and additional UDH. Preview and each Materialize call
copy their outputs. Session additionally materializes/preflights and, for
multipart, materializes with the leased reference before Request encoding.
There is no zero-copy claim. Callers must not mutate inputs concurrently with
preparation/sending. An immutable Plan can be shared by concurrent readers.

| Failure | Contract |
| --- | --- |
| Invalid profile/DCS, generated-tag conflict, incompatible mode/version/ports | `message.ErrMetadata` |
| Invalid numeric policy or exceeded input/output/segment budget | `message.ErrCapacity` |
| UDH IE 0x24/0x25 | `message.ErrUnsupportedNationalLanguage` |
| Command/address/TLV/frame preflight | Existing Codec errors, including semantic diagnostics where applicable |
| Role/bind/TLV negotiation | `smpp.ErrState` |
| Send cancellation, peer rejection or transport failure | Existing Request causes and per-part outcomes |

Preparation is not final Codec validation. For example, a 65535-byte Payload is
valid at the TLV length level but exceeds the default 64 KiB **total frame** limit
once mandatory fields and headers are included. SendBinary rejects before
writing; an explicitly larger Codec MaxFrame is needed if the peer supports it.
This does not weaken negotiated TLV admission. Missing bind-version negotiation
continues to prohibit TLV-based sends; no compatibility exception was added.

## Receive and examples

Use the existing `message.Receiver` with `AckOnBuffered`, without a text decoder
for DCS 2/4. Consume `Message.Payload` directly; `Decoded` remains false and a
raw-only message has no charset DecodeError. Memory admission ACKs each part;
it does not establish business processing, persistence or handset delivery.
Application-owned ACK decisions still use Session.Receive/Inbound.Respond.

- [Preparation example](../examples/binary/main.go): `go run ./examples/binary`.
- [Client/server usage](../examples/README.md#binary-submit-and-echo-v120-development):
  raw hex input, bounded reassembly and binary deliver_sm echo.
- [Independent wire fixtures](wire-samples.md#v120-binary-message-samples-unreleased).
- [Allocation benchmarks](benchmarks/README.md#binary-messaging-v120-development).
- [Executed F2 checks and limits](v1.2.0-f2-validation.md).

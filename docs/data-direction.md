# data_sm direction validation

Implementation preview, 2026-10-27. SMPP 3.4 and 5.0 use the same command ID
for submission (ESME to MC) and delivery (MC to ESME). The request direction
also determines which fields its response may carry. SMPP 3.3 has no data_sm.

Session automatically applies these checks to outgoing requests, incoming
requests, outgoing responses and correlated incoming responses. The shared
Codec owns the rules; Session only supplies its immutable endpoint role and
whether the packet is incoming or outgoing. TX/RX/TRX bind eligibility remains
a separate command check. One Codec can serve both endpoints concurrently.

For a standalone simulator, ordinary `Append`/`Decode` retain the union of
submission and delivery fields. Supply context explicitly with the additive
`Codec.ValidateDataDirection(body, direction)` API:

```go
packet, err := c.Decode(frame)
if err != nil { return err }
if err := c.ValidateDataDirection(packet.Body, codec.DataDelivery); err != nil {
    return err
}
```

This is a supplemental check, not a replacement for normal encoding/decoding.
Always run `Append` or `Decode` to check framing, widths, values, dependencies,
header status and registered customer definitions. Before sending encoded bytes,
complete both checks. Context validation does not mutate the body, change the
Codec's configuration or invoke customer callbacks. An independently compiled,
runnable example is [examples/data-direction](../examples/data-direction/main.go).

## Direction is the original request's direction

| Packet and endpoint operation | Context |
| --- | --- |
| ESME sends / MC receives data_sm | `codec.DataSubmission` |
| MC sends / ESME receives its data_sm_resp | `codec.DataSubmission` |
| MC sends / ESME receives data_sm | `codec.DataDelivery` |
| ESME sends / MC receives its data_sm_resp | `codec.DataDelivery` |

Response transport direction is reversed; its validation context is not.
Session's captured response command identity remains authoritative even if the
application edits the owned `Inbound.PDU`. data_sm_resp message IDs remain
opaque bounded ASCII strings; delivery responses are not forced to have an
empty ID by importing deliver_sm_resp's separate rule.

## Version-specific rules

The sources are [SMPP 3.4 issue 1.2](https://smpp.org/SMPP_v3_4_Issue1_2.pdf)
§§4.7, 5.2.12, 5.3.2.9/12/28/29/31/34/35 and
[SMPP 5.0](https://smpp.org/SMPP_v5.pdf) §§4.2.4–4.2.5, 4.3.3–4.3.4, 4.7.12.

For **3.4**, the shared data_sm optional-parameter table is narrowed only by
explicit directional descriptions:

| Field | Submission | Delivery |
| --- | --- | --- |
| esm_class message-type bits | 0x00, 0x08, 0x10 | Also 0x04, 0x18, 0x20 |
| Request receipted_message_id, message_state, network_error_code | Rejected | Allowed |
| Request set_dpf, more_messages_to_send | Allowed | Rejected |
| Response dpf_result | Allowed | Rejected |

Other 3.4 fields retain their normal command-level rules in both directions.
In particular, qos_time_to_live describes retention by the receiving system;
TTL, network/bearer metadata and callback metadata are not restricted using
5.0's later tables. The ignored low delivery mode bits are preserved.

For **5.0**, contextual optional-parameter checks reuse the existing standard
submission/delivery request and response tables, without copying a second TLV
catalog. See [TLV support](tlv-support.md): the `S`/`D` columns determine request
direction, and `Sr`/`Dr` determine response direction, in addition to ordinary
`A`/`Ar` applicability. Examples:

- Submission can carry set_dpf, more_messages_to_send and qos_time_to_live;
  delivery can carry receipt metadata and dpf_result.
- dpf_result is valid in a submission response, not a delivery response.
- Diagnostic response fields remain available in both directions.
- All six message types listed above are allowed in both directions by 5.0's
  esm_class table. 3.4's narrower submission rule is not imposed on 5.0.

Unknown/customer TLVs receive no inferred direction restriction. Normal
registered-definition checks still apply. A directionless union can contain
individually valid fields whose combination is invalid in both directions;
standalone users must supply context when they need this guarantee.

## Failure behavior and limits

Local direction failures return `codec.ErrValidation` before request sequence
allocation or writing. An invalid outgoing response leaves the inbound handle
available for correction. Invalid incoming requests and correlated responses
follow Session's existing malformed-frame close policy: they are not admitted
or reported as successful responses. Late/unmatched responses are still ignored
after ordinary decoding, before contextual checks, under the existing policy.

Omitted negative data_sm_resp bodies and Generic NACK retain their existing
status/correlation behavior. A valid response is not proof of final delivery;
no uncertain request is retried automatically.

The subsequent [transaction-context increment](data-transaction.md) adds
datagram registered-delivery checks and response-mode validation through
`Codec.ValidateDataResponse`, automatically used by Session. Receipt completeness,
operator provisioning and wireless-network payload semantics still need explicit
policy beyond these checks. There is no external SMSC conformance claim. Tests
include independent raw request/response peers, full octet sweeps, shared Codec
concurrency and literal protocol samples; see [validation](validation.md) and
[annotated wire samples](wire-samples.md).

# data_sm transaction context

Implementation preview, 2026-10-27. These checks extend the existing
[direction validation](data-direction.md) for SMPP 3.4/5.0. They use the same
Codec and shared Session. SMPP 3.3 still has no data_sm or TLVs.

## Implemented rules

The low two bits of an **ESME submission** select the messaging mode:

| Bits | Meaning | Validation |
| --- | --- | --- |
| 0 | MC default | Preserve; its actual mode is unknown without peer configuration |
| 1 | Datagram | registered_delivery must be zero; transaction-only response TLVs rejected |
| 2 | Transaction | Transaction response TLVs allowed; OK response cannot carry delivery_failure_reason |
| 3 | Store and forward | Transaction-only response TLVs rejected |

The ESM message-type and GSM feature bits remain independent. Existing scalar,
TLV value, width, dependency and direction checks still apply.

For submission responses, transaction-only parameters differ by version:

| Response TLV | 3.4 | 5.0 |
| --- | --- | --- |
| delivery_failure_reason | Transaction only | Transaction only |
| network_error_code | Transaction only | Transaction only |
| dpf_result | Transaction only | Transaction only |
| additional_status_info_text | Any mode | Transaction only |
| congestion_state | Unsupported | Any mode |
| Unknown/customer TLVs | No inferred mode restriction | No inferred mode restriction |

The distinction follows [SMPP 3.4](https://smpp.org/SMPP_v3_4_Issue1_2.pdf)
§§2.10.2–2.10.3, 4.7.2 Table 4-21 footnote a, 5.3.2.28/33 and
[SMPP 5.0](https://smpp.org/SMPP_v5.pdf) §§4.2.5, 4.2.10.3–4.2.10.4,
4.8.4.18/19/32. The 5.0 submission-response table explicitly limits all of its
listed TLVs to transaction mode, including diagnostic text. congestion_state is
defined separately for any response and does not inherit that restriction.

Only known explicit modes are narrowed. Mode zero is not silently interpreted
as store-and-forward. Direction/mode checks do not require optional parameters
to be present and do not restrict arbitrary nonzero peer status codes to a
selected whitelist. Omitted negative responses and Generic NACK remain valid
under the ordinary Codec's existing status/body rules.

## Standalone Codec API

`ValidateDataDirection` now rejects nonzero registered_delivery on datagram
submission. For responses, use:

```go
err := c.ValidateDataResponse(response, savedRequestESMClass, codec.DataSubmission)
```

`response` is a `pdu.PDU`, so the method can inspect command_status. It includes
the existing direction checks. Save the original request's ESM class before
giving its owned body to application code. The direction describes the request,
even though the response travels in the opposite transport direction.

This method supplements normal Append/Decode, which still validate framing,
field widths, values and customer definitions. It does not correlate sequence
numbers or re-run user callbacks. A standalone runtime owns correlation and
must perform both ordinary and contextual validation before accepting/sending a
response. See the runnable [data-transaction example](../examples/data-transaction/main.go).

Ordinary encoding/decoding remains usable without any request history. A literal
response can be structurally legal but rejected with a particular request mode.
Do not use direction-only response validation when mode validation is required.

## Session behavior and resource bounds

Session stores one scalar ESM byte alongside existing pending requests and
inbound handles. It neither retains the request payload nor creates another
map, goroutine or timer. Existing window/inbound limits and lifecycle cleanup
bound this metadata. The snapshot is immutable even if application code edits
the owned Inbound.PDU, including replacing its body.

Local request failures happen before sequence allocation or writing. Invalid
outgoing responses do not claim the handle; the application can correct them.
Incoming request violations close the session before admission. Violations in
live correlated responses use the existing malformed-frame close policy and
cannot produce ResponseReceived. A fully written request remains uncertain;
there is no automatic retry. Late/unmatched responses remain ignored after
ordinary decoding and do not regain a request context.

## Interpretation boundaries

Delivery request mode bits do not recover the original submission's messaging
mode: 3.4 explicitly ignores them on delivery, and 5.0 has a separate delivery
response table. The library therefore does not apply submission-mode exclusions
to MC-to-ESME requests or their responses. In particular, a 5.0 receipt carrying
dpf_result is not rejected because the receipt itself has mode zero. Existing
direction checks still reject dpf_result on a delivery response.

`delivery_failure_reason` is excluded on a successful delivery attempt. For an
explicit transaction submission, OK denotes the synchronous delivery outcome,
so the combination is rejected. For unresolved default mode or a delivery
exchange, the library does not infer that command_status OK establishes handset
delivery. It does not cross-check opaque network error meanings or require DPF
to match set_dpf/carrier state. Peer default-mode resolution, receipt completeness
and cross-message correlation remain application policy.

ResponseReceived records protocol observation. A valid transaction response may
report failure (for example status 0xfe), which Session retains and returns as
PeerError. A server is responsible for actually implementing the delivery
semantics it advertises; in-memory admission alone cannot promise handset
delivery. No new final-delivery tracking, durable ACK or retry behavior is added.

## Automatic buffered ACK boundary (RV21)

`AdmitMessage` cannot implement synchronous transaction delivery. On a server
Session it refuses explicit mode 2 on data_sm or submit_sm before admission or
ACK, returning an error wrapping `smpp.ErrState`. The handle is still available
for `Respond`; applications supporting transaction mode use `Receive/Respond`
and supply their actual outcome. `RunReceiver` returns that error and closes the
session without ACK or queued message. The check also rejects mode 2 on 3.4
submit_sm in this helper rather than treating the unsupported combination as
ordinary memory acceptance; standalone Codec/Request behavior is unchanged.

The check uses the original request identity and ESM class, not an edited PDU.
Mode 0 is still the configured MC default: users of memory ACK must choose a
non-transaction default. No peer default is guessed. The restriction does not
apply to MC-to-ESME data_sm/deliver_sm mode bits or to 3.3's legacy delivery flags.
This is an admission-policy boundary, not a new delivery or persistence system.

See [independent samples](wire-samples.md) and the current
[validation record](validation.md). No external SMSC conformance is claimed.

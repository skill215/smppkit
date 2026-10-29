# Delivery-receipt helpers

Available in **v1.1.0**, Go 1.26. `message.ParseReceipt`
and `message.BuildReceipt` are bounded, stateless helpers. They do not alter the
versioned Codec, shared Session, reassembly or existing ACK behavior.

## Standard behavior and gateway conventions

[SMPP 3.4 §4.6 and Appendix B](https://smpp.org/SMPP_v3_4_Issue1_2.pdf#page=167)
allow receipts in deliver_sm/data_sm; Appendix B describes an example text format,
not a mandatory universal grammar. Standard receipt TLVs carry an opaque message
ID and numeric state. [SMPP 5.0 §4.3.5](https://smpp.org/SMPP_v5.pdf#page=90)
distinguishes MC receipts from intermediate notifications and describes receipt
addresses and TLVs. `network_error_code` is a network type plus a big-endian
16-bit network-specific error, not a status-byte/error-byte pair.

[SMPP 3.3 §6.3.5](https://smpp.org/SMPP-v3.3.pdf#page=24) delegates receipt marker
assignment and text detail to its Applications Guide. This helper's 3.3 marker
`0x04` and key/value text are explicit legacy peer conventions. 3.3 has no TLVs
and no data_sm. The helper never constructs either for 3.3.

Gateway status mapping, proprietary error byte layouts, ID generation/radix
conversion, number normalization and timestamp timezone/century belong in the
adapter. The parser does not search ordinary message text for receipt-looking
words. A submit response, a later report's content and the ACK for that report
are three distinct observations. None creates an implicit delivery system.

## Parse explicit evidence

```go
report, err := message.ParseReceipt(in.PDU.Body, message.ReceiptParseOptions{
    Version: session.Codec().Version(),
    Dialect: message.ReceiptKeyValue,
})
if err != nil {
    // Application decides how to handle/ACK unsupported input.
    return err
}
for _, diagnostic := range report.Diagnostics {
    // Safe categories and field names only; never contains input values.
    log.Printf("receipt diagnostic: %+v", diagnostic)
}
// Text and TLVs are deliberately separate: there is no preferred/merged ID.
if report.TLV.HasMessageID {
    consumeOpaqueID(report.TLV.MessageID)
}
```

Use the [compiling Client/Server example](../examples/receipts/main.go) for the
complete exchange and explicit response. The snippet's application callbacks
are illustrative; the library supplies no consumer, storage or report handler.

Only typed DeliverSM/DataSM with message type `MCReceipt` (`0x04`) or
`IntermediateNotification` (`0x20`, 3.4/5.0) qualify. The existing receiver's
broader `Message.Receipt` classification is unchanged; the new helper requires
an explicit type marker, even if an ID TLV exists. Kind is retained independently
of state and is not a boolean assertion of successful or final delivery.

| Returned evidence | Contract |
| --- | --- |
| Texts | Separate short_message and unique message_payload carriers; owned encoded Raw plus decoded ordered Fields |
| Text.Value(name) | Unique occurrence only; false for absent or duplicate fields; empty text can be present |
| TLV.MessageID / HasMessageID | Unique, structurally valid C-octet value; leading zeros and case retained |
| TLV.State / HasState | Unique one-byte state, including unsupported numbers with a diagnostic |
| TLV.NetworkError | Unique three-byte network type and 16-bit code; no comparison with text err |
| Optional | Owned ordered copies of all TLVs, including unknown/customer, duplicate and malformed input values |
| Diagnostics | Missing, duplicate, malformed, unsupported or conflict; fixed field names and carrier/TLV index |

The input is never mutated. Optional values and Texts.Raw do not alias input or
one another. Strings are immutable. Raw payloads and parsed fields may contain
sensitive data: only diagnostics are designed to exclude values.

Field problems return a result and diagnostics with **nil error**. Inspect them
before using evidence. `Value` can return a unique but malformed/unsupported value;
it is a presence accessor, not a validation result. Duplicate TLVs do not populate
the convenient TLV value; their originals stay in Optional. Duplicate payload TLVs
are not chosen for text parsing. Two simultaneous payload carriers are reported
as conflicting and parsed separately when individually supported.

Only comparable evidence is compared: opaque IDs byte-for-byte and supported
text/numeric states. Unknown status text is preserved without guessing an alias.
Text err and network_error_code are separate namespaces. No conflict resolver,
ID-to-submission association or finality inference is supplied.

ParseReceipt is **not a replacement for Codec validation**. It can diagnose
manually constructed malformed typed bodies that strict Decode would reject.
For example, forbidden 3.3 TLVs are retained as unsupported raw evidence only;
none of their fields or payloads are interpreted. Codec still rejects them on
3.3 wire input/output. The parser never weakens command/TLV validation.

## Explicit text dialect

Dialect zero (`ReceiptRawText`) retains payload bytes without interpreting text,
while still extracting standard TLVs. `ReceiptKeyValue` opts into this grammar:

- Labels: `id`, `sub`, `dlvrd`, `submit date`, `done date`, `stat`, `err`, `text`.
  Labels are case sensitive; `Text` is an explicit accepted alias for `text`.
- Each label ends with `:`. Fields may be reordered; spaces/tabs separate them.
  Date labels have exactly one internal space. A non-text value is one token;
  whitespace may follow its colon. Unknown tokens produce a syntax diagnostic.
- `text:` consumes the entire remaining decoded string, including leading and
  trailing spaces. Field-looking content inside text is never reinterpreted.
- ID is an opaque 1..64-character token. Counts, when present, are three decimal
  digits. Dates are exactly ten digits, YYMMDDhhmm, with civil-component checks;
  no century/zone conversion or missing precision is invented. Year 00's leap
  century remains unspecified. These are not query_sm_resp.final_date strings.
- Text status spellings are ENROUTE, DELIVRD, EXPIRED, DELETED, UNDELIV, ACCEPTD,
  UNKNOWN and REJECTD, corresponding to numeric states 1..8. Unknown spellings
  remain in Fields with an unsupported diagnostic. No case/radix normalization.
- Error is an opaque 1..32-character token. ID/error tokens exclude whitespace,
  colon and non-printable/non-ASCII bytes. Text is at most 20 printable ASCII
  characters. Invalid values remain observable with a malformed diagnostic.
- Missing labels are diagnosed, including omitted sub/dlvrd/text. They do not
  prevent extraction of the available ID/state and never become invented zeros.

This is a named peer dialect inspired by Appendix B, including bounded common
variations (opaque longer IDs, omitted fields, wider opaque errors). It is not
advertised as a strict Appendix-B conformance validator or every vendor's format.

Text decoding is explicit: DCS 0 means **unpacked GSM default alphabet** in this
dialect, DCS 1/3 in 3.4/5.0 accept the ASCII subset, and DCS 8 uses strict UCS2.
The decoded grammar itself is ASCII. Other SMSC default alphabets, packed GSM,
unsupported coding, non-ASCII dialect content and UDH-bearing text remain raw
with diagnostics; no charset/table guessing occurs. A malformed GSM escape or
UCS2 sequence is diagnosed without producing decoded fields. NUL-terminated
vendor payloads are not silently stripped.

## Construct an explicit report

```go
body, err := message.BuildReceipt(message.ReceiptSpec{
    Kind:        message.MCReceipt,
    MessageID:   "opaque-0001",
    State:       3, // EXPIRED: supplied by the application, not inferred
    Source:      originalDestination,
    Destination: originalSource,
}, message.ReceiptBuildOptions{
    Version: pdu.V50,
    Command: pdu.DeliverSMID,
    Format:  message.ReceiptTLVOnly,
})
if err != nil { return err }
result, err := session.Request(ctx, body) // normal sequence/correlation/ACK
```

Version, command and format are required; there is no silent representation
fallback. Report addresses are explicit, normally reversed from the original
message. The constructor never normalizes addresses or chooses an ID/state.
TLV-only IDs are 1..64 non-NUL ASCII octets, retaining spaces, colons and other
legal ASCII values; text formats additionally require the documented token grammar.

| Version | DeliverSM | DataSM | State representation |
| --- | --- | --- | --- |
| 3.3 | ReceiptTextOnly, MCReceipt marker convention | Unsupported | Text spellings 1..8; no TLVs |
| 3.4 | TextOnly / TLVOnly / TextAndTLV | Same formats | TLV states 1..8 |
| 5.0 | TextOnly / TLVOnly / TextAndTLV | Same formats | TLV states 0..9 |

5.0 states 0 (SCHEDULED) and 9 (SKIPPED) are supported in TLV-only construction;
no new text spellings are invented for them. IntermediateNotification is
available in 3.4/5.0. Kind and numeric state remain caller-provided evidence;
UNKNOWN is not converted into success or an inferred terminal outcome.

Text goes in deliver_sm.short_message or data_sm.message_payload. 3.4/5.0
construction uses explicit ASCII DCS 1; 3.3 uses unpacked GSM7/DCS 0. A text
format with nil Text emits only id/stat. Optional ReceiptTextSpec fields are
omitted when absent; Text nil differs from a pointer to an empty string. Text-only
3.4/5.0 is a peer convention. Parsing text-only 5.0 also reports absent standard
ID/state TLVs; explicit format choice does not make it the standard representation.

TLV formats emit receipted_message_id and message_state, plus caller-supplied
NetworkError when present. Text fields with TLV-only format, or NetworkError with
text-only format, are rejected instead of silently discarded. Supplied fields
must satisfy the named dialect and versioned Codec limits. There is no truncation,
automatic segmentation or default counts/times. In particular, a long legacy
text report can exceed the 3.3 short_message limit and fail.

BuildReceipt returns an owned existing concrete PDU and performs standard Codec
preflight, including DataDelivery context for data_sm. This avoids a parallel
protocol-validation catalog; the temporary encoded validation buffer is discarded.
Callers may add customer TLVs afterward and validate them through their configured
Codec/Session. The helper does not bypass bind-role/TLV-negotiation checks.

## Resource and error bounds

ParseReceipt checks limits **before cloning**: at most 64 KiB combined payload and
TLV evidence (including four-byte TLV headers), 256 TLVs, address strings of at
most 64 bytes and service_type of at most five bytes. Text processing is capped
at 32 recognized/unknown tokens per carrier, with at most two carriers. Diagnostic
and field counts are consequently bounded. It owns no persistent resources.

Invalid options/body metadata return ErrMetadata; resource limits return
ErrCapacity; unmarked/other message bodies return ErrNotReceipt. These errors
return a zero Receipt. Construction returns nil on error: ErrMetadata for helper
contracts, existing Codec errors for protocol validation. Error text and diagnostics
exclude arbitrary message content. Raw evidence must be handled separately.

[Independent wire samples](wire-samples.md#v110-receipt-fixtures),
[validation](v1.1.0-receipts-validation.md), and
[benchmarks](benchmarks/README.md#receipt-helpers-v110) record local
evidence and scope. No persistence, callbacks, routing, delivery aggregation,
automatic reports, retries or external interconnection requirement is added.

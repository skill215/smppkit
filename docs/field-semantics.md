# Mandatory field semantics

Implementation preview, 2026-10-27. These checks apply to the implemented
standard commands on **both encode and decode**, including standalone Codec
use. Session uses the same Codec; no second protocol stack or public validation
mode was introduced. Carrier provisioning and application acceptance remain
separate from wire validation.

The authorities are [SMPP 3.3](https://smpp.org/SMPP-v3.3.pdf) §§6–7,
[SMPP 3.4 issue 1.2](https://smpp.org/SMPP_v3_4_Issue1_2.pdf) §§3.1, 4, 5.2, 7.1,
and [SMPP 5.0](https://smpp.org/SMPP_v5.pdf) §§3.1, 4.3.1, 4.7.

| Field | 3.3 | 3.4 | 5.0 |
| --- | --- | --- | --- |
| C-octet strings | ASCII, bounded and NUL-terminated | Same | Same |
| TON | 0..6 | 0..6 | 0..6 |
| NPI | Four-bit representation; assignments remain network-specific | 0, 1, 3, 4, 6, 8, 9, 10, 14, 18 | Same as 3.4 |
| priority_flag, SMS commands | 0..1 | 0..3 | 0..3; CBS-only priority 4 is not enabled |
| registered_delivery | 0..1 | Bits 5..7 zero; receipt selector 3 reserved | Bits 5..7 zero; receipt selector 3 means success only |
| replace_if_present_flag | 0/1; multi restricted to one explicit SME | 0/1 on submit_sm; zero on submit_multi/deliver_sm | 0/1 on submit/deliver/multi |
| sm_default_msg_id | 0 unused, 1..100 | 0 unused, 1..254; deliver_sm requires zero | 0 unused, 1..255 |
| schedule / validity | Empty or absolute | Empty, absolute or relative; deliver_sm requires empty | Empty, absolute or relative, including deliver_sm forwarding fields |
| query response state, OK header | Raw historical value; numeric assignments are not enumerated in the available 3.3 specification | 1..8 | 0..9 |
| deliver_sm_resp.message_id | Retained using the 3.3 submit-response layout | Must be empty | Must be empty |

TON/NPI checks cover bind metadata, submit/deliver/data addresses, query/replace/
cancel addresses, alert_notification and submit_multi's SME and failure lists.
The 3.3 NPI rule checks representation only; it does not silently apply the later
IP/WAP assignment table. 3.3 delegates address interpretation to GSM 03.40.
The related [3GPP address field description](https://www.etsi.org/deliver/etsi_ts/123000_123099/123040/16.00.00_60/ts_123040v160000p.pdf)
§9.1.2.5 describes TON and the four-bit NPI representation. No BCD conversion,
telephone-number normalization or carrier lookup is performed.

`esm_class` message-type bits are checked separately from mode/features:

- 3.3 submission requires zero. Its delivery marker is retained raw because
  the numeric delivery-receipt assignment is delegated to an applications guide.
- 3.4 submit_sm/submit_multi accept type bits 0x00, 0x08 and 0x10. Delivery
  also accepts 0x04, 0x18 and 0x20. The two low deliver_sm mode bits are ignored
  by that version and are preserved, not required to be zero.
- 5.0 uses those six message types for submission and delivery. Mode bits and
  the two GSM feature bits remain independently representable.
- Ordinary Codec encoding/decoding accepts data_sm's submission/delivery union.
  `Codec.ValidateDataDirection` supplies explicit context for standalone use;
  Session applies it automatically alongside command/bind-role checks. See
  [direction validation](data-direction.md) for request and response rules.

For 3.4/5.0, `data_coding` accepts 0..10, 13, 14, 0xC0..0xDF and 0xF0..0xFF.
Other assignments are reserved by those SMPP tables. The same rule checks the
encoding octet of callback_num_atag. Codec does not decode the text or interpret
the internal GSM control bits. 3.3 carries a GSM DCS, so its full octet remains
available instead of imposing the later SMPP coding table. Protocol ID, network
error codes and opaque binary payloads remain unchanged.

## Time grammar and recorded interpretations

Times are wire strings, not Go time.Time values. Nonempty absolute time is
`YYMMDDhhmmsstnnp`, with 15 ASCII digits and a `+` or `-` suffix. Month/day,
hour/minute/second, tenths and UTC offset are checked; offset is 00..48 quarter
hours. Impossible calendar dates are rejected. The wire has no century: years
divisible by four permit February 29, including `00`, which can denote 2000.
The library does not select a century, compare with the current time, reorder
schedule/expiry or apply an operator's maximum lifetime.

3.4/5.0 relative time has suffix `R`. Months 0..12 and days 0..31 are offsets,
not calendar fields. Zero components must be usable for durations such as one
hour, despite the 5.0 relative table repeating absolute minimums of 01. Hours
are 0..23; minutes/seconds 0..59. The specification says relative tenths/offset
digits are ignored: numeric nonzero values are accepted and preserved rather
than silently normalized. 3.3 has no relative form.

`final_date` is empty or absolute. 3.3 §7.5 and 3.4 §7.1.1 explicitly describe
12-digit local response dates, while the 3.4 query layout lists 1/17 C-octets.
The chosen profile accepts both 12-digit local and 16-character absolute dates
for those two versions. 5.0 requires the explicit absolute form. Relative final
dates are rejected in every profile. Known non-final states (ENROUTE, and 5.0
SCHEDULED) cannot carry a final date on an OK query response. Empty final dates
are preserved otherwise; terminal-state timestamp completeness is peer policy.

Other deliberate interpretations:

- 3.4's registered_delivery heading says bit 5, but its diagram sets bit 4.
  The implementation follows the diagram and the explicit 5.0 definition.
- Zero default-message ID remains allowed as unused, as required by command
  layouts, despite the 3.4 scalar table labelling zero reserved.
- 3.3 deliver_sm explicitly reuses submit_sm's fields. 3.4's unused-field
  restrictions are not imposed on 3.3 or 5.0. 5.0 forwarding context belongs to
  the application; Codec can validate its fields without owning that routing.
- An error query response may carry placeholders, including state zero. State
  enumerations/non-final consistency apply only when command_status is OK.
  A supplied date still must have a legal representation. Existing omitted
  negative-response handling and opaque status/error values are unchanged.

## Scope and evidence

An empty C-octet still occupies its terminating byte. The library does not infer
global nonempty-string requirements from carrier expectations: source defaults,
cancel selectors, system/password policy and peer message identifiers remain
application decisions. Message IDs are opaque ASCII strings, not parsed numbers.
Service types may be carrier-defined. Address ranges remain opaque ASCII routing
expressions; Codec does not execute or compile them as Go regular expressions.

The [standard TLV checks](tlv-support.md) additionally cover parameter bounds
and dependencies. Receipt IDs now obey the same ASCII C-octet rule. Unknown TLV
values, short_message bytes and private command bodies do not acquire an ASCII
restriction. UDH contents and text validity remain responsibilities of the
message/text-codec layers; Codec does not guess a network alphabet.

This is not full network or transaction conformance: selected GSM coding-group
rules, historical 3.3 assignments, transaction-mode-dependent receipt rules, peer
storage/canned-message policy, receipt recommendations and response-to-original-
request business relationships still need explicit profiles or application
context. No external SMSC interoperability claim is made.

The [data_sm transaction-context checks](data-transaction.md) now cover datagram
registered-delivery and explicit submission-mode response restrictions.
Unresolved peer defaults and cross-message receipt completeness remain outside
that scope.

[`codec/semantics_test.go`](../codec/semantics_test.go) supplies independent full
frames, exhaustive single-octet sweeps, address-layout checks and calendar/time
boundaries. [`session_semantics_test.go`](../session_semantics_test.go) verifies
no write/sequence allocation for invalid local fields, validation before bind
authentication, correctable response handles and rejection of a malformed peer
response. See [annotated samples](wire-samples.md) and [fresh validation results](validation.md).

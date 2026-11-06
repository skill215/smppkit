# v1.2.0 binary DCS policy

Finalized for F2 implementation: 2026-11-06 (Asia/Shanghai).
**Implemented locally for v1.2.0, unreleased.** This table defines the
[PrepareBytes/SendBinary](binary-messaging.md) high-level policy;
it does not narrow the existing Codec's accepted data_coding values.

| SMPP profile | Binary helper whitelist | Interpretation and constraints |
| --- | --- | --- |
| 3.3 | `0x04` | GSM uncompressed eight-bit data, no message class; single-part high-level messages only, no TLVs or generated UDH |
| 3.4 | `0x02`, `0x04` | Explicit octet data assignments; UDH8/UDH16/SAR/Payload subject to existing command and negotiated TLV rules |
| 5.0 | `0x02`, `0x04` | Same explicit octet assignments and mode restrictions as 3.4 |

`0x04` is the cross-profile choice when the peer agrees to binary messaging.
There is no implicit default or rewriting between `0x02` and `0x04`. In 3.3,
`0x02` is not the later SMPP binary assignment: the GSM general coding group
does not select the eight-bit alphabet with those bits.

Sources: [SMPP 3.3](https://smpp.org/SMPP-v3.3.pdf), section 6.3.3.1's GSM DCS
reference; [3GPP TS 23.038](https://www.etsi.org/deliver/etsi_ts/123000_123099/123038/16.00.00_60/ts_123038v160000p.pdf),
section 4's general SMS coding group (not the cell-broadcast table);
[SMPP 3.4](https://smpp.org/SMPP_v3_4_Issue1_2.pdf), section 5.2.19; and
[SMPP 5.0](https://smpp.org/SMPP_v5.pdf), section 4.7.7. These assignments are
protocol facts; the intentionally small whitelist is a library policy choice.

All other values are rejected by the binary helpers, including `0x00`
(default alphabet), text alphabets such as `0x03`/`0x08`, GSM class-control
values, compression, message-waiting and automatic-deletion groups. Some of those
values can represent valid protocol messages or eight-bit data in a specific
profile; excluding them here does not declare them invalid SMPP. Callers needing
those semantics can continue using concrete PDUs and their explicit peer policy.

Binary preparation preserves bytes; it does not interpret encoded text or promise
character-safe splitting for an arbitrary DCS. Each payload byte consumes one
octet of the configured radio budget. Application-port and concatenation UDH
consume their encoded length; SAR retains the existing conservative overhead
policy. Payload is a single SMPP request, not an assertion about radio segments.
No language-table handling, charset sniffing or mode fallback is introduced.

The F2 tests cover all 256 DCS values against each profile's whitelist,
independent UDH/SAR/Payload bytes and capacity boundaries, `0x02` rejection
in 3.3, and unchanged raw Codec behavior. See the [validation record](v1.2.0-f2-validation.md)
and [ownership, Session and outcome contract](binary-messaging.md).

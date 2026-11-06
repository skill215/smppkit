# Stable semantic diagnostics (v1.2.0 development)

Identifier catalog finalized: 2026-11-06 (Asia/Shanghai). This is the F1 contract;
v1.1.0 does not expose these types. Validation still reports the first encountered
failure; ordering between multiple failures is not a stable contract.

`errors.AsType[*codec.SemanticError]` (Go 1.26), or `errors.As`, retrieves Field,
Rule, Index/HasIndex and Tag/HasTag without parsing Error(). Field and rule strings
below are stable within v1. Callers must tolerate additional identifiers. The
zero values mean unspecified; a library-produced diagnostic has nonempty Field
and Rule. Error formatting is not stable and never includes input values.

Index is zero-based in the complete Optional, Destinations or Unsuccessful slice,
not a per-tag occurrence count or byte offset. HasIndex distinguishes absence
from index zero; HasTag similarly permits a numeric tag of zero. TLV diagnostics
use Field=`optional` plus Tag, avoiding a second public registry of TLV names.
Nested destination paths use the exact field strings below and a separate Index.

## Rule identifiers

| Exported constant | Stable string | Meaning |
| --- | --- | --- |
| RuleValue | `value` | Assigned scalar values, combinations or reserved bits |
| RuleLength | `length` | Byte length outside a version/definition bound |
| RuleCount | `count` | Collection count outside its bound |
| RuleUnused | `unused` | A field or inactive union arm must be empty/zero |
| RuleVersion | `version` | Selected version is unsupported for this item |
| RuleCommand | `command` | Unsupported command/profile or item not applicable to a command |
| RuleDirection | `direction` | Item not applicable to the original request direction |
| RuleConflict | `conflict` | Mutually exclusive payload/segmentation representations |
| RuleDependency | `dependency` | Missing/inconsistent related fields or counts |
| RuleCardinality | `cardinality` | Repetition prohibited by the TLV definition |
| RuleTime | `time` | Date/time grammar or civil component constraints |
| RuleMode | `mode` | Incompatible messaging mode |
| RuleStatus | `status` | Incompatible command or response status |
| RuleProfile | `profile` | Bind interface_version does not match the Codec |
| RuleBody | `body` | Missing/wrong concrete body or illegal omission |
| RuleExtension | `extension` | Customer callback rejected input; its private rule is not inferred |

`value` deliberately covers reserved bits and combined scalar predicates. It does
not promise a sub-rule for each reserved bit inside a standard TLV value. Rule,
Field and optional tag together identify the existing validation site/domain.

## Field/rule coverage inventory

| Field | Rules | Existing check sites / context |
| --- | --- | --- |
| `sequence_number` | value | Header range; zero only for generic_nack |
| `command_status` | status | Requests require OK; generic_nack requires non-OK |
| `command_id` | command, version | Unsupported standard command/profile, inactive private command profile, non-bind bootstrap result, 3.3 data_sm context |
| `interface_version` | version, profile | Codec creation/bootstrap version; bind profile match |
| `body` | body | Append shape/omission and data_sm contextual argument types |
| `addr_ton`, `addr_npi` | value | Bind address type |
| `source_addr_ton`, `source_addr_npi` | value | Source address type |
| `dest_addr_ton`, `dest_addr_npi` | value | Single destination address type |
| `esme_addr_ton`, `esme_addr_npi` | value | alert_notification ESME address type |
| `esm_class` | value, direction | Existing message flag table and explicit data_sm direction |
| `registered_delivery` | value, mode | Existing receipt flag table; datagram submission restriction |
| `data_coding` | value | Existing 3.4/5.0 assignment predicate; raw 3.3 remains opaque GSM DCS |
| `priority_flag` | value | Version-specific maximum |
| `replace_if_present_flag` | value, unused, dependency | Scalar bound, unused 3.4 deliver field, submit_multi replacement restrictions |
| `sm_default_msg_id` | value, unused | Version bound; unused 3.4 deliver field |
| `schedule_delivery_time`, `validity_period` | time, unused | Date/time validation; unused 3.4 deliver fields |
| `short_message` | length, conflict | Version byte bound; simultaneous message_payload |
| `message_id` | unused | deliver_sm_resp unused value in 3.4/5.0 |
| `message_state` | value | Successful query_sm_resp state table |
| `final_date` | time, dependency | Date/time grammar; non-final state conflict |
| `number_of_dests` | count | submit_multi destination bound |
| `dest_address.dest_flag` | value | Indexed unknown destination variant |
| `dest_address.dl_name`, `dest_address.address` | unused | Indexed inactive typed variant (encode only) |
| `dest_address.dest_addr_ton`, `dest_address.dest_addr_npi` | value | Indexed SME address in submit_multi |
| `no_unsuccess` | count | Typed response list exceeding one-byte count (encode only) |
| `unsuccess_sme.dest_addr_ton`, `unsuccess_sme.dest_addr_npi` | value | Indexed failed destination in submit_multi_resp |
| `data_direction` | value | Invalid explicit direction argument |
| `optional` without tag/index | version, command, count | 3.3 prohibition, command without optional section, contextual scan bound |
| `optional` with tag/index | version, command, length, cardinality, value | Standard and registered customer TLV applicability/bounds; opaque tag length; broadcast-only tag rejection |
| `optional` with tag/index | direction, mode, status | data_sm contextual TLV checks |
| `optional` with tag/index | conflict | SAR or application-port TLV with UDHI |
| `optional` with tag, optional index | dependency | SAR completeness/order, callback occurrence association, network/node and number-portability dependencies |
| `optional` with tag/index | extension | Registered customer value callback returned an error |

Standard TLV value coverage uses the existing [53-definition catalog](tlv-support.md)
and predicates; F1 adds no new acceptance restrictions. Unknown TLVs remain
opaque and may repeat. Customer definition checks run before their callback.

Dependency attribution is deterministic: a missing SAR member names its absent
tag without Index (reference, total, then sequence); a zero total names total;
zero/out-of-range sequence names sequence. Callback association names the first
ATag/PresInd occurrence that requires matching callback numbers. Network/node
dependencies name the network ID; portability dependencies name NPResolution.
Present dependency triggers use the first matching tag's absolute Optional index.

## Error chain and classification compatibility

Most semantic causes still unwrap to ErrValidation. Unsupported profile/command
causes retain ErrUnsupported. Decode wraps them in the existing DecodeError;
optional-section validation retains DecodeTLV, even when a customer cause is
itself ErrUnsupported or a FieldError.

Four historical decode checks already produced FieldError: bind profile mismatch,
submit_multi destination count, unknown destination flag, and replace_sm payload
length. They now also carry SemanticError while preserving that same FieldError
(field/offset/reason) and DecodeMandatory classification. They do **not** acquire
ErrValidation if the old cause lacked it. Equivalent encode failures still wrap
ErrValidation. The new diagnostic type alone therefore does not determine Kind
or sentinel membership.

Customer callback causes were previously discarded. F1 retains them alongside
ErrValidation using an error chain; errors.Is/As can now inspect the callback's
original error. RuleExtension describes only the callback boundary. The outer
SemanticError and DecodeError summaries do not format that cause. Logging an
unwrapped arbitrary cause still requires application care. Private command
callback causes and DecodeExtension remain unchanged.

No fabricated offset or payload is added. The diagnostic is a snapshot; adding
a destination index copies it and never mutates an already published error.
Valid checks do not allocate diagnostic objects. Concurrent operations on one
Codec return independent diagnostic snapshots.

## Deliberate exclusions

- Framing, I/O, MaxFrame admission, C-octet ASCII/terminator/length errors,
  truncated mandatory/TLV structures and trailing bytes keep their existing
  diagnostics. They are not converted into a second semantic validation engine.
- Codec registration configuration errors other than interface_version remain
  configuration errors, not protocol field diagnostics.
- Private command callback errors/identity failures and application-specific
  rules remain extension causes. Arbitrary callback text is never parsed.
- Session state, bind negotiation, message preparation, receipt diagnostics and
  business outcomes retain their own contracts; F1 does not unify them.
- Fields deliberately left opaque by current validation gain no invented checks.

This catalog describes built-in rules, not automatic SMPP status/NACK mapping,
retry advice, or permission to continue a malformed stream. See the existing
[framing diagnostics](framing.md) and [field semantics](field-semantics.md).

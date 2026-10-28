# Full implementation review — 2026-10-28

Status: RV20/RV21 are **fixed locally**, with formal regressions and the
[correction evidence](#rv20rv21-corrections) below. The original review findings
and failed probes are preserved as historical evidence. This does not approve
Release 1.0 or reopen the already fixed RV17–RV19 by inference.
The existing concrete PDUs, versioned Codec and shared Session remain suitable;
both new findings can be corrected locally without a new public framework.

The review covered the current local implementation, including untracked files,
not just the small tracked Git diff. Go remains 1.26 (tested with 1.26.4).
During the original review, production code, existing tests/examples, README and
reference repositories were not modified. Only the report and evidence were
added; the later correction changes are described below. Persistence, routing,
delivery tracking and external SMSC integration are not new acceptance demands.

## RV21 — P1: memory admission sends a premature transaction success

Location: [AdmitMessage](../messaging.go), especially the unconditional successful
status after `receiver.Admit` at lines 182–195. `RunReceiver` uses this helper.

An ESME can send an explicit transaction submission (`esm_class & 3 == 2`).
AdmitMessage accepts it into the in-memory receiver and immediately sends OK,
although no application consumer or delivery attempt has run. The normal
data_sm direction/response checks accept this response: those checks validate
the response's fields, not whether delivery happened. The example server inherits
this behavior through RunReceiver.

This differs from ordinary submission acceptance. Transaction mode reports the
delivery outcome in the response: [SMPP 3.4 §2.10.3](https://smpp.org/SMPP_v3_4_Issue1_2.pdf)
uses data_sm, while [SMPP 5.0 §4.2.10.4](https://smpp.org/SMPP_v5.pdf) also supports
submit_sm. A comment that memory ACK only means buffering cannot change what the
remote endpoint understands from that transaction response.

Reproduction: `TestFinalReviewBufferedTransactionACK` sends literal request bytes
over net.Pipe to a bound server-side Session. No Receiver.Receive or delivery
worker exists. In 3.4 data_sm, 5.0 data_sm and 5.0 submit_sm, the response is OK
while `Queued == 1`. All three mode-2 checks fail; corresponding mode-3 positive
controls pass. The probe deliberately does not claim to test bind negotiation.

Recommended correction: reject unsupported explicit transaction submissions
before memory admission in the automatic ACK path, and document that applications
implementing that mode must use Receive/Respond and supply their actual result.
Do not add a delivery engine or persistence. Keep default-mode policy explicit;
do not apply a submission-mode restriction blindly to MC-to-ESME delivery.
Regression coverage should include direction/version controls and confirm that
a rejected transaction does not enter the completion queue.

## RV20 — P2: appending a decoded field overwrites a sibling field

Location: [wire.Reader.Bytes](../internal/wire/wire.go), line 45; exposed through
Codec.Decode/Read by short_message and TLV value slices.

Decode owns a body copy, but Bytes returns a two-index subslice with capacity
extending through the remainder of that body. As a result, ordinary append to
one returned field can overwrite a later returned field in the same PDU. The
original input remains separate; the defect is aliasing *between decoded fields*.
This is silent payload/metadata corruption, not merely excess allocation.

Reproduction: `TestFinalReviewDecodedSliceIsolation` decodes independent data_sm
bytes with customer TLVs 0x1500=`11` and 0x1501=`22`. Appending five bytes to the
first value changes the second value from `22` to `05`, in both 3.4 and 5.0.
The in-range edit control does not change the sibling. No caller concurrent
mutation or unsafe reslicing is needed.

Recommended correction: cap each exposed byte field at its own end (a full
slice expression), retaining the existing owned-body allocation strategy.
There is no need to copy every field separately. Add ordinary append-isolation
tests for adjacent TLVs, empty values and short_message followed by TLVs;
preserve input independence and the existing allocation benchmarks.

## Reproduction artifacts

- [Probe source](validation/final-review-probes_test.go.txt). It uses the root
  package's existing private Session constructor to isolate the admission path.
  It is stored as text so expected failures do not silently enter the normal
  green test suite before the fixes are made.
- [Fresh results](validation/final-review-go1.26.4.txt), including the expected
  probe failures, fuzz runs, benchmark smoke results and example executions.
- [Reviewed-source manifest](validation/final-review-source.sha256), covering
  81 Go/module/README/workflow files, including untracked sources. Its SHA-256 is
  `56c409bb729c572cad01be88f9868e43f1cc36aa5d8e7a143e1baa186377a1da`.

To reproduce from the repository root without editing implementation files:

```sh
python3 - <<'PY'
import json
from pathlib import Path
root = Path.cwd()
Path('/tmp/smppkit-final-review-overlay.json').write_text(json.dumps({
    'Replace': {
        str(root / 'final_review_probe_test.go'):
        str(root / 'docs/validation/final-review-probes_test.go.txt')
    }
}))
PY
go test -race -overlay=/tmp/smppkit-final-review-overlay.json \
  -run '^TestFinalReview' -v -count=1 .
```

The command failed on the original reviewed implementation. The corrected tree
should pass these historical probes; ordinary regression tests now exercise
stronger assertions, including the exact rejection error, retained handle and
absence of queue admission, rather than accepting any helper error as a fix.

## Original review validation and remaining gates

| Check | This review's result |
| --- | --- |
| Formatting, build, vet, tracked diff whitespace | PASS |
| Full `go test -race ./...` | PASS, including local TCP/TLS tests; optional resource soak skipped by its normal opt-in guard |
| All 11 checked-in fuzz targets | PASS, each with `-fuzztime=10000x -parallel=2 -timeout=60s`; existing local corpus cache plus checked-in seeds |
| All benchmarks in root/codec/message/textcodec | PASS: 80 cases, `-race -benchtime=1x`; a smoke check, not performance measurements comparable to README medians |
| Eight self-contained examples | PASS, each executed with `go run -race` |
| Client/server examples | PASS using newly built race binaries: three-version submissions, four 3.4/5.0 multipart echo exchanges, unsupported 3.3 multipart, rejected bind, delivery timeout and both server Ctrl-C shutdowns |
| New review probes | FAIL as expected on RV20 and RV21; positive mode-3 controls PASS |
| Go 1.27, remote CI, ten-minute soak, external/legacy SMSC | NOT RUN in this review |

That bounded count-based fuzz campaign did not exercise the earlier time-budget
shutdown anomaly and does not resolve its unconfirmed cause. It also does not
satisfy R3's minimum 30 seconds per target. At review time CI omitted a
dedicated FuzzAdditionalUDH fuzz invocation (its seeds still run in normal tests);
the correction now adds that invocation. This was a coverage follow-up, not a
newly reproduced runtime defect.

After the corrections below, the next local work is to finish R2's API
and advertised-subset audit. R3's sustained/candidate validation and R4's release
review remain open. Broadcast, historical excluded commands, persistence,
business delivery state and a Go upgrade are not needed to repair these issues.

## RV20/RV21 corrections

Implemented locally on 2026-10-28, retaining Go 1.26 and all public signatures.

- RV20: wire.Reader.Bytes uses a full slice expression so every standard byte
  field has cap equal to len. The owned-body copy remains shared; no additional
  per-field allocation or copy is introduced. Codec GoDoc describes this boundary.
  [TestRV20DecodedFieldIsolation](../codec/ownership_regression_test.go) covers
  Decode and Read, 3.4/5.0, adjacent/empty TLVs and nonempty/empty short_message
  before a TLV. FuzzDecode includes all four literal seeds and asserts field
  isolation after accepted round trips, including 3.3 where applicable.
- RV21: AdmitMessage returns an error wrapping ErrState for server-side explicit
  mode-2 data_sm/submit_sm, before receiver admission, ID generation or response.
  The original command and scalar ESM snapshot are used even after application
  PDU edits. The existing inbound ESM byte now also captures submit_sm; no new
  map, goroutine, public interface or per-request payload retention is added.
  The response handle remains available to the application. RunReceiver keeps
  its existing error-exit close policy and emits no ACK for this case.
- [TestRV21BufferedACKPolicy](../messaging_ack_regression_test.go) checks the
  three-version direction/mode matrix, including mode 0/1/3 controls, other ESM
  bits, 3.3 delivery flags, zero retained receiver state and a subsequent explicit
  application response. A byte peer checks the first response header directly;
  it uses no production Codec to manufacture requests or interpret responses.
  Additional tests cover edited PDU identity and RunReceiver's no-ACK close.
- Mode zero still requires a configured non-transaction MC default for buffered
  ACK. MC-to-ESME delivery is not rejected based on submission-mode bits.
  Mode 2 on 3.4 submit_sm is also refused by the automatic helper, without
  changing low-level Codec acceptance. Applications supply their own transaction
  results through Receive/Respond. No persistence or delivery engine is added.

The final formal tests were also run through a temporary Go overlay that removes
only the capacity bound and automatic-ACK guard. They fail on both defects while
the corrected tree passes. The overlay is not part of the implementation.
An initially mistyped empty-short-message fixture contained an extra zero byte;
it was corrected before the final regression, reverted-overlay and fuzz runs.

Fresh correction results and benchmark comparison are recorded in
[RV20/RV21 validation](validation.md#rv20rv21-correction-validation), separately
from the original review's passing suite and failed probes. R2–R4 remain open.

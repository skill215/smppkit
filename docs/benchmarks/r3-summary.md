# R3 candidate benchmarks

Measured 2026-10-28 on Go 1.26.4 linux/amd64, WSL2, AMD Ryzen 7 6800H.
All 80 cases ran three times, 200 ms per case, `-benchmem -cpu=1`, without race
instrumentation or CPU affinity. No other task Go checks remained running during
these measurements; host scheduling, power and frequency were not controlled.

```sh
go test . ./codec ./message ./textcodec -run='^$' -bench=. \
  -benchmem -benchtime=200ms -count=3 -cpu=1
```

[Raw results](r3-go1.26.4.txt) retain all 240 observations. Fixture sizes and copy/
ownership semantics are unchanged from the [README](../../README.md#encode-decode-and-memory-copy-benchmarks).
The source identity is in the [R3 record](../r3-validation.md).

All **75** cases overlapping the earlier [expanded suite](expanded-go1.26.4-linux-amd64.txt)
have identical median B/op and allocs/op. The five older additional benchmarks
also pass; they are not included in that 75-case comparison. No copy elimination,
zero-allocation claim or universal speedup follows from these measurements.

## Timing changes and follow-up

Against the earlier expanded record, median timings range from -48.4% to +95.4%.
These are separate local runs, not a controlled before/after experiment. R3's
non-test Go sources are byte-identical to the R2 manifest; no runtime optimization
or slowdown was introduced by this stage. Historical pre-RV20 source is not the
same candidate and is not silently treated as a controlled baseline.

The largest increase, Read/DataSM/V50/4096, moved from historical 1474 ns/op to
2880 ns/op (2662–3026 across the three candidate observations). An isolated
500 ms ×5 recheck of the **same candidate** measured 1832–2179 ns/op, median
1932 ns/op, always 9968 B/op and 11 allocs/op. This demonstrates substantial
within-candidate timing variability; it does not identify its exact host cause
or prove the historical timing will be recovered. The remaining difference is
recorded, not hidden by selecting the fastest sample. There is no task-defined
latency/TPS threshold to change or waive.

```sh
go test ./codec -run='^$' \
  -bench='^BenchmarkCodecRead$/^DataSM$/^V50$/^4096$' \
  -benchmem -benchtime=500ms -count=5 -cpu=1
```

[Isolated recheck output](r3-read-recheck.txt). B/op is allocated heap bytes, not
copied traffic or retained heap. Copy baselines measure hot-buffer copy costs;
Session measurements include both net.Pipe endpoints. No external SMSC load or
network-latency claim is made.

## Candidate medians

All values below are medians of three observations. Allocation columns were
also checked against the 75-case historical intersection.

| Case | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| `BenchmarkSessionRequest/EnquireLink` | 7868 | 1664 | 30 |
| `BenchmarkSessionRequest/SubmitSM140` | 11022 | 2864 | 44 |
| `BenchmarkSessionSendText/GSM7Single` | 16781 | 5608 | 59 |
| `BenchmarkSessionSendText/UCS2FallbackSingle` | 16208 | 6008 | 63 |
| `BenchmarkCodecEncode/SubmitSM/V33/140/NewBuffer` | 387.8 | 496 | 5 |
| `BenchmarkCodecEncode/SubmitSM/V33/140/ReuseBuffer` | 319.5 | 304 | 4 |
| `BenchmarkCodecEncode/SubmitSM/V34/140/NewBuffer` | 385.9 | 496 | 5 |
| `BenchmarkCodecEncode/SubmitSM/V34/140/ReuseBuffer` | 330.6 | 304 | 4 |
| `BenchmarkCodecEncode/SubmitSM/V50/140/NewBuffer` | 362.2 | 496 | 5 |
| `BenchmarkCodecEncode/SubmitSM/V50/140/ReuseBuffer` | 315.6 | 304 | 4 |
| `BenchmarkCodecEncode/SubmitSM/V50/255/NewBuffer` | 398.3 | 752 | 5 |
| `BenchmarkCodecEncode/SubmitSM/V50/255/ReuseBuffer` | 345.7 | 432 | 4 |
| `BenchmarkCodecEncode/DataSM/V50/140/NewBuffer` | 498.1 | 496 | 5 |
| `BenchmarkCodecEncode/DataSM/V50/140/ReuseBuffer` | 466.2 | 304 | 4 |
| `BenchmarkCodecEncode/DataSM/V50/4096/NewBuffer` | 1531 | 9840 | 5 |
| `BenchmarkCodecEncode/DataSM/V50/4096/ReuseBuffer` | 1180 | 4976 | 4 |
| `BenchmarkCodecEncode/DataSM/V50/65535/NewBuffer` | 14093 | 147568 | 5 |
| `BenchmarkCodecEncode/DataSM/V50/65535/ReuseBuffer` | 9179 | 73840 | 4 |
| `BenchmarkCodecEncode/SubmitMulti/V50/2/NewBuffer` | 516.1 | 704 | 6 |
| `BenchmarkCodecEncode/SubmitMulti/V50/2/ReuseBuffer` | 431.6 | 496 | 5 |
| `BenchmarkCodecEncode/SubmitMulti/V50/255/NewBuffer` | 11616 | 12272 | 10 |
| `BenchmarkCodecEncode/SubmitMulti/V50/255/ReuseBuffer` | 10569 | 8176 | 9 |
| `BenchmarkCodecDecode/SubmitSM/V33/140` | 441 | 440 | 8 |
| `BenchmarkCodecDecode/SubmitSM/V34/140` | 448.1 | 440 | 8 |
| `BenchmarkCodecDecode/SubmitSM/V50/140` | 449.3 | 440 | 8 |
| `BenchmarkCodecDecode/SubmitSM/V50/255` | 475.3 | 584 | 8 |
| `BenchmarkCodecDecode/DataSM/V50/140` | 596.5 | 408 | 9 |
| `BenchmarkCodecDecode/DataSM/V50/4096` | 1177 | 5096 | 9 |
| `BenchmarkCodecDecode/DataSM/V50/65535` | 6817 | 73960 | 9 |
| `BenchmarkCodecDecode/SubmitMulti/V50/2` | 766.2 | 664 | 13 |
| `BenchmarkCodecDecode/SubmitMulti/V50/255` | 38749 | 52696 | 779 |
| `BenchmarkCodecCopy/187/NewBuffer` | 48.48 | 192 | 1 |
| `BenchmarkCodecCopy/187/ReuseBuffer` | 6.024 | 0 | 0 |
| `BenchmarkCodecCopy/4143/NewBuffer` | 467.7 | 4864 | 1 |
| `BenchmarkCodecCopy/4143/ReuseBuffer` | 36.49 | 0 | 0 |
| `BenchmarkCodecCopy/65582/NewBuffer` | 6196 | 73728 | 1 |
| `BenchmarkCodecCopy/65582/ReuseBuffer` | 1098 | 0 | 0 |
| `BenchmarkSubmitCodec` | 400.2 | 400 | 6 |
| `BenchmarkSubmitMultiCodec/2` | 1039 | 624 | 16 |
| `BenchmarkSubmitMultiCodec/255` | 40596 | 52144 | 782 |
| `BenchmarkReviewRejectOversizedFrame` | 529.2 | 208 | 5 |
| `BenchmarkCodecRead/SubmitSM/V33/140` | 614.9 | 640 | 10 |
| `BenchmarkCodecRead/SubmitSM/V34/140` | 669.7 | 640 | 10 |
| `BenchmarkCodecRead/SubmitSM/V50/140` | 680.9 | 640 | 10 |
| `BenchmarkCodecRead/SubmitSM/V50/255` | 803.7 | 912 | 10 |
| `BenchmarkCodecRead/DataSM/V50/140` | 930.8 | 608 | 11 |
| `BenchmarkCodecRead/DataSM/V50/4096` | 2880 | 9968 | 11 |
| `BenchmarkCodecRead/DataSM/V50/65535` | 13641 | 147696 | 11 |
| `BenchmarkCodecRead/SubmitMulti/V50/2` | 912 | 880 | 15 |
| `BenchmarkCodecRead/SubmitMulti/V50/255` | 41680 | 56800 | 781 |
| `BenchmarkCodecTLVs/0/EncodeReuse` | 253.6 | 112 | 3 |
| `BenchmarkCodecTLVs/0/Decode` | 372.9 | 264 | 6 |
| `BenchmarkCodecTLVs/8/EncodeReuse` | 1215 | 496 | 5 |
| `BenchmarkCodecTLVs/8/Decode` | 1318 | 848 | 10 |
| `BenchmarkCodecTLVs/64/EncodeReuse` | 11019 | 6488 | 16 |
| `BenchmarkCodecTLVs/64/Decode` | 11990 | 10040 | 22 |
| `BenchmarkCodecReject/ShortHeader` | 181.9 | 80 | 2 |
| `BenchmarkCodecReject/TruncatedTLV` | 321.4 | 240 | 7 |
| `BenchmarkMessagePlan/UDH8/Prepare` | 11912 | 7608 | 13 |
| `BenchmarkMessagePlan/UDH8/Materialize` | 726.5 | 1664 | 16 |
| `BenchmarkMessagePlan/UDH16/Prepare` | 12028 | 7608 | 13 |
| `BenchmarkMessagePlan/UDH16/Materialize` | 715.8 | 1664 | 16 |
| `BenchmarkMessagePlan/SAR/Prepare` | 12164 | 7608 | 13 |
| `BenchmarkMessagePlan/SAR/Materialize` | 1010 | 2068 | 26 |
| `BenchmarkReceiverReassemble/UDH16/Reversefalse` | 15479 | 13232 | 79 |
| `BenchmarkReceiverReassemble/UDH16/Reversetrue` | 15957 | 13232 | 79 |
| `BenchmarkReceiverReassemble/SAR/Reversefalse` | 16945 | 13168 | 109 |
| `BenchmarkReceiverReassemble/SAR/Reversetrue` | 16818 | 13168 | 109 |
| `BenchmarkReceiverDuplicate` | 776.6 | 672 | 11 |
| `BenchmarkPrepare` | 12779 | 7864 | 13 |
| `BenchmarkTextCodec/GSM7/Basic160/Encode` | 2987 | 1440 | 2 |
| `BenchmarkTextCodec/GSM7/Basic160/Decode` | 1152 | 816 | 2 |
| `BenchmarkTextCodec/GSM7/Extension80/Encode` | 1586 | 880 | 3 |
| `BenchmarkTextCodec/GSM7/Extension80/Decode` | 1468 | 736 | 2 |
| `BenchmarkTextCodec/Latin1/140/Encode` | 1442 | 1440 | 2 |
| `BenchmarkTextCodec/Latin1/140/Decode` | 959 | 864 | 2 |
| `BenchmarkTextCodec/UCS2/70/Encode` | 911.4 | 800 | 2 |
| `BenchmarkTextCodec/UCS2/70/Decode` | 795.9 | 800 | 2 |
| `BenchmarkTextCodec/UTF16BE/35/Encode` | 622 | 432 | 2 |
| `BenchmarkTextCodec/UTF16BE/35/Decode` | 507.6 | 720 | 2 |

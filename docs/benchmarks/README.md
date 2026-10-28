# Benchmark guide and historical measurements

[Back to smppkit](../../README.md#benchmarks) · [Release candidate results](r3-summary.md)

This guide preserves the methodology, historical tables and profiling commands
previously in the root README. The dated measurements below were not rerun for
this documentation reorganization. Use the R3 report for the release candidate's
full 80-case results; compare timings only with their stated measurement limits.
Run all commands from the repository root.

## Encode, decode and memory-copy benchmarks

Run the [benchmark suite](../../codec/benchmark_test.go) with allocation reporting:

```sh
go test ./codec -run='^$' -bench='BenchmarkCodec(Encode|Decode|Copy)$' \
  -benchmem -benchtime=200ms -count=3 -cpu=1
```

`Encode/NewBuffer` calls `Codec.Append(nil, p)` for each operation.
`Encode/ReuseBuffer` supplies a preallocated destination with capacity for the
whole frame and resets its length on every operation. `Decode` consumes an
already buffered frame and returns owned PDU data. Fixture/profile construction,
initial serialization, reusable buffer allocation and final result checks are
outside the timed `b.Loop`; results remain observable through benchmark sinks.
All profiles use an explicit 128 KiB MaxFrame so a 65,535-byte TLV plus frame
overhead fits. Production defaults are unchanged.

The suite includes submit_sm in 3.3/3.4/5.0, the 5.0 255-byte short_message
boundary, data_sm payload scaling, and submit_multi destination-list scaling.
Addresses are synthetic nonempty strings. Multi cases carry a 140-byte message;
data cases carry one message_payload TLV. These are Codec measurements, excluding
I/O, TLS, Session, contextual request/response checks and text conversion.

Local results, 2026-10-27: **Go 1.26.4**, linux/amd64 under WSL2, AMD Ryzen 7
6800H, `-cpu=1` (GOMAXPROCS=1, no CPU affinity), three 200 ms runs per case.
Each cell below is **ns/op / B/op / allocs/op**, using the median of the three
runs. B/op and allocs/op were stable across those runs. These short local
observations are not throughput guarantees or cross-version speed comparisons.
The [raw benchmark output](codec-go1.26.4-linux-amd64.txt) preserves
all three runs, including MB/s and allocation metrics.

| Workload | Wire bytes | Encode, new buffer | Encode, reused buffer | Decode, owned result |
| --- | ---: | ---: | ---: | ---: |
| submit_sm 3.3, 140-byte payload | 190 | 370.1 / 496 / 5 | 299.3 / 304 / 4 | 363.4 / 440 / 8 |
| submit_sm 3.4, 140-byte payload | 190 | 283.6 / 496 / 5 | 252.8 / 304 / 4 | 351.5 / 440 / 8 |
| submit_sm 5.0, 140-byte payload | 190 | 283.4 / 496 / 5 | 247.7 / 304 / 4 | 358.7 / 440 / 8 |
| submit_sm 5.0, 255-byte payload | 305 | 320.3 / 752 / 5 | 267.4 / 432 / 4 | 367.6 / 584 / 8 |
| data_sm 5.0, 140-byte payload | 187 | 393.6 / 496 / 5 | 368.1 / 304 / 4 | 479.9 / 408 / 9 |
| data_sm 5.0, 4,096-byte payload | 4,143 | 1,054.0 / 9,840 / 5 | 772.8 / 4,976 / 4 | 791.8 / 5,096 / 9 |
| data_sm 5.0, 65,535-byte payload | 65,582 | 10,160.0 / 147,568 / 5 | 6,775.0 / 73,840 / 4 | 4,951.0 / 73,960 / 9 |
| submit_multi 5.0, 2 destinations | 207 | 387.2 / 704 / 6 | 338.3 / 496 / 5 | 590.9 / 664 / 13 |
| submit_multi 5.0, 255 destinations | 4,002 | 8,836.0 / 12,272 / 10 | 8,823.0 / 8,176 / 9 | 30,493.0 / 52,696 / 779 |

Memory-copy baselines use the same lengths as the three data_sm frames. They
measure repeated Go `copy` on hot buffers, either allocating the destination
each iteration or reusing it; they perform no protocol work. Same units:

| Copied bytes/op | Allocate + copy | Reuse + copy |
| ---: | ---: | ---: |
| 187 | 57.0 / 192 / 1 | 5.8 / 0 / 0 |
| 4,143 | 430.3 / 4,864 / 1 | 35.4 / 0 / 0 |
| 65,582 | 6,402.0 / 73,728 / 1 | 1,008.0 / 0 / 0 |

`B/op` measures allocated heap bytes, **not copied bytes or retained memory**.
`allocs/op` counts heap allocations. Codec `MB/s` uses complete wire-frame bytes;
`wire-B/op` reports that frame length, not total memory traffic. Copy baseline
`MB/s` uses bytes passed to `copy`. Neither `-benchmem` nor this baseline counts
the actual copies inside Codec, and subtracting the copy baseline from codec
timings would not give an accurate breakdown.

The current [Codec implementation](../../codec/codec.go) builds a private encoded
frame, then copies it into the caller's destination. Growing that private frame
can copy previously written bytes. Reusing the output therefore removes one
allocation in these cases, but **does not make encoding zero-copy or allocation
free**. For the largest data_sm case, reuse reduces allocation from 147,568 to
73,840 B/op and from five to four allocations. Decode clones the body once to
establish ownership; payload and TLV slices refer into that owned clone, while
strings, typed bodies and lists require additional allocations. Each standard
byte field's capacity ends at its own length, so append cannot overwrite another
field; this requires no per-field copy. `Codec.Read` also allocates an input frame,
which the Decode benchmark intentionally excludes.
These copy paths describe source behavior, not benchmark-derived copy counters.

To inspect allocation sites separately (profiling changes timings):

```sh
go test ./codec -run='^$' \
  -bench='^BenchmarkCodecEncode$/^DataSM$/^V50$/^65535$/^NewBuffer$' \
  -benchmem -benchtime=200ms -cpu=1 -memprofilerate=1 \
  -memprofile=/tmp/smppkit-codec-alloc.pprof -o /tmp/smppkit-codec.test
go tool pprof -top -alloc_space /tmp/smppkit-codec.test /tmp/smppkit-codec-alloc.pprof
go tool pprof -top -alloc_objects /tmp/smppkit-codec.test /tmp/smppkit-codec-alloc.pprof
```

Profiles include test-harness/setup allocations as well as timed operations;
filter by Codec call paths when attributing costs. Keep the toolchain, fixture,
CPU setting and profiling flags consistent when comparing changes. Allocation
budgets are observations here, not hard-coded test assertions.

This local toolchain omits the prebuilt `pprof` executable. If that is also true
of your installation and the Go sources are available, replace `go tool pprof`
with `go run cmd/pprof`; the profile format and flags are the same.

## Additional performance workloads

The expanded suite adds **42 benchmark cases** beyond the existing encode/decode
and copy cases. All report allocations and use Go's `testing.B.Loop`. Each
fixture/setup is outside the measured loop unless stated below.

| Suite | Cases and measurement boundary |
| --- | --- |
| [Codec.Read](../../codec/stream_benchmark_test.go) | The same nine version/payload/destination fixtures as Decode, including frame acquisition allocation through a reused bytes.Reader. No sockets or Session. |
| Codec TLVs | Encode into a reused destination and Decode with 0/8/64 distinct customer TLVs, eight-byte values. Includes parameter validation and materialization; excludes custom validator callbacks. |
| Codec rejection | Short header and truncated TLV, including error construction. This is an error-path cost, not valid-frame throughput. |
| [Text codecs](../../textcodec/benchmark_test.go) | Encode/decode GSM basic and extension characters, Latin-1, UCS-2 and supplementary UTF-16BE. Encode includes character boundaries. MB/s uses UTF-8 input bytes for encode and encoded input bytes for decode. |
| [Message planning](../../message/benchmark_test.go) | Prepare versus Materialize for UDH8/UDH16/SAR, five segments from a fixed 560-byte UTF-8 string. Materialize includes owned copies and headers/TLVs; the plan is precomputed. No leased references or transport. |
| Receiver reassembly | Five segments, UDH16/SAR, ordered/reversed. One operation includes a fresh Receiver, all admissions and Receive with text decoding. Constructor cost is included to keep iterations independent and resource-bounded. |
| Receiver duplicate | Re-admit one buffered UDH segment; includes parsing/copying/hash checks. The retained group and byte accounting must remain unchanged. |
| [Session](../../session_benchmark_test.go) | EnquireLink and 140-byte SubmitSM round trips, plus single-part GSM7 and UCS-2 fallback SendText. Includes both endpoints, their codecs, and net.Pipe; excludes bind, startup/teardown, TCP/TLS and real network latency. Window=1, serial calls. |

Run just the new cases, or use `-bench=.` for all benchmarks:

```sh
go test . ./codec ./message ./textcodec -run='^$' \
  -bench='Benchmark(Codec(Read|TLVs|Reject)|TextCodec|MessagePlan|Receiver(Reassemble|Duplicate)|Session(Request|SendText))$' \
  -benchmem -benchtime=200ms -count=3 -cpu=1
```

Measured 2026-10-28: **Go 1.26.4**, linux/amd64 under WSL2, AMD Ryzen 7 6800H,
GOMAXPROCS=1, no CPU affinity, no race instrumentation. The new cases plus the
existing encode/decode/copy cases produced **75 cases × 3 runs** at 200 ms each.
The table shows representative medians; [raw results and exact command](expanded-go1.26.4-linux-amd64.txt)
include every case. These are new measurements, not a speedup claim against the
historical table above.

| Workload | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Decode data_sm, 140-byte payload | 595.5 | 408 | 9 |
| Read data_sm, same payload, including framing | 655.2 | 608 | 11 |
| Decode submit_sm with 64 customer TLVs | 10,471 | 10,040 | 22 |
| Encode GSM7, 160 basic characters | 2,713 | 1,440 | 2 |
| Encode UCS-2, 70 Chinese characters | 871.7 | 800 | 2 |
| Prepare UDH16, five segments | 10,781 | 7,608 | 13 |
| Materialize precomputed UDH16 plan, five segments | 604.4 | 1,664 | 16 |
| New Receiver + five reversed UDH16 segments + decoded Receive | 13,050 | 13,232 | 79 |
| Duplicate buffered UDH segment | 656.2 | 672 | 11 |
| Session enquire_link round trip | 10,206 | 1,664 | 30 |
| Session submit_sm round trip, 140-byte payload | 12,755 | 2,864 | 44 |
| SendText, single-part GSM7 | 23,110 | 5,608 | 59 |
| SendText, single-part UCS-2 fallback | 29,889 | 6,008 | 63 |

Session numbers include allocations by both endpoints' background goroutines;
they cannot be added to isolated Codec figures or interpreted as SMSC capacity.
The receiver constructor is measured only in reassembly, not duplicate admission.
These choices avoid hidden unbounded tombstone/reference growth across iterations.
Neither B/op nor allocs/op counts copies. The existing explicit copy benchmarks
remain memory-copy baselines, and every owned-result contract remains in force.

The [RV20/RV21 follow-up measurements](rv20-rv21-go1.26.4.txt)
compare 28 affected/adjacent cases before and after the fixes (Go 1.26.4,
100 ms x3, `-cpu=1`, no race instrumentation). All cases retained the same median
B/op and allocs/op; bounding decoded slice capacity adds no per-field copy.
Short-run timings were noisy, including unchanged paths and a reversed/fixed
control. The record retains those observations and a longer timing recheck;
it does not claim a speedup. All 80 benchmark cases also passed a separate
race smoke run, whose timings are not comparable with these measurements.

### R3 candidate measurements

The [R3 benchmark report](r3-summary.md) records a fresh run of
all **80 cases × 3**, Go 1.26.4, 200 ms per case, `-benchmem -cpu=1`, without
race instrumentation. All 75 cases shared with the expanded historical suite
retain the same median B/op and allocs/op. Representative candidate medians:

| Workload | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Encode data_sm, 65,535-byte payload, new buffer | 14,093 | 147,568 | 5 |
| Encode same frame, reused output | 9,179 | 73,840 | 4 |
| Decode same frame, owned result | 6,817 | 73,960 | 9 |
| Copy 65,582 bytes, reused destination | 1,098 | 0 | 0 |
| Prepare UDH16, five segments | 12,028 | 7,608 | 13 |

Timing changes against older runs are not controlled code comparisons. The
largest increase (Read data_sm/4096) measured 2880 ns/op in the full run and
1932 ns/op in an isolated 500 ms ×5 recheck, with unchanged allocation counts.
Both outputs and the unresolved host-timing variability are retained in the
report. R3 did not change library runtime code or claim a performance gain.

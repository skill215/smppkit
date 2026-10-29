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

<a id="optional-logging-v110-unreleased"></a>

## Optional logging (v1.1.0)

The new suites measure a successful request diagnostic in isolation, and the
same logging mode during one Session enquiry/automatic response over net.Pipe.
Both endpoints use the configured logger; only the initiating Request emits a
per-exchange record. Setup, bind, startup/terminal diagnostics and cleanup are
outside the timed loop. These results do not measure TCP/TLS or network TPS.

```sh
go test . -run='^$' -bench='Benchmark(LoggingEvent|SessionLogging)$' \
  -benchmem -benchtime=200ms -count=3 -cpu=1
```

Local results, 2026-10-29: Go 1.26.10, linux/amd64 under WSL2, AMD Ryzen 7
6800H, GOMAXPROCS=1 without CPU affinity. Each row is the median of three runs;
B/op and allocs/op were stable. [Raw output](logging-go1.26.10-linux-amd64.txt).

| Workload | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Default nil — isolated event | 3.337 | 0 | 0 |
| Explicit discard — isolated event | 7.218 | 0 | 0 |
| Info threshold — isolated event | 6.778 | 0 | 0 |
| Enabled sink — isolated event | 440 | 240 | 1 |
| JSON — isolated event | 1741 | 240 | 1 |
| Text — isolated event | 1624 | 240 | 1 |
| Default nil — Session round trip | 7343 | 1664 | 30 |
| Explicit discard — Session round trip | 6892 | 1664 | 30 |
| Info threshold — Session round trip | 6948 | 1664 | 30 |
| Enabled sink — Session round trip | 7816 | 1904 | 31 |
| JSON — Session round trip | 8878 | 1904 | 31 |
| Text — Session round trip | 9158 | 1904 | 31 |

`DefaultNil` is normalized outside timing to the shared discard logger.
`Discard` is an explicitly supplied slog.DiscardHandler. `FilteredInfo` uses
JSONHandler at Info, so successful Debug events are filtered. `EnabledSink`
constructs a record but performs no formatting/output. JSON and Text include
formatting and write to io.Discard, excluding actual output-device cost.

B/op measures heap allocation, not bytes copied or retained. These suites do
not count copies; the diagnostic path does not copy message payloads. The
isolated disabled cases allocate no record. Enabled request records currently
need one allocation; complete Session exchanges still allocate for framing,
correlation, timers and jobs. No comparison with a separately measured v1.0.0
build or claim of zero total Session allocation is made. Costs for custom
handlers, failures, stack capture, sinks and different hardware may differ.


<a id="udh-language-protection-v110-unreleased"></a>

## UDH language protection (v1.1.0)

Local execution on 2026-10-29, Go 1.26.10, linux/amd64 WSL2, Ryzen 7 6800H.
Median of three 200 ms runs with one CPU worker; no CPU affinity. Command:

```sh
go test ./message -run '^$' -bench '^BenchmarkReceiveLanguagePolicy$' -benchmem -benchtime=200ms -count=3 -cpu=1
```

| Five-byte user data | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Empty UDH plus ordinary GSM7 text | 1147 | 520 | 8 |
| Opaque IE plus ordinary GSM7 text | 1090 | 528 | 9 |
| Language declaration, decoding blocked | 1048 | 520 | 8 |
| Language declaration, raw-only reception | 1018 | 520 | 8 |

[Raw output](udh-go1.26.10-linux-amd64.txt). These measure Admit + Receive,
including owned copies, ID generation and optional text decoding. The receiver
is reused, its construction is excluded, and completed messages release their
charges each iteration. UDH/payload proportions differ despite equal total input
length. Results are not a before/after speed comparison or an isolated guard cost.
B/op is allocated memory, not bytes copied. Source inspection shows the guard
scans the already-owned IE slice without an additional payload copy; this suite
does not instrument copy counts or claim zero-copy reception.


<a id="raw-framing-and-diagnostic-errors-v110-unreleased"></a>

## Raw framing and diagnostic errors (v1.1.0)

Local results, 2026-10-29: Go 1.26.10, linux/amd64 WSL2, Ryzen 7 6800H,
three 200 ms runs per case, -cpu=1, no CPU affinity. Command:

```sh
go test ./codec -run '^$' -bench '^(BenchmarkRawFrame|BenchmarkDecodeDiagnostic)$' -benchmem -benchtime=200ms -count=3 -cpu=1
```

| Workload | Median ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| RawFrame/Read/16 | 52.95 | 20 | 2 |
| RawFrame/WriteDiscard/16 | 5.07 | 0 | 0 |
| RawFrame/WriteBuffer/16 | 8.193 | 0 | 0 |
| RawFrame/Read/256 | 82.52 | 260 | 2 |
| RawFrame/WriteDiscard/256 | 5.061 | 0 | 0 |
| RawFrame/WriteBuffer/256 | 10.39 | 0 | 0 |
| RawFrame/Read/4096 | 409 | 4100 | 2 |
| RawFrame/WriteDiscard/4096 | 4.874 | 0 | 0 |
| RawFrame/WriteBuffer/4096 | 34.33 | 0 | 0 |
| RawFrame/Read/65536 | 4552 | 65540 | 2 |
| RawFrame/WriteDiscard/65536 | 4.91 | 0 | 0 |
| RawFrame/WriteBuffer/65536 | 980.2 | 0 | 0 |
| DecodeDiagnostic/DecodeCompleteBadBody | 338.2 | 360 | 8 |
| DecodeDiagnostic/ReadCompleteBadBody | 427.3 | 456 | 11 |

[Raw output](framing-go1.26.10-linux-amd64.txt). Read includes allocation of
owned frame bytes and copying from bytes.Reader. Successful reads use two
allocations (length scratch and frame); partial/error paths are distinct.
WriteDiscard measures validation and dispatch only: io.Discard does not copy the
payload, so its MB/s is not memory bandwidth or network throughput. WriteBuffer
uses a preallocated reused bytes.Buffer and includes its copy. WriteFrame itself
creates no frame copy; B/op is allocated memory, not copied-byte accounting.
No sockets, TLS, flush, kernel buffers or Session work is measured. Error cases
include safe error context creation; they are not successful Decode benchmarks.
No before/after speedup is claimed. Existing successful CodecDecode/CodecRead
workloads were also run once for 100 ms each as a smoke check, recorded separately
in [smoke output](framing-codec-smoke-go1.26.10-linux-amd64.txt).


<a id="heartbeat-paths-v110-unreleased"></a>

## Heartbeat paths (v1.1.0)

Local Go 1.26.10 results, 2026-10-29, linux/amd64 WSL2, Ryzen 7 6800H.
Three 200 ms runs per case, -cpu=1, no CPU affinity. Command:

```sh
go test . -run '^$' -bench '^BenchmarkHeartbeat(Request|Lifecycle)$' -benchmem -benchtime=200ms -count=3 -cpu=1
```

| Workload | Median ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Explicit enquiry round trip | 7038 | 1696 | 30 |
| Automatic probe request path | 7070 | 1696 | 30 |
| Full-window skip | 514.8 | 272 | 4 |
| Construct/close: disabled, bound | 3513 | 4912 | 38 |
| Construct/close: enabled, before bind | 3675 | 5176 | 42 |
| Construct/close: enabled, bound | 4090 | 5300 | 43 |

[Raw output](keepalive-go1.26.10-linux-amd64.txt). Round trips include both
endpoints' Codec, request correlation, timers and net.Pipe copies, with startup
excluded. The automatic case directly invokes the scheduler's shared request
path; it excludes interval waiting/wakeup and scheduler diagnostic dispatch.
The full-window case includes creating/cancelling the request deadline context,
without a frame write or sequence allocation. These are not network TPS results.

Lifecycle cases include net.Pipe creation, all Session setup, goroutine startup,
Close and Wait. Immediate close races scheduler startup; EnabledBound may create
its timer before stopping, so byte/allocation averages depend on that scheduling.
These numbers are not a pure per-timer allocation measurement. Disabled scheduling
creates no heartbeat-specific goroutine/channel/timer; the row includes the
ordinary Session machinery. B/op measures allocations, not copied bytes; no
instrumented copy count or zero-copy transport claim is made. No comparison with
historical benchmark environments or before/after speedup is claimed.

The [S5 correction rerun](s5-fixes-keepalive-go1.26.10-linux-amd64.txt) uses the
same command on Go 1.26.10 after RV22/RV23. Request paths still allocate
1696 B / 30 allocations, and full-window skips 272 B / 4 allocations; lifecycle
allocation counts remain 38/42/43. Timings differ between runs; these short
unpaired samples do not establish a throughput improvement or regression. The
new cancellation branch adds no buffer copies or new allocation sites.


<a id="receipt-helpers-v110-unreleased"></a>

## Receipt helpers (v1.1.0)

Local Go 1.26.10 results, 2026-10-29, linux/amd64 WSL2, Ryzen 7 6800H.
Three 200 ms runs, -cpu=1, no CPU affinity; timing noise is visible in the raw
samples. Command:

```sh
go test ./message -run '^$' -bench '^BenchmarkReceipt(Parse|Build)$' -benchmem -benchtime=200ms -count=3 -cpu=1
```

| Workload | Median ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Parse/FullText | 2529 | 784 | 7 |
| Parse/TLVOnly | 313 | 136 | 6 |
| Parse/Mixed | 2912 | 920 | 13 |
| Parse/MissingFields | 1194 | 1072 | 9 |
| Parse/Malformed | 2445 | 1640 | 17 |
| Parse/RawOnly | 387.9 | 328 | 8 |
| Build/LegacyText | 2449 | 2008 | 17 |
| Build/Text | 822.3 | 1112 | 16 |
| Build/TLVOnly | 790 | 608 | 12 |
| Build/Mixed | 1333 | 1496 | 20 |
| Build/DataMixed | 1342 | 1288 | 20 |

[Raw output](receipts-go1.26.10-linux-amd64.txt). Parse includes owned raw evidence,
text decoding/field extraction when selected, and any diagnostics. Its typed input
is constructed outside timing. FullText is a 101-byte ASCII report; TLVOnly contains
ID/state/network error; Mixed combines them. MissingFields/Malformed intentionally
exercise diagnostic allocation. RawOnly retains mixed raw evidence and extracts
TLVs without parsing text. These workloads have different content and are not
before/after speed comparisons.

Build includes owned PDU storage and the standard Codec preflight (including a
temporary encoded buffer). LegacyText also includes the existing GSM7 encoder's
boundary metadata; 3.4/5.0 text construction uses ASCII DCS 1. TLVOnly omits text;
Mixed/DataMixed build both representations. The full text fixture is supplied
explicitly, without inferred counts, timestamps or delivery state.

Parser raw payloads/TLVs are cloned; text strings own their decoded storage.
Constructors copy values into new PDUs and allocate the preflight wire buffer.
B/op measures allocations, not the number of copied bytes. No zero-copy claim or
instrumented copy count is made. These are stateless helper costs, excluding
Session, sockets, ACKs, persistence, tracking and delivery TPS.

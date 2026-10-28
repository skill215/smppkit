# smppkit

A general-purpose Go SMPP library for clients, servers, and simulators. Module
`github.com/skill215/smppkit`, root package `smpp`, Apache-2.0.

**This library is AI-written.** OpenAI Codex generated and iteratively revised
the implementation under maintainer direction. See [AI authorship and review
status](#ai-authorship-and-review-status) for the agent, scope and validation limits.

**Requires Go 1.26 or newer.** The minimum in `go.mod` is `go 1.26.0`.
The [standard-library usage audit](docs/go-modernization.md) records modern Go
API choices, compatibility boundaries and before/after allocation checks.
The [Go 1.27 assessment](docs/go1.27-assessment.md) maps new APIs and runtime
changes to this code. The Go 1.27 upgrade is deferred, with Go 1.26 retained as
the minimum.

**Version 1.0.0** provides the documented SMS subset of SMPP 3.3, 3.4 and 5.0.
See the [command/version matrix](docs/support.md),
[API contracts](docs/decisions.md), [compatibility policy](docs/compatibility.md)
and [changelog](CHANGELOG.md). Stable API support does not imply full SMPP
conformance or interoperability with every SMSC.

```sh
go get github.com/skill215/smppkit@v1.0.0
```

[R1/R2](docs/r2-contract-audit.md), [R3 local validation](docs/r3-validation.md)
and [R4 preparation](docs/r4-readiness.md) record the implementation review,
regressions and local evidence. The [1.0 release record](docs/release-1.0.md)
links the submitted candidate's successful remote CI and publication procedure.
Historical validation records retain the status and limitations at their run date.
The subsequent [RV20/RV21 corrections](docs/review-final.md#rv20rv21-corrections)
isolate decoded byte-field append capacity and restrict automatic buffered ACKs
to requests whose meaning is compatible with memory admission.
The [field semantics inventory](docs/field-semantics.md) documents strict wire
checks, version differences and the boundary with carrier policy.
Session automatically checks [data_sm direction semantics](docs/data-direction.md);
standalone Codec users can supply the same context explicitly.
Response checks also use the [original data_sm transaction mode](docs/data-transaction.md).

```go
c, err := codec.New(codec.Config{Version: pdu.V50})
if err != nil { return err }
s, err := smpp.Dial(ctx, "localhost:2775", smpp.ClientConfig{
    Codec: c,
    Bind: &pdu.BindTransceiver{BindFields: pdu.BindFields{
        SystemID: "demo", Password: password, InterfaceVersion: pdu.V50,
    }},
})
if err != nil { return err }
defer func() { s.Close(); s.Wait() }()
result, err := s.SendText(ctx, smpp.TextMessage{
    Source: pdu.Address{Value: "example"},
    Destination: pdu.Address{TON: 1, NPI: 1, Value: "12345"},
    Text: text,
}, message.GSMText())
// Inspect result.Segments even when err != nil.
```

`message.GSMText()` explicitly selects unpacked GSM default-alphabet encoding,
strict UCS-2 fallback, a 140-octet radio budget, and 16-bit UDH concatenation.
Configure a different `message.Policy` for a peer with different capabilities.
The plan reports the effective encoding. Invalid text and all segment bodies
are validated before the first request is written. Input strings are never
silently sent as UTF-8 after an encoding error.
Built-in Latin-1 is available for 3.4/5.0; selecting it for 3.3, including after
fallback, returns a policy error before sending. Custom encodings must explicitly
map their DCS to the chosen version and peer.

Submission is not atomic. Each segment retains its request outcome, peer status,
and opaque message ID. A timeout after writing is uncertain; smppkit never
retries it automatically. An ordinary successful submit response means acceptance,
not final delivery; explicit transaction responses require interpreting the peer's
reported outcome. Delivery receipts remain separate incoming messages.

For complete-message reception, create a `message.Receiver` with explicit
`AckOnBuffered`, run `s.RunReceiver(ctx, receiver, "")` as the sole inbound
consumer, and consume complete messages with `receiver.Receive(ctx)` in a
separate worker. The empty namespace uses the session generation. ACK follows
each segment's bounded memory admission, including while the rest of a multipart
message is missing. Memory ACK does not survive a process crash. Application
completion cannot retract it. Consume `receiver.Events()` periodically to observe
expired groups. For mixed-command dispatch, use `s.Receive` and
`smpp.AdmitMessage` instead of `RunReceiver`.
Explicit transaction submissions (mode 2) cannot use memory ACK: `AdmitMessage`
returns `ErrState` before admission or response, leaving the handle available
for application `Respond`. `RunReceiver` exits and closes the session without
ACK. Implement such submissions through `Receive/Respond` with an actual result.
Mode 0 requires a configured non-transaction default at the MC; it is not inferred
from the wire. MC-to-ESME deliveries are unaffected by this submission restriction.
Persistence, durable ACK and application crash recovery belong to the consuming
application and are outside the library scope.

Runnable examples ([setup, versions and error paths](examples/README.md)):

- [Client and long-text submission](examples/client/main.go).
- [Server and complete-message reception](examples/server/main.go).
- [Standalone codec for a simulator](examples/codec/main.go).
- [Standalone data_sm direction checks](examples/data-direction/main.go).
- [Standalone data_sm transaction response checks](examples/data-transaction/main.go).
- [Outbind handshake and one-way availability notifications](examples/notifications/main.go).
- [Segmentation preview without networking](examples/multipart/main.go).
- [Scoped customer TLV definition](examples/custom-tlv/main.go).
- [Typed private command pair](examples/private-command/main.go).
- [submit_multi with individual failures](examples/submit-multi/main.go), a
  self-contained in-memory client/server demonstration (`go run ./examples/submit-multi`).

For multi-destination submission, use `Session.Request` with `pdu.SubmitMulti`.
Inspect `SubmitMultiResp.Unsuccessful` even if the request returned no error.
This operation submits one encoded payload to SME addresses or peer-owned list
names; it does not automatically segment text or expand distribution lists.
See the [standard TLV matrix](docs/tlv-support.md) for validated tags and versions.

For one-way alerts, an MC uses `Session.Notify` and an RX/TRX ESME consumes
`Session.ReceiveNotification` independently of its message receiver. These
packets need no ACK and do not occupy the request window. `Server.Outbind`
supports MC-initiated receiver binds on an existing connection, with explicit
ESME credential validation. See the [notification contract](docs/notifications.md)
or run `go run ./examples/notifications` for a self-contained example.

`Shutdown(ctx)` performs a bounded unbind exchange, including simultaneous peer
unbind, then closes. `Close()` aborts; `Wait()` joins Session's goroutines.
Finish application work before shutdown. Unread input is discarded on close.
See [Session lifecycle and independent peer checks](docs/session-lifecycle.md)
for cancellation, ownership and the three-version local TCP test scope.

Optional [local resource and legacy simulator checks](docs/local-validation.md)
exercise sustained loopback traffic, receiver saturation and the
[skill215/go-smpp](https://github.com/skill215/go-smpp) SMSC simulator,
specifically its [smpptest.Server implementation](https://github.com/skill215/go-smpp/blob/743fa92c92a2e87fc64d53c677f774a856c1b6e3/smpp/smpptest/server.go).
The [legacy harness and invocation](interop/legacy/README.md) run in a temporary module and add no
runtime dependency to smppkit; its measured compatibility scope is SMPP 3.4 UDH.
The default legacy request echo is adapted through its public handler. Known
bind-sequence and missing-TLV-advertisement limitations are documented in the
local validation record. This link does not imply external SMSC certification.

For a local demo, set the same `SMPP_PASSWORD` (at most 8 octets) in two shells:

```sh
# Terminal 1 (set SMPP_PASSWORD=demo123 in both terminals):
go run ./examples/server -echo

# Terminal 2:
go run ./examples/client -version 5.0 -wait-delivery -text 'Hello from smppkit'
```

The example server accepts 3.3/3.4/5.0; echo mode requires a 3.4/5.0 transceiver.
The client supports `-version`, `-timeout`, addresses and optional delivery wait,
reports every segment, and unbinds with a separate bounded cleanup deadline.
Long text is automatically segmented. Echoed deliver_sm traffic is not a delivery
receipt. See [long-text commands and limitations](examples/README.md).

The demo uses plain loopback TCP. `ClientConfig.TLS` and a `tls.Listener` support
TLS; existing `net.Conn`/TLS connections can also be supplied directly. No HTTP,
database, logging framework, application routing, or private consumer code is
required. A simulator can import only `codec`, `pdu`, and `tlv` and keep ownership
of its runtime.

Validation:

```sh
go build ./...
go vet ./...
go test -race -count=1 -timeout=90s ./...
go test ./codec -run='^$' -fuzz=FuzzDecode -fuzztime=5s -parallel=2
go test ./codec -run='^$' -fuzz=FuzzSubmitMulti -fuzztime=5s -parallel=2
go test ./codec -run='^$' -fuzz=FuzzStandardTLVSection -fuzztime=5s -parallel=2
go test ./codec -run='^$' -fuzz=FuzzSemanticTimes -fuzztime=5s -parallel=2
go test ./codec -run='^$' -fuzz=FuzzDataDirection -fuzztime=5s -parallel=2
go test ./codec -run='^$' -fuzz=FuzzDataResponse -fuzztime=5s -parallel=2
go test ./message -run='^$' -fuzz=FuzzUDH -fuzztime=5s -parallel=2
go test ./message -run='^$' -fuzz=FuzzAdditionalUDH -fuzztime=5s -parallel=2
go test ./message -run='^$' -fuzz=FuzzPrepareVersionEncoding -fuzztime=5s -parallel=2
go test ./textcodec -run='^$' -fuzz=FuzzCharsets -fuzztime=5s -parallel=2
go test . ./codec ./message ./textcodec -run='^$' -bench=. -benchmem
```

Tests include independent protocol bytes and a raw TCP peer, beyond shared-codec
round trips. CI is configured for Go 1.26 and 1.27; local results identify the
actual toolchain used. `/doc/` contains ignored local design material; public
documentation lives in this README, package comments, and `docs/`.

## Encode, decode and memory-copy benchmarks

Run the [benchmark suite](codec/benchmark_test.go) with allocation reporting:

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
The [raw benchmark output](docs/benchmarks/codec-go1.26.4-linux-amd64.txt) preserves
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

The current [Codec implementation](codec/codec.go) builds a private encoded
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
| [Codec.Read](codec/stream_benchmark_test.go) | The same nine version/payload/destination fixtures as Decode, including frame acquisition allocation through a reused bytes.Reader. No sockets or Session. |
| Codec TLVs | Encode into a reused destination and Decode with 0/8/64 distinct customer TLVs, eight-byte values. Includes parameter validation and materialization; excludes custom validator callbacks. |
| Codec rejection | Short header and truncated TLV, including error construction. This is an error-path cost, not valid-frame throughput. |
| [Text codecs](textcodec/benchmark_test.go) | Encode/decode GSM basic and extension characters, Latin-1, UCS-2 and supplementary UTF-16BE. Encode includes character boundaries. MB/s uses UTF-8 input bytes for encode and encoded input bytes for decode. |
| [Message planning](message/benchmark_test.go) | Prepare versus Materialize for UDH8/UDH16/SAR, five segments from a fixed 560-byte UTF-8 string. Materialize includes owned copies and headers/TLVs; the plan is precomputed. No leased references or transport. |
| Receiver reassembly | Five segments, UDH16/SAR, ordered/reversed. One operation includes a fresh Receiver, all admissions and Receive with text decoding. Constructor cost is included to keep iterations independent and resource-bounded. |
| Receiver duplicate | Re-admit one buffered UDH segment; includes parsing/copying/hash checks. The retained group and byte accounting must remain unchanged. |
| [Session](session_benchmark_test.go) | EnquireLink and 140-byte SubmitSM round trips, plus single-part GSM7 and UCS-2 fallback SendText. Includes both endpoints, their codecs, and net.Pipe; excludes bind, startup/teardown, TCP/TLS and real network latency. Window=1, serial calls. |

Run just the new cases, or use `-bench=.` for all benchmarks:

```sh
go test . ./codec ./message ./textcodec -run='^$' \
  -bench='Benchmark(Codec(Read|TLVs|Reject)|TextCodec|MessagePlan|Receiver(Reassemble|Duplicate)|Session(Request|SendText))$' \
  -benchmem -benchtime=200ms -count=3 -cpu=1
```

Measured 2026-10-28: **Go 1.26.4**, linux/amd64 under WSL2, AMD Ryzen 7 6800H,
GOMAXPROCS=1, no CPU affinity, no race instrumentation. The new cases plus the
existing encode/decode/copy cases produced **75 cases × 3 runs** at 200 ms each.
The table shows representative medians; [raw results and exact command](docs/benchmarks/expanded-go1.26.4-linux-amd64.txt)
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

The [RV20/RV21 follow-up measurements](docs/benchmarks/rv20-rv21-go1.26.4.txt)
compare 28 affected/adjacent cases before and after the fixes (Go 1.26.4,
100 ms x3, `-cpu=1`, no race instrumentation). All cases retained the same median
B/op and allocs/op; bounding decoded slice capacity adds no per-field copy.
Short-run timings were noisy, including unchanged paths and a reversed/fixed
control. The record retains those observations and a longer timing recheck;
it does not claim a speedup. All 80 benchmark cases also passed a separate
race smoke run, whose timings are not comparable with these measurements.

### R3 candidate measurements

The [R3 benchmark report](docs/benchmarks/r3-summary.md) records a fresh run of
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

## AI authorship and review status

**This library was written by AI agents, using OpenAI Codex, under human
maintainer direction.** AI generated and iteratively revised the implementation,
tests, benchmarks, examples and substantial portions of the documentation.
This is a disclosure of authorship, not merely use of autocomplete.

| Item | Disclosure |
| --- | --- |
| Known coding agent / provider | OpenAI Codex / OpenAI. A complete per-change model-version history has not been recorded; no exact generation percentage is claimed. |
| Human role | The maintainer supplied requirements, constrained the public-library scope, reviewed progress and directed corrections. This does not claim an independent line-by-line human audit. |
| Validation | Build/vet/race, targeted fuzzing, independent protocol-byte fixtures and local interoperability checks are recorded in [validation](docs/validation.md). Benchmarks report executed local measurements with their boundaries. |
| Review limits | Agent review and AI-authored tests can share mistakes with the implementation. Independent wire fixtures mean independence from the production serializer, not independent human authorship or conformance certification. External SMSC interoperability remains unverified. |
| Responsibility | Maintainers remain responsible for accepted changes and release decisions. Contributors should disclose the tool and scope of material AI-generated changes and review/test them. The declared project license is unchanged by this notice. |

The library itself has no AI runtime dependency. Assess the implementation and
its documented evidence before using it in your environment; neither the agent
name nor passing tests establishes production readiness.

This project-specific statement draws on the GitHub-hosted community
[AI Disclosure Convention](https://github.com/ggfevans/ai-disclosure) and
[GitHub's guidance for reviewing AI-generated code](https://docs.github.com/en/copilot/tutorials/review-ai-generated-code),
checked on 2026-10-28. The community convention is a draft, not a mandatory GitHub
library certification. We use explicit prose rather than implying an unperformed
human review through a badge or automatically adding provenance tags to every file.

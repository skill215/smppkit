# smppkit

**SMPP for Go — clients, servers, and standalone protocol tools.**

Send text, segment long messages, and reassemble incoming SMS with explicit
outcomes and bounded resources. One shared Session and versioned Codec support
**SMPP 3.3, 3.4, and 5.0**.

**Go 1.26+** · [Releases](https://github.com/skill215/smppkit/releases) ·
[Apache-2.0](LICENSE) · No third-party runtime module dependencies

> **AI-written:** OpenAI Codex generated this library under maintainer direction.
> Model: **GPT-6.0 Astra**. See [AI_DISCLOSURE.md](AI_DISCLOSURE.md).

[Quick start](#quick-start) · [Go API](#go-api) · [Protocol support](#protocol-support) ·
[Examples](#examples) · [Benchmarks](#benchmarks) · [Documentation](#documentation)

**v1.1.0** adds optional diagnostics, keepalive and bounded protocol utilities:

- Optional [slog diagnostics](docs/logging.md), with discard as the default.
- [Active enquire_link](docs/keepalive.md), disabled by default on both endpoints.
- [Raw framing and classified decode errors](docs/framing.md) for standalone tools.
- [DLR parsing and construction](docs/receipts.md), with explicit dialects and separate evidence.
- [UDH national-language protection](docs/v1.1.0-design.md#u1-national-language-declarations-and-text-safety)
  to prevent unsupported text decoding.

See the [release notes](docs/releases/v1.1.0.md) and
[submission/publication gates](docs/v1.1.0-release.md). Go 1.26 remains the minimum.

## What it provides

- **Client and Server:** bind/authentication, TCP/TLS, request correlation,
  configurable windows, cancellation, deadlines, and bounded shutdown.
- **Text and multipart SMS:** explicit encoding policies, automatic UDH8/UDH16/SAR
  segmentation in 3.4/5.0, per-segment results, bounded reassembly, and expiry events.
- **Standalone Codec:** concrete PDU structs, version-specific layouts, owned
  decoded data, and scoped extensions for simulators and protocol tools.
- **Standard and customer TLVs:** validation and preservation where supported
  in 3.4/5.0; no TLVs in 3.3.
- **Explicit failure semantics:** no automatic retry of uncertain requests;
  submission acceptance and final delivery remain separate.

The library has no database, routing engine, or delivery-state service. Those
remain application responsibilities.

## Quick start

### Install

Requires **Go 1.26 or newer** (`go 1.26.0` in `go.mod`). The root package is named
`smpp`, although the module is named `smppkit`.

```sh
go get github.com/skill215/smppkit@v1.1.0
```

Versioned installation requires the corresponding Git tag. Before publication,
use the source checkout below; see the [publication record](docs/v1.1.0-release.md)
and [GitHub Releases](https://github.com/skill215/smppkit/releases) for availability.

### Run a local server and client

Clone the repository, then run the following commands from its root in two
terminals. The examples use plain loopback TCP and synthetic credentials.

```sh
git clone https://github.com/skill215/smppkit.git
cd smppkit
```

**Terminal 1 — receive and echo text:**

```sh
export SMPP_PASSWORD=demo123
go run ./examples/server -echo
```

**Terminal 2 — submit and receive the echo:**

```sh
export SMPP_PASSWORD=demo123
go run ./examples/client -version 5.0 -wait-delivery -text 'Hello from smppkit'
```

The client prints each segment's result and performs a bounded unbind. Long text
is segmented automatically; text such as `你好，SMPP` selects strict UCS-2 fallback.
The echoed `deliver_sm` is a new message, **not a delivery receipt**.

See the [example guide](examples/README.md) for long-text commands, SMPP 3.3/3.4,
authentication failures, timeouts, and cleanup.

## Go API

A client connects and binds with `smpp.Dial`; a server admits connections through
`smpp.Server`. Both use `*smpp.Session`. For a simulator, use `codec`, `pdu`, and
`tlv` directly without a Session.

The following helper submits text through an already-bound client TX/TRX Session.
The caller owns the Session and supplies a context with a deadline.
See the [client example](examples/client/main.go) for dialing, binding and shutdown.

```go
package example

import (
	"context"
	"fmt"

	smpp "github.com/skill215/smppkit"
	"github.com/skill215/smppkit/message"
	"github.com/skill215/smppkit/pdu"
)

func SendText(ctx context.Context, session *smpp.Session, text string) error {
	result, err := session.SendText(ctx, smpp.TextMessage{
		Source:      pdu.Address{Value: "example"},
		Destination: pdu.Address{TON: 1, NPI: 1, Value: "12345"},
		Text:        text,
	}, message.GSMText())

	// Inspect every segment even when the overall send returns an error.
	for _, part := range result.Segments {
		fmt.Printf("part=%d outcome=%v message_id=%q err=%v\n",
			part.Number, part.Result.Outcome, part.MessageID, part.Err)
	}
	return err
}
```

`message.GSMText()` selects unpacked GSM7 with strict UCS-2 fallback, a 140-octet
radio budget, and UDH16 concatenation. Supply a different `message.Policy` when
your peer requires it. All planned segments are validated before the first write;
a failure during sending stops subsequent submissions and retains every result.

For reception, `message.Receiver` provides bounded reassembly. `RunReceiver`
uses explicit `AckOnBuffered`: it ACKs each segment after memory admission,
while another consumer reads complete messages. Drain expiry events too.
Use `Receive/Respond` for application-controlled ACKs and transaction results.
See the [server example](examples/server/main.go) and [ACK contract](docs/decisions.md).

`Shutdown(ctx)` exchanges unbind and closes; `Close()` aborts and `Wait()` joins
Session goroutines. Applications still own their workers and processing lifecycle.

### Optional diagnostics (v1.1.0)

Pass `smpp.Config{Logger: logger}` through `ClientConfig.Session`,
`ServerConfig.Session`, or `NewSession`, where `logger` is a `*slog.Logger`.
Nil means discard; global loggers are never used or changed. For example:

```go
logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))
sessionConfig := smpp.Config{Logger: logger}
```

See the [logging contract](docs/logging.md) for safe fields, synchronous handler
requirements and shutdown behavior, and the [bridge example](examples/logging/main.go)
for application-owned output. Client/Server demos accept `-diagnostics`.

## Protocol support

Version selection is explicit; there is no automatic downgrade.

| Capability | SMPP 3.3 | SMPP 3.4 | SMPP 5.0 |
| --- | --- | --- | --- |
| Transmitter / receiver bind | Yes | Yes | Yes |
| Transceiver bind | — | Yes | Yes |
| submit_sm / deliver_sm / submit_multi | Yes | Yes | Yes |
| query_sm / replace_sm / cancel_sm | Wire/API support | Wire/API support | Wire/API support |
| data_sm / outbind / alert_notification | — | Yes | Yes |
| Standard and customer TLVs | None | Yes | Yes |
| Automatic UDH8 / UDH16 / SAR multipart | — | Yes | Yes |
| Single-part text | Yes, encoding limits apply | Yes | Yes |
| Broadcast | — | — | Not implemented |

See the [full command/role matrix](docs/support.md) and [standard TLV matrix](docs/tlv-support.md)
for command-specific constraints. `submit_multi` uses `Session.Request`; inspect
individual unsuccessful SMEs even when the response header is OK.

### Semantics to preserve

- **Acceptance is not delivery.** Ordinary submission success is acceptance.
  Explicit transaction responses require mode-aware interpretation of the peer's
  outcome. Cancellation after writing may leave the outcome uncertain.
- **Multipart sends are not atomic.** Inspect every segment, including unsent,
  rejected and uncertain parts. smppkit does not automatically retry them.
- **Memory ACK is not durable.** `AckOnBuffered` does not survive a crash.
  Explicit transaction submissions cannot use this automatic ACK path; mode 0
  requires a configured non-transaction MC default. Use `Receive/Respond` for
  application-owned transaction results.
- **Support has boundaries.** Standard 3.3 multipart and built-in Latin-1 in 3.3,
  packed GSM7, national-language tables, receipt dialect parsing/aggregation,
  historical excluded 3.3 commands and broadcast are outside v1.0.0.

## Examples

| Task | Example |
| --- | --- |
| Connect, send text and receive deliveries | [client](examples/client/main.go) |
| Authenticate peers and receive complete messages | [server](examples/server/main.go) |
| Bridge optional slog diagnostics to application output | [logging](examples/logging/main.go) (v1.1.0) |
| Encode/decode without networking | [codec](examples/codec/main.go) |
| Preview text segmentation | [multipart](examples/multipart/main.go) |
| Submit to multiple destinations | [submit-multi](examples/submit-multi/main.go) |
| Validate data_sm direction / transaction context | [data-direction](examples/data-direction/main.go) · [data-transaction](examples/data-transaction/main.go) |
| Use outbind and one-way alerts | [notifications](examples/notifications/main.go) |
| Extend TLVs / private commands | [custom-tlv](examples/custom-tlv/main.go) · [private-command](examples/private-command/main.go) |

The optional interoperability harness uses the
[skill215/go-smpp SMSC simulator](https://github.com/skill215/go-smpp/blob/743fa92c92a2e87fc64d53c677f774a856c1b6e3/smpp/smpptest/server.go).
It runs in a separate temporary module and adds no library dependency.
Its verified scope is **SMPP 3.4 TRX with UDH**; external SMSC interoperability
remains unverified. [Run the harness and read its limits](interop/legacy/README.md).

## Benchmarks

### Encode, decode and memory-copy benchmarks

Selected release-candidate medians, measured **2026-10-28** with **Go 1.26.4**,
linux/amd64 under WSL2, AMD Ryzen 7 6800H. Three 200 ms runs per case,
`GOMAXPROCS=1`, no CPU affinity or race instrumentation. These are existing
measurements, not new results from this README update.

| Workload | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Encode submit_sm 5.0, 140-byte payload, new buffer | 362.2 | 496 | 5 |
| Encode same frame, reused output | 315.6 | 304 | 4 |
| Decode same frame, owned result | 449.3 | 440 | 8 |
| Encode data_sm 5.0, 65,535-byte payload, new buffer | 14,093 | 147,568 | 5 |
| Encode same frame, reused output | 9,179 | 73,840 | 4 |
| Decode same frame, owned result | 6,817 | 73,960 | 9 |
| Copy 65,582 bytes, reused destination | 1,098 | 0 | 0 |
| Prepare UDH16 text, five segments | 12,028 | 7,608 | 13 |

**B/op measures allocated heap bytes; allocs/op counts heap allocations. Neither
measures copied bytes or retained memory.** Reusing output removes one allocation
in these cases; encoding still builds and copies a private frame. Decode clones
the body for ownership. Copy baselines measure only `copy` on hot buffers.
These timings are not SMSC throughput guarantees or controlled speedup claims.

### Additional performance workloads

The **80-case suite** also covers stream reads, TLVs, malformed frames, text
codecs, message planning, receiver reassembly/duplicates, and Session round trips.
Session benchmarks include both `net.Pipe` endpoints, not TCP/TLS or a real SMSC.

```sh
go test . ./codec ./message ./textcodec -run='^$' -bench=. \
  -benchmem -benchtime=200ms -count=3 -cpu=1
```

[All results and timing caveats](docs/benchmarks/r3-summary.md) ·
[Raw output](docs/benchmarks/r3-go1.26.4.txt) ·
[Methodology, historical tables and allocation profiling](docs/benchmarks/README.md)

### Optional logging (v1.1.0)

Local Go 1.26.10 results on 2026-10-29, linux/amd64 WSL2, Ryzen 7 6800H:
median of three 200 ms runs, `-cpu=1`. Disabled diagnostics allocate no record;
the Session numbers include the existing protocol/runtime allocations.

| Workload | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Default nil — isolated event | 3.337 | 0 | 0 |
| JSON — isolated event | 1741 | 240 | 1 |
| Text — isolated event | 1624 | 240 | 1 |
| Default nil — Session round trip | 7343 | 1664 | 30 |
| JSON — Session round trip | 8878 | 1904 | 31 |
| Text — Session round trip | 9158 | 1904 | 31 |

JSON/text use `io.Discard`: formatting is included, file/terminal I/O is not.
Session measurements use a manual enquiry/automatic response over `net.Pipe`,
excluding startup and shutdown. These are local observations, not throughput
promises. [All cases, commands and raw output](docs/benchmarks/README.md#optional-logging-v110).

### UDH language protection (v1.1.0)

Five-byte inputs, complete bounded `Admit` + `Receive` path; Go 1.26.10,
three 200 ms runs, `-cpu=1`, same local machine as above:

| Workload | Median ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Language declaration, decoding blocked | 1048 | 520 | 8 |
| Language declaration, raw-only reception | 1018 | 520 | 8 |

These include ownership copies and message ID creation. They measure allocation,
not copy counts or isolated guard overhead. [Controls, commands and raw results](docs/benchmarks/README.md#udh-language-protection-v110).

### Raw framing (v1.1.0)

Go 1.26.10 on the same local machine, median of three 200 ms runs, `-cpu=1`:

| Workload | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| ReadFrame, owned 4 KiB frame | 409.0 | 4100 | 2 |
| WriteFrame, reused 4 KiB bytes.Buffer | 34.33 | 0 | 0 |
| Decode, complete malformed body | 338.2 | 360 | 8 |

Read includes acquisition copies; the buffer write includes its payload copy.
Allocation counts do not measure copies. These exclude network I/O and do not
claim an improvement over an earlier release. [Full results and commands](docs/benchmarks/README.md#raw-framing-and-diagnostic-errors-v110).

### Active heartbeat paths (v1.1.0)

Go 1.26.10, same local machine, median of three 200 ms runs, `-cpu=1`:

| Workload | Median ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Explicit enquiry round trip | 7038 | 1696 | 30 |
| Automatic probe request path | 7070 | 1696 | 30 |
| Full-window skip | 514.8 | 272 | 4 |
| Construct/close: disabled, bound | 3513 | 4912 | 38 |
| Construct/close: enabled, before bind | 3675 | 5176 | 42 |
| Construct/close: enabled, bound | 4090 | 5300 | 43 |

The request benchmarks include both peers and net.Pipe work. Automatic probes
measure the shared request path without waiting for scheduled intervals. Lifecycle
rows include construction, Close/Wait and startup races; they are not isolated
timer costs. B/op is allocated memory, not copied bytes. See the
[method and raw output](docs/benchmarks/README.md#heartbeat-paths-v110).

### DLR helpers (v1.1.0)

Go 1.26.10, same local machine; median of three 200 ms runs, `-cpu=1`:

| Workload | Median ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Parse/FullText | 2529 | 784 | 7 |
| Parse/TLVOnly | 313 | 136 | 6 |
| Parse/Mixed | 2912 | 920 | 13 |
| Parse/Malformed | 2445 | 1640 | 17 |
| Build/TLVOnly | 790 | 608 | 12 |
| Build/Mixed | 1333 | 1496 | 20 |

Parsing includes owned evidence copies and diagnostics. Construction includes
standard Codec preflight and its temporary wire buffer. These are stateless
helper costs; B/op is not a copy counter. [All 11 cases, scope and raw output](docs/benchmarks/README.md#receipt-helpers-v110).

## Documentation

| Read about | Guide |
| --- | --- |
| API contracts, ownership and ACK policy | [Implementation contracts](docs/decisions.md) |
| v1 compatibility commitments | [Compatibility policy and API snapshot](docs/compatibility.md) |
| Command, role and TLV coverage | [Support matrix](docs/support.md) · [TLVs](docs/tlv-support.md) |
| Field validation and data_sm context | [Field semantics](docs/field-semantics.md) · [Direction](docs/data-direction.md) · [Transactions](docs/data-transaction.md) |
| Cancellation, shutdown and local resources | [Session lifecycle](docs/session-lifecycle.md) · [Local validation](docs/local-validation.md) |
| v1.1.0 scope, increments and implementation status | [Design](docs/v1.1.0-design.md) · [Roadmap](docs/roadmap/v1.1.0.md) |
| Raw frames, ownership and classified decode errors | [Framing](docs/framing.md) · [Runnable example](examples/framing/main.go) (v1.1.0) |
| DLR text/TLV evidence, construction and peer conventions | [Receipt helpers](docs/receipts.md) · [Compiling example](examples/receipts/main.go) (v1.1.0) |
| Active enquiries, window pressure and probe deadlines | [Keepalive](docs/keepalive.md) (v1.1.0) |
| Optional structured diagnostics and handler integration | [Logging](docs/logging.md) (v1.1.0) |
| Release changes and test evidence | [Changelog](CHANGELOG.md) · [Validation records](docs/validation.md) |

Run the core checks from the repository root:

```sh
go build ./...
go vet ./...
go test -race -count=1 -timeout=90s ./...
```

The current [CI configuration](.github/workflows/ci.yml) covers the Go 1.26/1.27
matrix and all 14 fuzz targets. Go 1.26 runs each target for **1,000,000
executions**; Go 1.27 retains a five-second budget per target. The count budget
avoids a Go 1.26 fuzz shutdown race without ignoring failures or retrying tests.
See the [diagnosis and CI policy](docs/v1.1.0-ci-fuzz.md).
Tests include independent protocol bytes and raw TCP peers, beyond shared-Codec
round trips. Historical records distinguish local results from remote CI.

## AI authorship and review status

**This library was written by AI agents using OpenAI Codex / OpenAI, under human
maintainer direction.** AI generated and revised the implementation, tests,
benchmarks, examples and substantial documentation. The library has no AI runtime
dependency.

The maintainer confirms the model used as **GPT-6.0 Astra**. The repository-level
[AI_DISCLOSURE.md](AI_DISCLOSURE.md) and every Go file's `SPDX-AI-*` header record
the model, provider, scope and provenance classification.

The maintainer supplied requirements, constrained scope and directed corrections.
This does **not** claim an independent line-by-line human audit. Agent review and
AI-authored tests can share mistakes; independent wire fixtures are independent
of the serializer, not necessarily independently human-authored. No exact AI
percentage or complete per-change model-version history is claimed.

Maintainers remain responsible for accepted changes and releases. Contributors
should disclose the tools and scope of material AI-generated work, then review
and test it. The Apache-2.0 license is unchanged by this disclosure.

We adopt the community [AI Disclosure Convention](https://github.com/ggfevans/ai-disclosure)
and draw on [GitHub's AI code review guidance](https://docs.github.com/en/copilot/tutorials/review-ai-generated-code),
checked on 2026-10-28. The convention is a voluntary draft, not a GitHub certification.

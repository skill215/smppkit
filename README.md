# smppkit

**SMPP for Go — clients, servers, and standalone protocol tools.**

Send text, segment long messages, and reassemble incoming SMS with explicit
outcomes and bounded resources. One shared Session and versioned Codec support
**SMPP 3.3, 3.4, and 5.0**.

**Go 1.26+** · [v1.0.0](https://github.com/skill215/smppkit/releases/tag/v1.0.0) ·
[Apache-2.0](LICENSE) · No third-party runtime module dependencies

> **AI-written:** OpenAI Codex generated this library under maintainer direction.
> Model: **GPT-6.0 Astra**. See [AI_DISCLOSURE.md](AI_DISCLOSURE.md).

[Quick start](#quick-start) · [Go API](#go-api) · [Protocol support](#protocol-support) ·
[Examples](#examples) · [Benchmarks](#benchmarks) · [Documentation](#documentation)

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
go get github.com/skill215/smppkit@v1.0.0
```

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

## Documentation

| Read about | Guide |
| --- | --- |
| API contracts, ownership and ACK policy | [Implementation contracts](docs/decisions.md) |
| v1 compatibility commitments | [Compatibility policy and API snapshot](docs/compatibility.md) |
| Command, role and TLV coverage | [Support matrix](docs/support.md) · [TLVs](docs/tlv-support.md) |
| Field validation and data_sm context | [Field semantics](docs/field-semantics.md) · [Direction](docs/data-direction.md) · [Transactions](docs/data-transaction.md) |
| Cancellation, shutdown and local resources | [Session lifecycle](docs/session-lifecycle.md) · [Local validation](docs/local-validation.md) |
| Release changes and test evidence | [Changelog](CHANGELOG.md) · [Validation records](docs/validation.md) |

Run the core checks from the repository root:

```sh
go build ./...
go vet ./...
go test -race -count=1 -timeout=90s ./...
```

[CI](.github/workflows/ci.yml) runs the Go 1.26/1.27 matrix and all 11 fuzz targets.
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

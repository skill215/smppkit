# Runnable SMPP examples

Requires Go 1.26 or newer. Run commands from the repository root. The client and
server are separate programs using the public smppkit API; no external SMSC,
database or AI service is needed. Their default transport is plain loopback TCP.

## Submit and receive complete messages

Terminal 1:

```sh
export SMPP_PASSWORD=demo123
go run ./examples/server -address 127.0.0.1:2775
```

Terminal 2:

```sh
export SMPP_PASSWORD=demo123
go run ./examples/client -address 127.0.0.1:2775 -version 5.0 \
  -from example -to 12345 -text 'Hello from smppkit'
```

`demo123` is a synthetic local-demo password. The server accepts system ID `demo`
by default; set `-system-id` on both programs to change it. Passwords are supplied
through the environment and must fit the protocol's eight-octet ASCII limit.

The client prints every segment's outcome, sequence, written-byte count, peer
message ID and error, then performs a bounded unbind. Errors exit nonzero after
cleanup. The server acknowledges each admitted segment and consumes reassembled
messages separately. Its limits include 32 simultaneous connections and the
Receiver's bounded in-memory defaults. Ctrl-C closes the listener/connections
and joins handlers; expiry events are drained during idle periods as well.

## Bidirectional text and automatic segmentation

Restart terminal 1 with echo enabled:

```sh
go run ./examples/server -echo
```

In terminal 2, send a message that exceeds a single GSM segment:

```sh
go run ./examples/client -version 5.0 -wait-delivery \
  -text "$(printf '%180s' '' | tr ' ' A)"
```

The server reassembles the submission and sends its text back using deliver_sm.
The client reads and ACKs concurrently with submission, reassembles the delivery,
and waits for its final ACK write before unbinding. This echo is another message,
not a delivery receipt or proof of handset delivery. `-wait-delivery` waits for
one complete incoming message; it is a demo, not a receipt tracker. The demo
prints incoming text, so use synthetic messages.

The policy is explicit: unpacked GSM7 with strict UCS-2 fallback, 140-octet radio
budget and UDH16 concatenation. For example, `-text '你好，SMPP'` selects UCS-2.
No request is automatically retried after a rejection or uncertain outcome.

## Version and error paths

In the v1.1.0 development checkout, add `-diagnostics` to either Client or Server
to enable library debug diagnostics on stderr. Without it, library logs are
discarded; the examples' application logs still print their normal output.
The [logging contract](../docs/logging.md) explains event fields and handler
requirements. This option does not enable periodic enquire_link.

### Active enquire_link (v1.1.0, unreleased)

Both examples accept `-enquire-link-interval` (default `0`, disabled) and
`-enquire-link-timeout` (default `0`, Session RequestTimeout). Both endpoints
already answer incoming enquiries automatically, even with scheduling disabled.
To observe client probes after sending, use two terminals:

```sh
SMPP_PASSWORD=demo go run ./examples/server -diagnostics
SMPP_PASSWORD=demo go run ./examples/client -diagnostics \
  -enquire-link-interval 1s -enquire-link-timeout 3s -idle 5s -timeout 20s
```

To initiate on the server instead, put the two enquiry flags on the server
command and keep `-idle 5s` on the client. Both may initiate simultaneously.
The client idle period remains inside its overall timeout and ends early if
Session closes. The same scheduling works with `-version 3.3` transmitter bind.
The default two-minute ReadTimeout remains independent: choose a shorter
interval plus probe timeout for proactive detection. Full request windows skip
probes; failed sent probes retire the connection without retry or reconnect.
See [keepalive](../docs/keepalive.md) for shutdown and failure details.


Run a standalone standard-logger bridge without a network connection:

```sh
go run ./examples/logging
```

It uses a canceled Dial context to emit a setup diagnostic through an
application-owned TextHandler. It does not change global logger settings.

| Client option | Bind and supported demo flow |
| --- | --- |
| `-version 3.3` | Transmitter bind, single-part submit; standard multipart and `-wait-delivery` are rejected. Use the server without `-echo`. |
| `-version 3.4` | Transceiver bind, submit and optional deliver/echo, including multipart. |
| `-version 5.0` | Default; transceiver bind, submit and optional deliver/echo, including multipart. |

Echo mode accepts only transceiver binds because it sends both directions on the
same connection. It does not implement separate TX/RX routing. A non-echo server
accepts the supported roles in all three profiles. All wire versions remain
explicit; there is no negotiated downgrade.

The server's memory ACK path uses a non-transaction default for mode 0.
Explicit transaction submissions (mode 2) cause RunReceiver to return ErrState
and close without admission or ACK. Applications implementing transaction mode
must use Receive/Respond and supply a real outcome; enabling echo does not
implement that mode. MC-to-ESME receive/echo acknowledgements are unaffected.

```sh
# Against the non-echo server: a shorter operation deadline.
go run ./examples/client -version 3.4 -timeout 5s -text 'short test'

# Expected nonzero exit: rejected authentication; no automatic retry.
SMPP_PASSWORD=wrong go run ./examples/client -text 'rejected test'

# Expected nonzero exit: server accepts submission but sends no delivery.
go run ./examples/client -wait-delivery -timeout 1s -text 'timeout test'
```

The overall client deadline covers bind/send/receive. A successful exchange uses
a separate three-second unbind deadline. Close/Wait and receiver-worker joins
also run on error paths. Server `-address 127.0.0.1:0` chooses a free local port
and prints it, which is useful for automated smoke checks.

The client sweeps incomplete groups and drains expiry events between receives,
both during traffic and after idle polls. ACK writes and consumer work can delay
that sweep; the operation and transport deadlines still apply. The R2 regression
in `client/main_test.go` covers ready traffic arriving alongside an expired group.

## Other examples and peer simulator

The root [README](../README.md) links standalone codec, multipart planning,
customer TLV, private command, data_sm, notification and submit_multi examples.
The [legacy simulator harness](../interop/legacy/README.md) exercises the
[go-smpp smpptest server](https://github.com/skill215/go-smpp/tree/743fa92c92a2e87fc64d53c677f774a856c1b6e3/smpp/smpptest)
in a separate temporary module. Its recorded scope is 3.4 UDH, not all features
demonstrated by these examples. See [local validation](../docs/local-validation.md).


## Raw framing and explicit decode policy (v1.1.0, unreleased)

```sh
go run ./examples/framing
```

This standalone example uses synthetic bytes, no sockets. It retains a complete
invalid submit_sm frame, inspects structured decoding diagnostics, and explicitly
chooses to continue to the next valid frame. It is not an automatic bad-frame
recovery or proxy policy. See the [framing contract](../docs/framing.md).


## Delivery receipts (v1.1.0, unreleased)

`examples/receipts` runs a Client and Server over net.Pipe. The server constructs
an explicitly supplied synthetic EXPIRED report; it does not derive delivery
from a submit response. The client parses separate text/TLV evidence and chooses
an explicit ACK only after inspecting diagnostics, then unbinds. No sockets,
external peer, credentials configuration, persistence or delivery tracker needed.

```sh
go run ./examples/receipts -version 3.3
go run ./examples/receipts -version 3.4
go run ./examples/receipts -version 5.0
go run ./examples/receipts -version 3.4 -data-sm
go run ./examples/receipts -version 5.0 -data-sm
```

3.3 uses the documented legacy text convention; data_sm is rejected. 3.4/5.0
use text plus standard ID/state/network-error TLVs. Text err and network error
intentionally differ to show their separate namespaces. The input outcome and
addresses are application facts. See [receipt contracts](../docs/receipts.md)
and [source](receipts/main.go) for version, grammar, ownership and ACK boundaries.

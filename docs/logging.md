# Optional structured logging (v1.1.0, unreleased)

`smpp.Config.Logger` accepts a standard-library `*slog.Logger`. It is shared by
`NewSession`, `ClientConfig.Session`, and `ServerConfig.Session`; Client/Server
setup failures use the same configured logger. Go 1.26 remains the minimum.
Standalone Codec has no logger dependency or new configuration.

```go
logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
    Level: slog.LevelDebug,
})).With("service", "sms-example")

sessionConfig := smpp.Config{Logger: logger}
// Pass sessionConfig as ClientConfig.Session, ServerConfig.Session, or the
// Config argument of NewSession. See examples/client and examples/server.
```

## Defaults and ownership

A nil Logger uses `slog.DiscardHandler`. The package never consults or changes
`slog.Default`, writes directly to stderr, or configures a global logger.
Applications own handler configuration, output destinations, flushing, and
closing. No third-party logger or custom public logging interface is required.
Setting a logger does not enable scheduled `enquire_link`; that is a separate,
unimplemented feature in [issue #2](https://github.com/skill215/smppkit/issues/2).

Omitting Logger preserves the existing protocol behavior. Use keyed Config
literals, as required by the [v1 compatibility policy](compatibility.md).
The logger pointer is captured at construction; a concurrency-safe handler can
use `slog.LevelVar` for dynamic filtering. Diagnostic messages, event names, and
field inventories are not a stable machine-consumption API.

## Events and safe fields

| Event | Level | Meaning |
| --- | --- | --- |
| `session_started` | Debug | Transport Session started; `state` distinguishes Connected from server-admitted Bound. This alone is not proof of client bind success. |
| `request_completed` | Debug on success/local cancellation; Warn on other errors | One low-level Request returned, including Bind, manual enquiry, Unbind and each submitted multipart segment. |
| `session_closed` | Debug on explicit/graceful close or local cancellation; Warn otherwise | One terminal record, emitted by the existing expiry loop. EOF without a completed unbind is an abnormal termination. |
| `setup_failed` | Debug on cancellation; Warn otherwise | Client connect failure, Server Accept/Outbind handshake failure, or an unexpected Serve listener failure. Includes a fixed `stage`. |

Session records include `component=smppkit`, `endpoint`, an opaque generated
`session_id`, and the numeric SMPP version byte (51/52/80 for 3.3/3.4/5.0).
Request records add numeric `command_id`, `sequence`, `outcome`, `bytes_written`,
`status`, and a fixed `reason` category. Status is meaningful only with
`Outcome == ResponseReceived`. Command ID is zero if local validation or
admission failed before identifying a valid command. Successful client bind is
a successful bind `request_completed`, not merely `session_started`.

Categories include `invalid_pdu`, `unsupported`, `unexpected_response`,
`invalid_state`, `overloaded`, `timeout`, `inbound_timeout`, `peer_rejected`,
`eof`, `truncated_frame`, `transport_closed`, `canceled`, `closed`, `ok`, and
`other`. Unknown errors retain their original identity in the returned error or
`Session.Err`; their message is never copied into diagnostics. Server stages
distinguish authentication failures even when an application error is `other`.
Initial configuration errors are returned directly rather than logged.

Library records exclude bind credentials/identities, addresses, message IDs,
message contents, raw frames, arbitrary TLV values, and error text. Custom
codecs, transports, and authentication callbacks can embed secrets in errors,
so logging `err.Error()` is deliberately avoided. Application-added attributes
and application-owned handlers remain the application's responsibility.

There is no per-inbound-PDU dump or complete traffic/audit log. Receive, Notify,
Respond, and message reassembly outcomes retain their existing return-value
APIs. Returned errors, Result and Session.Err remain authoritative; logs do not
change retry policy or establish final delivery. One connection failure can
produce both a terminal event and failed outstanding Request events. Concurrent
events have no guaranteed ordering beyond their own call's execution.

## Handler execution and shutdown

Handlers, including `Enabled`, run synchronously outside Session and write-job
locks. They must support concurrent calls and return promptly. Request logging
runs after its protocol work and window release; handler latency can therefore
extend the observed call duration beyond its request deadline. Server handshake
failure logging also runs in the caller/Serve worker, after transport cleanup.
Session startup logs run before runtime goroutines start.

Request and setup diagnostics receive the caller's context, including its
values; internal timeout cleanup does not cancel that context. Runtime startup
and terminal records use a background context and the Session's fixed metadata.
Automatic probe records also use a background context; they run in the optional
heartbeat scheduler after releasing request-window capacity.

The expiry loop emits the terminal record without holding Session.mu. `Close`
does not call a handler and can still abort while a handler is blocked. `Wait`
joins runtime logging, reader/writer/expiry and the optional heartbeat scheduler, but
does not join application goroutines still returning from Request. Join those
callers before flushing a handler if complete log output matters.

A handler must not wait for `Wait`, protocol traffic, or another operation on
the emitting Session. Reading State/Err is safe; this does not make arbitrary
reentrant operations safe. Handler errors are ignored by slog and do not alter
protocol outcomes. Panics are not recovered. Logging introduces no asynchronous
logger, additional logging goroutine, or unbounded log queue. Applications needing slow
sinks must supply their own bounded output policy.

## Integration and verification

- `go run ./examples/logging` demonstrates a standard logger bridge through
  TextHandler, without a socket, private interface, or third-party dependency.
  It preserves formatted fields as text, not backend-native structured fields.
  It deliberately avoids global logger redirection to prevent output recursion.
- Add `-diagnostics` to the [Client/Server examples](../examples/README.md) to
  emit debug diagnostics. Their existing application logs are separate and can
  include synthetic message contents; the library's privacy contract does not
  redact application logs.
- `logging_test.go` covers default silence, levels, safe metadata, independent
  wire peers for all versions, Serve authentication failure, overload, malformed
  input, inbound timeout, request context, concurrent calls, handler errors and
  shutdown without holding locks.
- `logging_benchmark_test.go` measures disabled, filtered, enabled sink, JSON,
  and text paths both in isolation and through a net.Pipe Session exchange.
  See the [benchmark guide](benchmarks/README.md) for measured results and limits.

The [local validation record](v1.1.0-logging-validation.md) distinguishes executed
checks from planned work and published releases.

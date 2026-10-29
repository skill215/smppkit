# Optional enquire_link scheduling

Available in **v1.1.0**, on Go 1.26. The same Session
runtime supports SMPP 3.3, 3.4 and 5.0, on both Client and Server connections.

```go
cfg := smpp.Config{
    EnquireLinkInterval: 30 * time.Second,
    EnquireLinkTimeout:  10 * time.Second,
}
// Pass cfg as ClientConfig.Session, ServerConfig.Session, or to NewSession.
```

## Defaults and lifecycle

| Setting | Default | Meaning |
| --- | --- | --- |
| EnquireLinkInterval | 0: disabled | Delay after successful bind and after each completed or skipped automatic probe |
| EnquireLinkTimeout | RequestTimeout (normally 30s) | Bounds one automatic exchange from admission through queueing, write and response |
| RequestTimeout | 30s | Unchanged for explicit Request/Notify calls |
| ReadTimeout | 2m | Independent whole-frame read deadline; heartbeat does not override it |
| WriteTimeout | 10s | Independent complete-frame write deadline |

Negative durations are rejected. A timeout alone does not enable scheduling.
With the default zero interval, no heartbeat channels, timer or goroutine are
created. Incoming enquiries are still automatically answered while bound,
without an application Receive loop; this behavior predates v1.1.0.

Scheduling starts only after successful bind. A failed bind does not start it;
a subsequent successful retry does. Server sessions start after their successful
bind response is written. Ordinary traffic does not reset the interval. Only one
automatic probe can be in flight; there is no accumulated tick backlog. Explicit
Request(EnquireLink) calls remain independent and share the same request window.

Both local unbind admission and received unbind stop scheduling. A queued probe
is cancelled before writing; an active frame must finish before the writer can
send an unbind ACK. After unbinding begins, the probe's timer cannot retire the
connection: the existing unbind, read and write limits govern shutdown. Close
interrupts everything immediately. Wait joins the optional scheduler as well as
the reader, writer and inbound-expiry loop.

An incoming enquiry may already be in flight when local unbind starts. While
Unbinding, valid incoming enquiries are consumed and ignored without queuing a
response. They do not restart scheduling, reopen business admission or extend
the unbind request/write deadlines. Bound sessions still answer automatically.
Crossed unbind requests retain their existing ACK and completion obligations.

## Capacity, responses and failure

Admission atomically tries the existing request window. A full window skips the
probe without allocating a sequence or waiting for a slot. A queued probe whose
timeout expires before its write starts is also skipped. Neither proves peer
failure. The next attempt waits a full interval. Probes consume ordinary window
capacity; there is no reserved heartbeat slot or priority over business requests.
Continuous full-window pressure can therefore prevent probes indefinitely.

The probe uses the normal versioned Codec, sequence allocator, response map and
serialized writer. Only a matching, successful enquire_link_resp completes it.
Unsolicited, duplicate and late sequences are ignored. An opposite-direction
request with the same number uses the independent inbound sequence space.

| Event while bound | Session.Err / behavior |
| --- | --- |
| Probe deadline with an incomplete exchange after writing started, including a stalled write join | ErrEnquireLinkTimeout |
| Correlated non-OK response or Generic NACK rejection | *PeerError with enquiry command/status |
| Correlated incompatible response opcode | ErrResponse |
| Malformed response / transport or read/write deadline failure | Existing Codec/transport cause |
| Explicit Close | ErrClosed |

First terminal cause wins. Heartbeat failure retirement is serialized with
unbind admission, so a failure cannot replace an already accepted unbind.
ErrEnquireLinkTimeout is a distinct sentinel checked with errors.Is; it does not
wrap context.DeadlineExceeded. It means the automatic exchange exceeded its
budget, not proof the peer received zero or all bytes. Even a correlated reply
cannot keep a stalled local Write alive beyond the probe deadline.
A correlated response and an already finished write complete the exchange even
when the requester also observes its expired deadline. Scheduling delay after
that completion does not itself retire the connection.

The library closes that connection generation; it does not reconnect or replay
requests. Concurrent business calls keep their existing Result byte counts and
Written/PartlyWritten/ResponseReceived outcomes. Submission acceptance is not
final delivery.

Choose an interval plus probe timeout comfortably below ReadTimeout if you want
the probe policy to act first during idle periods. A shorter ReadTimeout can
close before the first probe or its response; incoming traffic can postpone the
next read deadline, but never a pending probe's own deadline. A shorter
WriteTimeout can terminate a blocked write first. No automatic adjustment is made.

## Diagnostics and examples

With an optional slog.Logger, successful probes use request_completed metadata;
local admission/queue pressure emits a Debug enquire_link_skipped event. A probe
deadline is a terminal session_closed event with reason enquire_link_timeout.
Other failures retain their existing safe categories. No PDU contents are logged.

The [client/server examples](../examples/README.md#active-enquire_link-v110)
provide flags for scheduling and an explicit client idle period. Neither logging
nor automatic responses enable initiation by themselves. See the
[validation record](v1.1.0-keepalive-validation.md) for independent byte fixtures,
virtual-time lifecycle tests and measured benchmark scope.

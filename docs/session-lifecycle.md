# Session lifecycle and independent peer checks

The shared Session remains one connection generation, with one reader, one
serialized writer and one inbound-expiry loop. All three protocol profiles use
the same lifecycle code. The unreleased v1.1.0 adds optional
[slog diagnostics](logging.md) without adding logging goroutines, and an optional
[heartbeat scheduler](keepalive.md) that starts probing after bind.

## Closing a connection

`Close` aborts immediately. `Shutdown(ctx)` attempts unbind and closes even if
the exchange fails. Neither method drains outstanding business requests or
application handlers. Finish that work before calling Shutdown when it matters.
`Done` signals terminal shutdown; call `Wait` to join Session's runtime
goroutines, including the optional scheduler.
Wait does not join application goroutines making Request/Notify/Respond calls.

With an explicit logger, the expiry loop emits the terminal diagnostic outside
Session locks. Close never calls a handler; Wait joins terminal logging. Request
diagnostics run in the calling goroutine after releasing window capacity;
automatic-probe diagnostics run in the scheduler after its request completes. Slow
handlers can delay Wait or Request return, so handlers must return promptly.

An admitted `Request(ctx, &pdu.Unbind{})` also retires the connection on
completion or failure. Local encoding/admission failures before unbind is
accepted do not by themselves retire it. Once Unbinding, new business requests
are rejected. An inbound unbind is automatically acknowledged on the shared
writer. Duplicate peer unbind requests during this exchange are rejected.
Incoming enquiries that cross unbind are consumed without replying while
Unbinding, so a pre-existing heartbeat cannot abort or block the final exchange.
This does not admit new business requests or restart the heartbeat scheduler.

When both peers initiate unbind, there are two obligations: receive the
correlated response to our request, and fully write our response to the peer's
request. Either may finish first; both must finish before successful shutdown.
If a response arrives but the peer does not consume its ACK, Request can return
`ResponseReceived` **and an error** when its deadline expires. The result records
what happened to our request, not successful completion of the whole handshake.
If our response is missing, fully written unbind remains `Written` and uncertain.
RequestTimeout, the caller's context and WriteTimeout bound these paths.

There is a final transport race: a peer can read the last ACK byte and close
before the local Write call or its completion hook returns. When all required
incoming unbind information is already present, EOF/closed-connection errors
leave the final decision to the queued/active ACK writer. Its successful hook
closes normally; write failure closes with that error. Truncated frames,
timeouts, malformed frames and EOF before our required response still fail.
An externally requested Close can always abort this exchange.

## Ownership after retirement

Closing clears pending and inbound maps and discards unread application requests
and notifications. They cannot be drained after Close. Already returned PDUs and
handles remain caller-owned; an old handle cannot respond on a retired or new
connection. Reconnecting requires a new Session. No request is automatically
retried, and successful submission does not establish final delivery.

Completed or cancelled write jobs release encoded buffers, captured contexts and
completion closures. The writer cancels/drains queued jobs before exiting.
Join application calls as well as Session.Wait before checking released admission
slots: the calling goroutines own those slots until their methods return.
This is bounded lifecycle cleanup, not a guarantee that application-held PDUs,
multipart receiver stores or explicit reference leases are released by Close.

Receive/ReceiveNotification check terminal state before handing out input.
An already-cancelled receive context does not consume queued input. Cancellation
concurrent with a ready input may race with successful receipt; callers must
handle a successful return even when their context has just been cancelled.

## Evidence and limits

`session_lifecycle_test.go` covers both crossed-unbind completion orders,
simultaneous Shutdown, terminal direct Request(Unbind), missing response/blocked
ACK, fragmented header/body reads, partial writes, one deadline per whole frame,
unread-input cleanup, completed/cancelled job captures and stale response handles.
A deterministic completion-state test exercises reader EOF before the writer
hook, alongside the full transport concurrency tests.

Forty bound connection cycles alternate graceful and aborted work, explicitly
join calls and loops, and assert empty maps/queues/admission slots. The test also
compares Session-owned goroutine stacks before and after. It does not assert
global process goroutine counts, RSS/heap baselines or long-running soak capacity.

`session_interoperability_test.go` uses a local TCP MC whose headers and bodies
are authored independently of Codec. Each of 3.3, 3.4 and 5.0 checks exact client
bind/submit/query/enquire/unbind bytes. The peer fragments its bind response,
coalesces a late query response, a duplicate submit response, a live response and
an opposite-direction request using the same sequence, then crosses unbind.
3.3 has no bind TLV; 3.4/5.0 explicitly advertise their interface version.
The request after a timed-out query is inspected to rule out an automatic retry.
See the [wire examples](wire-samples.md) and [executed checks](validation.md).

This is an independent local byte peer, not an external SMSC implementation or
full SMPP certification. Durable ACK/recovery belongs to the application.
Receipt correlation, external carrier interoperability, 3.3 multipart
compatibility and 5.0 broadcast remain outside this increment. Earlier Codec benchmark results are unchanged; this
work makes no new throughput or allocation-rate claim.

The [R1 correction of RV18](review-gaps.md) makes Request's write join observe
cancellation and RequestTimeout even after correlation. An active write is
retired and joined before reading its final byte count. The observed response
still wins the request outcome: OK returns nil, rejection returns PeerError,
and Result retains ResponseReceived, the response and byte count. This does not
promise the Session is still usable: cancellation may have closed it. Unbind
additionally checks completion of our ACK to a concurrent peer Unbind and returns an error
when that obligation was interrupted. A write already completed at cancellation
does not cause the transport to be retired. Virtual-time regressions cover all
these orderings without scheduler-dependent sleeps.

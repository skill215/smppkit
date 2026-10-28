# One-way notifications and outbind

Implementation preview, 2026-10-27. SMPP 3.4/5.0 support alert_notification and
outbind; SMPP 3.3 supports neither. Both have concrete Codec bodies and no
response command. This increment adds endpoint flows using the existing
versioned Codec, shared Session and server admission path.

Sources: [SMPP 3.4](https://smpp.org/SMPP_v3_4_Issue1_2.pdf) §§2.2.1, 2.3,
4.1.7, 4.12 and [SMPP 5.0](https://smpp.org/SMPP_v5.pdf) §§2.3.7, 2.5.5,
4.1.1.7, 4.1.3. The [runnable example](../examples/notifications/main.go)
demonstrates both operations without external services or customer traffic.

## Bound-session alerts

The MC sends an availability notification with:

```go
result, err := serverSession.Notify(ctx, &pdu.AlertNotification{
    Source: pdu.Address{TON: 1, NPI: 1, Value: "12345"},
    ESME: pdu.Address{TON: 5, Value: "example"},
    Optional: tlv.Fields{{Tag: tlv.MSAvailabilityStatus, Value: []byte{0}}},
})
```

Notify currently accepts only alert_notification, from an MC on an RX or TRX
session. It rejects TX, wrong direction, unbound/unbinding states, unsupported
versions and other bodies before allocating a sequence. Normal Codec validation
and the negotiated TLV policy apply. Request does not send these commands.

On the ESME, independently of the request/message consumer:

```go
packet, err := session.ReceiveNotification(ctx)
```

Use one ReceiveNotification consumer and one Receive/RunReceiver consumer per
Session. The notification is an owned `pdu.PDU`, with no response handle. Do not
ACK it or pass it to AdmitMessage. Reading a notice does not change bind state.
Duplicate alert sequence values are delivered as separate events rather than
silently deduplicated; the library does not infer subscriber/business identity.

Notify returns the existing Result structure with NotWritten, PartlyWritten or
Written. Success means the frame was fully written, not that the peer consumed
it. It never returns ResponseReceived and never creates a pending response entry.
A later Generic NACK for that sequence is unmatched and follows the existing
ignore policy; Notify does not wait for it or retry. An alert is an availability
signal, not final SMS delivery or an instruction to automatically resubmit.

## Bounds, cancellation and scheduling

`Config.NotificationLimit` defaults to 32 (allowed range 1..65536). It independently
bounds queued inbound notifications, concurrent admitted Notify calls and the
outbound notification queue. The existing Codec frame cap bounds each encoded
frame. No additional worker, expiry ticker or unbounded history is created.

Outbound notices share the sole writer and monotonically increasing sequence
space with requests, but have their own admission slots and write queue. An
occupied request window cannot block notification admission. The writer permits
up to eight consecutive responses before taking a queued application frame;
requests and notifications alternate when both queues are busy. This prevents
either application queue from monopolizing the writer, without treating a
notice as a response.

RequestTimeout also bounds Notify's admission and write wait; WriteTimeout still
bounds each active frame. Queued cancellation prevents writing, leaves the
session usable and may have already consumed a sequence. Cancelled queue entries
remain until the writer drains them; a full physical queue returns ErrOverloaded
before allocating another sequence. Cancellation during an active write closes
the transport and joins the writer before reporting bytes. A completed successful
write wins a concurrent cancellation. No uncertain frame is automatically retried.

ReceiveNotification cancellation does not close the session; an already-cancelled
wait does not consume queued notices. Notifications have no inbound ACK deadline
and never enter the inbound request map. If their queue fills, the reader closes
the session with ErrOverloaded instead of blocking response dispatch or silently
dropping a notice. After closure, queued notices are not a durable/drainable store.

## MC-initiated outbind

Outbind occurs before a Session is bound. `Server.Outbind` owns the supplied
connection, writes outbind, then reuses Accept's bind decoding, authentication,
response and session construction:

```go
serverSession, err := server.Outbind(ctx, conn, pdu.V50,
    &pdu.Outbind{SystemID: mcID, Password: mcPassword})
```

The chosen version must be a configured 3.4/5.0 profile. Outbind carries no
interface_version, so the ESME needs matching configuration. A single BindTimeout
covers the outbind write, incoming bind, authentication and bind response. Any
failure closes the owned transport; outbind might already have reached the peer.
There is no automatic reconnect or retry. Direct callers bound handshake
concurrency themselves, as with direct Accept; Serve remains the ordinary
ESME-initiated listener API.

The ESME accepts the MC connection, calls NewSession with its configured Codec,
and obtains outbind through ReceiveNotification while still Connected. The
application validates the MC credentials, then explicitly calls Bind with a
BindReceiver containing its own ESME credentials. Rejecting credentials means
closing the connection. The library neither authenticates the MC automatically
nor echoes its credentials into the bind. Avoid logging the owned outbind body.

This increment supports the **receiver outbind flow** described by the protocol
examples. TX/TRX follow-up binds, repeated outbind, outbind after binding and
version mismatches are rejected. RX is the supported library scope here, not a
claim that every other 5.0 deployment flow is forbidden by the protocol. An
ESME-initiated ordinary bind still supports TX/RX/TRX as before.

The MC bootstrap uses sequence 1; its resulting Session starts subsequent
outbound traffic at sequence 2. The ESME bind has its own sequence space and is
not an outbind response. Bootstrap writes finish before Session's sole writer
starts. A late NACK for outbind cannot match a new request reusing sequence 1.

Transport dialing/listening and TLS roles remain application choices. An MC
that dials an ESME remains the SMPP MC even when it is a TCP/TLS client. Use
Server.Outbind with an already secured connection; no reverse-dial manager or
credential database is added.

## Evidence and remaining scope

Independent full alert/outbind bytes and truncation/ownership tests are in
`codec/notification_test.go`. `session_notification_test.go` checks roles,
windows, no ACK, queue overflow, cancellation, partial writes, sequence retirement
and wire-observed writer fairness. `server_outbind_test.go` checks public flows,
an independent byte-oriented ESME, profile/role rejection and handshake failures.
See [wire samples](wire-samples.md) and [validation](validation.md).

External SMSC interoperability and reverse-connection TLS integration are not
established by these local tests. Durable event replay and subscriber DPF state
belong to the application, outside the library scope.
Private one-way command registration is not added; private request/response
extensions keep their existing contract.

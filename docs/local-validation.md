# Local resource and legacy-peer validation

The [R3 candidate run](r3-validation.md#resource-and-legacy-peer-checks),
2026-10-28, extends this same workload to ten minutes under race detection.
It completes 27,944 timed batches / 111,776 TCP connection pairs, with zero
Session loops and seven FDs at every post-join sample. The earlier one-minute
results below remain historical. R3 also reruns the same limited legacy harness
and verifies reference-repository status and file hashes are unchanged.

This increment extends M2/M3 local evidence without changing the public API.
Persistence and application crash recovery are outside the library scope.
It uses loopback connections only.
External service interoperability is deferred by request.

## Reproducible resource workload

```sh
SMPPKIT_SOAK_DURATION=60s go test -race . -run '^TestSoakLocalResources$' -count=1 -timeout=90s -v
go test -race . -run '^TestReceiverFloodResourceRecovery$' -count=1
```

The timed test is skipped unless SMPPKIT_SOAK_DURATION is set, accepts 1s..30m,
and checks the duration between complete batches. Allow extra timeout for warmup,
the final batch, GC and race-detector shutdown. The deterministic receiver flood
check also runs in the ordinary root test suite.

Each batch establishes four concurrent local TCP connections with real 5.0 TRX
binds. Each connection runs 32 submit_sm and 32 deliver_sm requests through a
window of four, four application senders per direction, deliberately slow
application consumers, eight notifications and sixteen enquire_link exchanges.
The bind role, correlation, payload, response identity and cleanup are checked.
Notifications have no peer ACK: capacity eight holds the complete burst even
when their consumer is temporarily descheduled. The first short probe used
capacity four and correctly triggered overload; this was a workload-budget
error, not a protocol fix or an ignored failing library result.

All application calls and Session loops are joined after graceful unbind. Every
cycle checks empty correlation maps, inbound/outbound queues and admission slots.
The harness does not register per-cycle test cleanups that would retain every
retired Session until the end of the run.

Every batch also exercises a bounded receiver independently of networking:

- Saturate sixteen incomplete groups, then repeatedly reject new groups while
  admitting identical duplicates without changing occupancy.
- Expire under a four-event limit, drain and continue without losing any group's
  missing-part evidence; duplicates do not extend first-admission deadlines.
- Saturate a four-message completion queue, reject final segments atomically,
  drain messages, explicitly retry those final segments and verify one complete
  message per group. Retained earlier segments survive the capacity rejection.
- Expire completion tombstones and assert all accounting returns to zero.

The time supplied to standalone Receiver is synthetic so TTL boundaries are
deterministic. The network and deadline machinery use real TCP and real time.
Saturation does not silently evict acknowledged data.

## Measurement boundaries

After warmup and periodically between joined batches, explicit GC precedes
ReadMemStats. The record includes HeapAlloc, HeapInuse and HeapObjects, Session
reader/writer/expiry goroutine stack counts, and Linux `/proc/self/fd` counts.
The regression guards are post-GC live heap growth below 8 MiB over baseline,
unchanged Session-loop count, and at most two extra descriptors. Exact retained
state assertions apply on every connection and receiver cycle, independently of
these intentionally generous process-level thresholds.

These measurements are not RSS, race-detector shadow memory, bytes copied,
allocations per operation, latency percentiles or production throughput. The
local one-minute run is a resource regression probe, not an overnight soak or
a proof that all workloads are bounded. Existing [Codec benchmarks](../README.md#encode-decode-and-memory-copy-benchmarks)
retain their separate workload and allocation methodology.

## Legacy peer scope

The optional [legacy harness](../interop/legacy/README.md) loads the unchanged
local go-smpp simulator in an isolated temporary module. Inspected reference HEAD:
`743fa92c92a2e87fc64d53c677f774a856c1b6e3`. Its working tree already contains
modified and untracked reviews/tests; those are preserved. The recorded run
uses the working checkout, not a claim that all its bytes match HEAD.

Only the simulator's SMPP 3.4 TRX/UDH path is checked by these tests. Its default
echo handler is replaced through the supported Handler hook. Its bind sequence
bug requires a fresh process for the positive case and has a separate expected
timeout test. Missing TLV negotiation stays disabled; 3.3/5.0/TLV support must
not be inferred from this peer. The three-profile independently authored byte
peers remain separate evidence in [Session lifecycle](session-lifecycle.md).

Raw local output is kept in [resource soak](validation/soak-go1.26.4-linux-amd64.txt)
and [legacy simulator](validation/legacy-go-smpp-local.txt). The accompanying
[validation record](validation.md) reports executed results and environment.
No external SMSC, old regression suite, crash-recovery store, broadcast support
or production delivery guarantee was validated here.

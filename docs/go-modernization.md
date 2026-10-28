# Go standard-library usage audit

2026-10-28, Go 1.26.4 linux/amd64. The module still requires **Go 1.26.0**;
this pass uses current APIs available at that minimum, without raising it to a
newer release. Public SMPP APIs, ownership contracts and the shared architecture
are unchanged. No third-party dependency was added.

The audit combined Go 1.26's `go fix -diff ./...` with a source review of cloning,
membership checks, error extraction, goroutine accounting, expiry cleanup,
iteration and benchmark/test helpers. API availability was checked against the
installed toolchain's `api/go1.*.txt` and standard-library source. The
[Go 1.26 release notes](https://go.dev/doc/go1.26) describe the revamped modernizers,
`errors.AsType` and expression arguments to `new`.

## Applied choices

| API / feature | Available since | Use in this tree |
| --- | --- | --- |
| `errors.AsType[T]` | Go 1.26 | Ordinary tests extract PeerError and net.Error directly, without pointer-to-output variables. Sentinel checks still use errors.Is. |
| `new(expr)` | Go 1.26 | Built-in encoding pointer cases in the R1 tests no longer need an addressable temporary. |
| `sync.WaitGroup.Go` | Go 1.25 | Session reader/writer/expiry tasks, Server handlers and concurrency/resource tests use counted task launch. Loop bodies no longer call Done themselves. |
| `slices.Contains` | Go 1.21 | Codec version and command membership checks; the two private loop wrappers were removed. |
| `slices.Clone` | Go 1.21 | Server profile lists, codec registration metadata and submit_multi destination/failure lists. Mutable nested payload/TLV values still need their existing deep copies. |
| `maps.DeleteFunc` | Go 1.21 | Expired reference leases and completed-message tombstones are removed under their existing locks. |
| `min` / `max` | Go 1.21 | Inbound expiry ticker interval remains clamped to the same 1 ms–1 s bounds. |
| Integer / slice range | Go 1.22 / earlier | Fixed-count loops and truncation/field-value tests use direct range forms. |
| `fmt.Appendf` | Go 1.19 | The local resource workload formats directly into payload bytes. |

The constructor registers all three Session tasks before returning the Session.
Close/Wait, response ordering, writer priority and context cancellation retain
their existing contracts. WaitGroup.Go does not make application callbacks safe
to panic; it is task accounting, not a panic-recovery facility.

Existing uses of `bytes.Clone`, `clear`, `context.AfterFunc`, `testing.T.Context`,
`testing.B.Loop`, `testing/synctest` and binary append helpers were retained.
Examples already use suitable context and protocol APIs. The optional legacy
interop harness is a separately built, tagged compatibility test and was left
unchanged; no reference repository was edited.

## Deliberately retained forms

- `append([]byte(nil), src...)` in paths that normalize an empty payload to nil:
  bytes.Clone preserves a non-nil empty input, so blanket replacement would
  change observable slice semantics. Existing Clone APIs that promise nil/empty
  preservation continue to use bytes.Clone or slices.Clone.
- `tlv.Fields.DeleteAll` allocates a new field list. slices.DeleteFunc would edit
  the existing backing array and clear its tail, changing previously borrowed
  field-list views. There is no new in-place mutation contract in this pass.
- Incomplete-group expiry still uses an explicit loop: it records bounded events
  and may stop with ErrCapacity. A predicate-only deletion helper cannot express
  that partial-progress/error contract as clearly.
- Position-sensitive byte parsing, exact-length `copy`, and manually authored
  protocol fixtures remain explicit. Reusing production serializers for fixtures
  would weaken their independence. reflect.DeepEqual still checks nested values
  where a shallow slice comparison would be insufficient.
- Explicit test cancellation is retained where it drives the behavior under
  test. A test context ending at cleanup is not equivalent to early cancellation.
- crypto/rand plus hex encoding still produces the established message-ID format;
  switching to rand.Text would change the alphabet and length.

## Validation and allocation observations

Formatting, `go fix -diff ./...`, build, vet and the full race suite passed.
The race run included local TCP/TLS tests with loopback permission. No external
SMSC or remote CI was used. The default modernizer reports no remaining diff
for this package graph; that is not an assertion that every manual form should
be replaced or that it examines the separately built legacy harness.

Two 5-second, two-worker fuzz smoke runs also passed: FuzzDecode (148,768
executions) and FuzzPrepareVersionEncoding (38,886 executions). Counts are
observations, not coverage guarantees; the latter finished in about 6 seconds
including harness/minimization overhead.

Before/after codec measurements used the same Go 1.26.4 toolchain, AMD Ryzen 7
6800H, linux/amd64, `-cpu=1`, and command:

```sh
go test ./codec -run='^$' -bench='BenchmarkCodec(Encode|Decode)$' \
  -benchmem -benchtime=100ms -count=3 -cpu=1
```

All **27 cases × 3 runs per revision** retained identical B/op and allocs/op.
Raw results: [before](benchmarks/go-modernization-bench-before-go1.26.4.txt),
[after](benchmarks/go-modernization-bench-after-go1.26.4.txt).
These are codec measurements, not Session or Receiver heap measurements.

The first short sample showed DataSM/V50/140 decode median rising from 653.1 to
742.8 ns/op. A focused 300 ms × 5-run check, run after then before using a source
overlay of the pre-edit tree, measured medians of 656.1 ns/op after and 711.9
ns/op before. Both retained 408 B/op and 9 allocs/op. Raw focused results:
[before](benchmarks/go-modernization-focus-before-go1.26.4.txt),
[after](benchmarks/go-modernization-focus-after-go1.26.4.txt). The initial increase
did not repeat; these short, unpinned observations do not establish a throughput
improvement or a precise timing regression bound. No zero-copy claim is made.

The older README benchmark table remains a labeled historical baseline. This
audit does not overwrite it or claim that B/op counts actual copy traffic.

For subsequent maintenance, preview `go fix -diff ./...`, inspect ownership and
concurrency implications, then apply the appropriate changes and rerun checks.
This pass supports the release contract review but does not complete R2–R4.

# Go 1.27 upgrade assessment

**Deferred by the owner on 2026-10-28.** Retained as research for later work;
neither the upgrade nor adoption of its APIs is a Release 1.0 prerequisite.
The deferral returned work to R1; see the release checklist for current progress.
Go 1.26 remains the minimum.

Assessed 2026-10-28 against the public Go release notes, versioned API inventory
and the current smppkit source. **Assessment only: no Go 1.27 build, test or
benchmark was executed in this pass.** The installed toolchain is Go 1.26.4;
`go.mod` still declares Go 1.26.0. Existing benchmark results remain Go 1.26
observations. Working changes and reference repositories were preserved.

The [official release history](https://go.dev/doc/devel/release) lists Go 1.27.3
(2026-10-19) as the latest 1.27 patch at assessment time. Recheck the latest patch
when executing the upgrade. The former next-step proposal is retained as
[deferred U1 work](release-1.0.md#deferred-u1--upgrade-the-go-baseline).

## Changes worth applying or measuring

| Candidate | Current location | Decision for U1 |
| --- | --- | --- |
| `testing/synctest.Sleep` | `session_r1_test.go`, response-before-write-completion timeout cases | Use after the minimum version changes. The two timeout branches can combine fake-time advancement with waiting for runnable work to settle. Keep explicit Wait calls for cancellation and completed-write branches, and the earlier response-dispatch barrier. |
| Stable `goroutineleak` profile | `session_lifecycle_test.go` and `session_soak_test.go` | Add local diagnostic collection after shutdown/join. Retain existing join, resource and lifecycle assertions. No diagnostic listener or public Session API is needed. |
| Small-object allocation improvements | `codec/codec.go`, `codec/bodies.go`, TLV decoding, message preparation | Compare unchanged source under both toolchains before any modernization. Potential runtime savings are a hypothesis; no reduction in allocations or copies has been measured under 1.27. |
| Embedded-field struct literals and `go fix`'s `embedlit` | PDU construction in examples/tests, including BindFields and SMFields | Preview selectively after upgrading. Keep nested literals where grouping protocol fields helps readers. No exported struct or interface change is required. |

[`synctest.Sleep`](https://pkg.go.dev/testing/synctest@go1.27.3#Sleep) combines
time.Sleep with synctest.Wait inside a test bubble. Our R1 regression already
performs both operations, so this is a clarity improvement, not a new correctness
fix. The sleeps in the real network/resource tests must not be replaced blindly:
those tests do not run inside a bubble.

[`runtime/pprof`](https://pkg.go.dev/runtime/pprof@go1.27.3#Profile) exposes the
leak profile through Lookup/WriteTo. The detector covers a subset of permanently
blocked goroutines; a clean profile does not prove absence of leaks. Capture
write errors and require the profile to exist in the 1.27-only validation path.
Keep profile output local to validation, and preserve the existing memory and FD
checks. See the [runtime changes](https://go.dev/doc/go1.27#runtime) for the
detector's reachability limitations and allocation changes.

## New functions that do not justify a current replacement

The [versioned API inventory](https://raw.githubusercontent.com/golang/go/go1.27.3/api/go1.27.txt)
was checked alongside call sites, rather than treating every addition as an
optimization opportunity:

| API | Finding in this tree |
| --- | --- |
| `bytes.CutLast`, `strings.CutLast` | No LastIndex-and-slice pattern was found. `internal/wire.Reader.C` deliberately reads the first NUL and validates the bound; splitting at the last separator would change wire semantics. |
| `math/rand/v2.(*Rand).N` | No current seeded random workload needs it. Future randomized fault tests could use a local seeded Rand; production IDs must retain cryptographic randomness. |
| `uuid.NewV4` / `NewV7` | `messaging.localID` and `message.newID` already produce opaque random hex identifiers. UUID formatting/version semantics would change those values without a demonstrated library benefit. Keep the existing ID contract. |
| `maphash.Hasher` / `ComparableHasher` | Receiver keys already fit native maps. No custom hash container is needed. |
| `url.URL.Clone`, `url.Values.Clone`, `httptest.NewTestServer` | There is no URL ownership or HTTP server path to modernize. SMPP tests use net.Conn/net.Pipe and independent wire peers. |
| `encoding/json/v2` / `jsontext`, SQL scan additions | No matching core serialization/database path. Persistence remains application responsibility. |
| `big.Int.Divide`, compiler AST/type helpers | No matching production calculation or Go-source analysis path. TLS test certificate serial numbers do not need new rounding behavior. |

Generic methods are available in Go 1.27, but cannot implement interface methods.
There is no established need to redesign Request, Codec or Encoding around them.
Experimental SIMD is also outside this upgrade: profile actual byte/character
loops before considering architecture-specific implementation. These language
and tooling changes are described in the [release notes](https://go.dev/doc/go1.27).

The previous [Go 1.26 modernization](go-modernization.md) already adopted suitable
WaitGroup.Go, errors.AsType, slices/maps helpers and current testing APIs.
Retain its nil/empty, ownership and copy decisions. The removal of the fmtappendf
modernizer does not deprecate fmt.Appendf or require reverting our workload code.

## Compatibility checks during the upgrade

The [release notes](https://go.dev/doc/go1.27) also identify changes to timers,
closure names, formatting, vet defaults, UnixConn EOF, Unicode tables and platform
support. Apply these checks to our code:

- Repeat request cancellation, writer join, deadlines, crossed unbind and expiry
  tests. The source has no asynctimerchan override; keep the current timer
  ownership rather than adding legacy channel-drain workarounds.
- Check that stack-based Session loop counts still recognize known running
  loops before trusting a zero after shutdown. They currently match named
  methods, not closure symbols, but stack text is still diagnostic formatting.
  Leak profiles supplement these observations rather than replacing joins.
- Preview go fix and inspect gofmt changes using the selected 1.27 toolchain.
  Keep stdversion vet enabled; adding a new API while claiming a 1.26 minimum
  would make the module contract inconsistent.
- Rerun supplied-connection and TLS tests. Audit EOF checks for errors.Is rather
  than assuming an error wrapper type. The library does not configure tls.Config.Rand
  or need a new public certificate-observation API.
- Preserve protocol-defined GSM mappings and strict UTF-8/UTF-16 checks. New
  Unicode classifications must not broaden SMPP alphabets or DCS interpretation.
- Note the Go 1.27 macOS 13 minimum in the toolchain support documentation when
  the baseline changes. Linux validation alone is not evidence for other ports.

## Measurement and completion criteria

First compare the same pre-upgrade source snapshot with explicit Go 1.26.4 and
the selected 1.27 patch. Keep CPU, fixtures, benchmark flags and experiment
settings fixed; record exact toolchains and retain raw output. Then measure
again after any source modernization to separate compiler/runtime effects from
source changes. Use repeated runs and investigate material variation.

Cover BenchmarkCodecEncode, BenchmarkCodecDecode and BenchmarkCodecCopy with
`-benchmem`, plus message preparation. Compare ns/op, B/op and allocs/op. Faster
allocation does not imply fewer allocations; B/op is not copied bytes. Decode
must continue to return owned data. A lower allocation count is not grounds to
alias caller buffers or weaken MaxFrame/resource limits.

U1 must update the module minimum, README, support documents and CI together.
The existing workflow lists 1.26.x and 1.27.x, but that is not execution evidence.
After the minimum becomes 1.27, remove the 1.26 success job and explicitly set
GOTOOLCHAIN=local in validation so auto-selection cannot disguise which compiler
was tested. Update the temporary-module instructions for the optional legacy
harness without editing its reference repository.

Run build, vet, full race tests, examples and all checked-in fuzz targets on the
upgraded tree; retain the independent byte fixtures and R1 regressions. Record
new results in validation.md and README's benchmark section only when executed.
U1 is still unchecked; this document establishes no Go 1.27 compatibility or
performance result.

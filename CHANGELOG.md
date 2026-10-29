# Changelog

## v1.1.0 — release candidate, unreleased

Candidate notes finalized 2026-10-29. S0–S5 implementation and local review are
complete, including RV22–RV24 corrections. Submission is authorized; tagging
and publication are not. There is no release date yet. See the
[release record](docs/v1.1.0-release.md) and [release notes](docs/releases/v1.1.0.md).

- Add optional `smpp.Config.Logger *slog.Logger` to shared Session and
  Client/Server setup. Nil discards logs without consulting a global logger.
- Record safe lifecycle/request metadata and setup failures, including failed
  Server.Serve authentication. Arbitrary error text and PDU contents are excluded.
- Keep Close independent of handlers; Wait joins terminal runtime logging.
  Handlers execute synchronously outside locks and must return promptly.
- Add logging regression tests, allocation benchmarks, a standard logger bridge,
  and optional `-diagnostics` flags in Client/Server examples.

- Protect high-level text operations from unsupported UDH national-language
  declarations (IE 0x24/0x25). Prepare rejects before encoding; Receive preserves
  raw evidence and reports `message.ErrUnsupportedNationalLanguage` in DecodeError
  when decoding was requested. Raw-only and receipt handling are unchanged.

- Add codec.ReadFrame/ParseHeader/WriteFrame for bounded raw access, exact
  partial-read/write progress and explicit caller-owned transport policy.
- Wrap decode/read failures in DecodeError with categories and safe header/stream
  context; preserve prior sentinels, FieldError and I/O/extension causes.
  Unknown private commands still decode to Opaque.
- Add independent frame/error regressions, fuzzing, allocation benchmarks and a
  runnable framing example.

- Add opt-in `Config.EnquireLinkInterval` and `EnquireLinkTimeout` for shared
  Client/Server Session scheduling. Zero interval disables it; existing automatic
  replies are unchanged. Window/queue pressure skips probes, while started-probe
  expiry retires a bound session with `ErrEnquireLinkTimeout`.
- Stop scheduling at unbind/close, join it with Wait, and preserve uncertain
  business outcomes. Add independent wire, lifecycle/deadline/concurrency tests,
  allocation benchmarks and explicit example flags.
- Correct S5 heartbeat races: a completed correlated exchange cannot be retired
  merely because its deadline is also observable; incoming enquiries crossing
  unbind are consumed without queuing replies or reopening business admission.
  Add formal deadline/shutdown regressions and clarify the receipt support matrix.

- Add bounded, stateless `message.ParseReceipt` / `BuildReceipt` helpers, retaining
  separate text/TLV evidence, explicit report kinds and safe field diagnostics.
  Select a named text dialect explicitly; preserve opaque IDs, missing fields,
  unsupported states, duplicates and conflicts without implicit precedence.
- Construct existing DeliverSM/DataSM with explicit version/format and caller
  outcomes, using Codec preflight. Keep 3.3 TLV-free and distinguish standard
  fields from gateway text/error conventions. Add independent wire fixtures,
  boundary/concurrency/fuzz tests, allocation benchmarks and a receipt example.

- Fix the Go 1.26 CI fuzz budget: all 14 targets run for 1,000,000 executions
  each, avoiding the toolchain's timed-shutdown race without filtering failures
  or rerunning jobs. Retain Go 1.27 timed fuzzing, preserve failing corpus files,
  and let both compiler jobs finish independently. See the
  [diagnosis and validation](docs/v1.1.0-ci-fuzz.md).
- Correct RV25 in release automation: after CI completes, recheck remote main
  and the resolved tag commit against the tested SHA. Block publication on
  changed refs or lookup failures; add local regression coverage for lightweight
  and annotated tags. Library behavior is unchanged.

Go 1.26 and the existing protocol/runtime architecture are unchanged.
The [design](docs/v1.1.0-design.md) and [roadmap](docs/roadmap/v1.1.0.md)
record the completed scope. The [S5 recheck](docs/v1.1.0-s5-recheck.md) found no
remaining local correctness blocker; final submitted-revision CI and explicit
publication authorization remain release gates.
Raw framing and classified errors are implemented locally under the
[framing contract](docs/framing.md). See the [logging contract](docs/logging.md).

## 1.0.0 — 2026-10-28

Initial stable release of the documented SMS subset. The six public packages
follow the [v1 compatibility policy](docs/compatibility.md).

### Included

- A general-purpose Go SMPP module for Client, Server and standalone simulators:
  concrete PDUs, an immutable versioned Codec and one shared Session runtime.
- Explicit SMPP 3.3, 3.4 and 5.0 profiles for the
  [documented SMS command subset](docs/support.md). Standard/customer TLVs are
  supported where allowed in 3.4/5.0; 3.3 rejects all TLVs.
- Bind/authentication, bounded request/response windows, roles, cancellation,
  deadlines and shutdown; supplied TCP/TLS transports and explicit outbind/
  notification operations. No implicit reconnect or uncertain-request retry.
- Text preparation and sequential multipart sending with per-segment results;
  explicit GSM7/UCS2/Latin-1/UTF16BE policies, safe boundaries, UDH8/UDH16/SAR,
  optional message_payload mode and bounded reference leases.
- Bounded in-memory reception, reassembly, duplicate/conflict handling,
  namespace/port isolation, completion backpressure and expiry events. ACKs
  follow each segment's memory admission; transaction submissions require an
  application-owned result through the low-level response API.
- submit_multi with explicit destination variants and per-SME failures;
  standard TLV and mandatory-field validation; data_sm direction/mode checks.
- Runnable [examples](examples/README.md), independent protocol-byte fixtures,
  race/fuzz/resource tests, allocation/copy benchmarks, Apache-2.0 licensing and
  an explicit [AI-authorship disclosure](README.md#ai-authorship-and-review-status).

### Requirements and compatibility

Minimum Go: **1.26** (`go 1.26.0`). There are no third-party runtime module
dependencies. Root import `github.com/skill215/smppkit` uses package name `smpp`.
The old go-smpp API is not a compatibility target; existing applications adopt
this API explicitly. The library does not replace ygw or import sms-fw/DEI
business behavior. See the [compatibility policy](docs/compatibility.md).

### Boundaries users must retain

- SMPP 3.3 standard multipart and built-in Latin-1 selection are rejected;
  historical 3.3 commands and SMPP 5.0 broadcast are not implemented.
- GSM7 is unpacked; national language tables and packed septet inference are
  absent. UCS2 is strict; UTF16BE compatibility requires explicit selection.
- An OK submission header is not a final-delivery guarantee; submit_multi may
  still report individual failures. Transaction responses need mode-aware
  interpretation. Partly/fully written requests without responses are uncertain.
- Memory ACK does not survive a crash. Persistence, durable admission, routing,
  business delivery state and receipt aggregation belong to applications.
- Standard Codec validity is not full network conformance. Custom callbacks are
  trusted synchronous code and may run on the Session reader; they must bound
  work and avoid waiting for traffic on that Session.
- External SMSC interoperability remains unverified. The optional legacy
  simulator check covers only the documented 3.4 TRX/UDH path.

### Validation

[R1/R2](docs/r2-contract-audit.md) close the recorded RV01–RV21 findings and
audit the public contract. [R3](docs/r3-validation.md) records local build/vet/
race checks with Go 1.26.4 and 1.27.2, 11 timed fuzz targets, a ten-minute race
resource workload, the unchanged legacy simulator and 80 benchmark cases.
These are executed local observations, not a conformance or production-use
certificate. No cross-version benchmark speedup is claimed.

One timed Go 1.26.4 fuzz run failed at budget shutdown. The original log remains;
a diagnostic reproduction identifies the coordinator cancellation window, with
successful timed runs recorded separately. Unpatched timed fuzz jobs can still
encounter it; CI does not ignore errors or retry them into success.

[R4 local readiness](docs/r4-readiness.md) records the frozen API snapshot and
historical candidate distribution checks. Candidate `4749b177` passed
[remote CI on its first attempt](https://github.com/skill215/smppkit/actions/runs/40646388506):
Go 1.26.10 and 1.27.3, formatting, build, vet, full race and all 11 fuzz targets
(5 seconds each, two workers). The Go minimum remains 1.26. The owner authorized
merge and v1.0.0 publication on 2026-10-28. Final release documentation and the publication workflow must
also pass the configured CI before the release tag is created; see the
[release record](docs/release-1.0.md).

# Optional local go-smpp simulator interoperability

Requires Go 1.26 or newer and a local checkout of `github.com/skill215/go-smpp`.
From the smppkit repository root:

```sh
sh interop/legacy/run.sh /path/to/go-smpp
```

The script builds a temporary module, runs the tests with `-race`, then removes
that module. It never edits either checkout's go.mod/go.sum or the peer sources.
The build tag excludes this harness from the public library's ordinary build
and dependency graph. Use the script rather than adding the legacy dependency
to the main module or enabling the tag on the root test suite.

Dependency downloads are disabled by default (`GOPROXY=off`); the local module
cache must contain the reference checkout's dependencies. Explicitly set GOPROXY
if downloading dependencies is desired. The inspected peer imports x/text
v0.36.0 through its text packages; the recorded run used an existing local cache
and the reference go.sum. smppkit itself still has no third-party runtime
dependency. All traffic in the harness uses a local ephemeral loopback listener.

## What this test proves

The unmodified legacy `smpp/smpptest.Server` supplies bind/authentication,
transport, parsing and serialization. A handler written here through its public
Handler hook returns actual SMPP responses instead of its default request echo.
No old implementation source is copied or vendored. Test payloads and demo
credentials are synthetic.

- SMPP 3.4 TRX bind and disabled TLVs when sc_interface_version is absent.
- Two-part UDH16 SendText submission, actual segment bytes/boundaries inspected
  on the old codec side, individual opaque response IDs retained.
- Independently constructed UDH8 deliveries in reverse order with a duplicate;
  every segment ACK precedes the next delivery (window one), one complete message.
- Negative submit response, fully written request with no response, no automatic
  replay, enquire_link and unbind with explicit sequence echo in the handler.

The accepted long message is 301 GSM `A` characters; its two payloads contain
152 and 149 unpacked septets. The reverse deliveries encode `hello ` / `world`
with literal UDH8 headers. This harness deliberately does not use old text
encoding to establish character correctness, or smppkit's parser to validate
the simulator's received UDH16 bytes.

## Simulator limitations kept visible

The inspected old authentication code creates bind_resp with a process-global
sequence rather than echoing bind's sequence. A fresh child process guarantees
the first generated response happens to use 1, matching the first client bind.
This is one connection per child, not evidence that arbitrary repeated binds
work with that simulator. `TestLegacyBindSequenceMismatch` consumes a legacy
sequence first and confirms the unmatched response leads to a bounded timeout;
the library does not loosen correlation or rewrite the simulator's bytes.

Server.Close in this simulator closes the listener only; the harness also
closes its active connection and joins the smppkit Session. No simulator
sources, regression tests or review files are modified.

There is no sc_interface_version advertisement, so this run covers UDH, not SAR
or standard/customer TLVs. It makes no 3.3/5.0, TLS, delivery receipt, durability,
or full SMPP conformance claim. The [local validation record](../../docs/local-validation.md)
identifies the inspected commit, executed checks and resource-test boundaries.

The reference LICENSE grants MIT-style permission (copyright 2015 go-smpp
authors); its source banners call it BSD-style, so use the LICENSE text itself
when assessing redistribution. This is a test-only local dependency review, not
an endorsement of upstream maintenance or the old encoding/error semantics.
The prior review/regression materials remain outstanding reference evidence;
this harness does not rerun or certify those suites.

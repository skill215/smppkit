# Raw framing and decode diagnostics (v1.1.0, unreleased)

These APIs are implemented locally in `codec`, using the same bounded framing
primitive as Codec.Read and ReadBind. They do not own a connection or replace
strict protocol decoding. Go 1.26 remains the minimum.

## Public surface

```go
func ReadFrame(r io.Reader, maxFrame int) ([]byte, error)
func ParseHeader(src []byte) (FrameHeader, error)
func WriteFrame(w io.Writer, src []byte, maxFrame int) (int, error)
```

FrameHeader contains raw Length, Command, Status and Sequence. ParseHeader reads
at least 16 available bytes, ignores extras, and performs no validation or I/O.
The functions are stateless; callers must serialize access to shared readers,
writers and mutable buffers. No callback/extension interface gains methods.

ReadFrame returns one owned raw frame. The default maximum is 64 KiB; explicit
limits must be 16 bytes through 16 MiB. Invalid configuration reads nothing.
Only the four-byte length is acquired before checking the allocation bound. An
invalid length stops after those four bytes: no unbounded drain or resynchronizing
scan occurs. Partial failures return the exact received prefix, with capacity
clamped to its length. An unread zero-filled allocation tail is never evidence.
No partial buffer is retained by the library or reused for another result.

The caller supplies deadlines, cancellation and connection policy. A custom Reader
must obey io.Reader and make progress; these helpers do not invent a timeout around
an arbitrary blocking implementation. Complete framing permits further inspection,
not automatic trust or forwarding of an invalid PDU. Unknown bytes, duplicate TLVs
and binary/UDH content are unchanged by framing. Codec validation remains separate.

WriteFrame checks only exact declared/supplied length and the configured maximum,
then calls Write once with the original bytes. It allocates no frame copy and
performs no semantic validation, sequencing or implicit retry. Caller mutation
must wait until it returns. A short write with nil error yields io.ErrShortWrite;
a non-nil I/O error is returned even when all bytes were reported written. n is
actual reported progress. Impossible writer counts return n=0/io.ErrShortWrite.
Buffered writers require caller Flush. A retry of a partly written whole frame
can corrupt the stream or repeat a submission: the application owns that decision.

To modify an address, explicitly Decode a supported frame, edit its typed PDU,
and Append it. Encoding recalculates length. Decoding errors must not trigger
an unchecked splice or Opaque bypass of a standard command.

## DecodeError contract

Decode, Codec.Read, ReadBind, ReadFrame and short-input ParseHeader failures return
*codec.DecodeError. Use errors.As/AsType and its Kind, not Error text. Existing
ErrValidation/ErrUnsupported and I/O/extension causes remain discoverable with
errors.Is; FieldError remains available with errors.As. Structural FieldError
causes are not newly made ErrValidation. Encoding-only operations keep their
existing errors; WriteFrame validation wraps ErrValidation without DecodeError.

| Kind | Meaning |
| --- | --- |
| DecodeFraming | Frame length/limit or input acquisition failure |
| DecodeUnsupported | Known unsupported command/version |
| DecodeMandatory | Mandatory-field structure, including C-octet and trailing-byte failures |
| DecodeTLV | Optional-section structure or semantic validation |
| DecodeSemantic | Header or other body semantics outside the optional section |
| DecodeExtension | Registered command decoding callback failed or returned the wrong body |

Categories are marked at their decoding boundaries, not inferred from diagnostic
strings. Unregistered private commands still decode successfully as Opaque.
Reserved standard commands do not gain a bypass. Command callback errors are
classified as extensions even if their cause wraps another category. Custom TLV
validation remains an optional-section error under its existing sanitized cause.

The error snapshot distinguishes:

| Field | Exact meaning |
| --- | --- |
| HasHeader | All 16 raw header bytes were available |
| HeaderValid | Declared length is within the configured bounds and sequence/status rules pass |
| StreamRead | This error came from ReadFrame, Codec.Read or ReadBind |
| BytesRead | Bytes actually consumed by that call, only meaningful when StreamRead is true |
| FrameComplete | A stream call acquired exactly the declared frame |

HeaderValid does not imply a supported opcode/version, matching supplied slice
length, valid body, authorization to reply, or a bound session. It may be true
for a truncated body. Decode and ParseHeader leave all stream fields zero: a
provided slice proves nothing about how a caller acquired it.

A complete bad body from Codec.Read has FrameComplete=true; a short read or
invalid length has false. A caller may continue after a complete invalid body
if its own policy allows it, but this is not an automatic recovery instruction.
Unknown response correlation is Session context and cannot be inferred by a
standalone Codec. Do not reply automatically to a malformed response.

For compatibility, the underlying io.ReadFull cause is preserved. In particular,
EOF while reading the body immediately after a four-byte length has BytesRead=4,
whereas a clean pre-frame EOF has BytesRead=0. Never use errors.Is(io.EOF) alone
to claim a clean boundary. Deadline/transport errors also preserve their causes.

Error() includes only category and safe numeric header context. The unwrapped
cause may be application-controlled and contain secrets; inspect safe fields
instead of logging arbitrary cause strings. Field/offset diagnostics are present
through FieldError when a structural reader supplied them. All snapshots own no
raw payload and remain independent across concurrent decoding calls.

## Example and evidence

`go run ./examples/framing` exercises a hand-authored complete-but-invalid
submit_sm followed by a valid enquire_link, with explicit caller policy. It
shows raw retention, typed failure and byte-preserving forwarding without sockets.

Tests cover every truncated prefix of a frame, bounds checked before I/O,
fragmented/coalesced input, preserved errors, bind bootstrap, negative-response
bodies, unknown commands, malformed/duplicate TLVs, safe extension errors,
concurrency/ownership, partial writes and an independent address-edit fixture.
The [G1/G2 validation record](v1.1.0-framing-validation.md) records fresh checks.

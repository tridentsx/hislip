# hislip

A native Go implementation of HiSLIP, the IVI High-Speed LAN Instrument
Protocol, for the [GoTMC][gotmc] test and measurement stack.

HiSLIP is specified by [IVI-6.1][ivi61], revision 2.0. It is the LXI-era
replacement for VXI-11 and requires no ONC/Sun RPC.

## Status

Early development. Milestone 0 of the design specification, the protocol codec,
is implemented. The client, server and vendor extension packages are not yet
written.

The design specification is [docs/design-spec.md](docs/design-spec.md). Section
references in the source comments, such as "§5.1", refer to it.

This repository is not yet part of the `gotmc` organisation. It is developed
against the final module path, `github.com/gotmc/hislip`, so that no import
rewriting is needed when the upstream repository exists.

## Layout

```text
protocol/          wire format: header codec, message types, error codes,
                   MessageID arithmetic, version negotiation
server/            session state machine and the Device interface
client/            desktop client
extension/gotmc/   optional vendor extension for explicit reads
```

The `protocol` and `server` packages are the device side. They compile under
TinyGo for microcontroller firmware, which imposes two rules:

- imports are restricted to a small, verified subset of the standard library;
- the caller supplies every buffer, and the data path does not allocate.

Both rules exist because the same code runs on a Raspberry Pi RP2350 with a
cooperatively scheduled runtime and a stop-the-world garbage collector, where an
allocation in the data path can stall a timing-sensitive bus transfer.

The import boundary is enforced rather than merely documented. `TestDeviceImportGraph`
in the root package walks the transitive import graph of the device-side
packages and fails on anything outside an allowlist. The realistic failure is
not a deliberate bad import but an `fmt.Errorf` added to an error path long
after the boundary was agreed.

## Two rules worth knowing before reading the code

**Never size a buffer from the wire.** The header's payload length field is 64
bits wide and is supplied by the peer. `make([]byte, header.Length)` is a
denial-of-service vector on an unauthenticated TCP port. Use
`protocol.CheckLength` against the negotiated maximum, then stream through a
fixed scratch buffer with `protocol.ReadPayload`.

**Decoding is permissive on purpose.** `protocol.DecodeHeader` validates the
prologue and nothing else. A receiver has to be able to decode a header it
intends to reject, so that it can consume the payload and stay synchronised with
the peer. Acceptance decisions belong to the session layer, using
`MessageType.Defined`, `MessageType.Reserved`, `MessageType.LegalOn`,
`MessageType.MinVersion` and `CheckLength`.

## Testing

```sh
go test ./...
go test ./... -race -cover
```

Golden wire vectors are written out by hand from IVI-6.1 rather than generated
by the code under test, and the message type table is transcribed twice — once
in `protocol/constants.go` and once in `protocol/constants_test.go` — so that a
transcription error has to be made identically in both places to go unnoticed.

## A defect in IVI-6.1

Table 4 lists `DeviceClearAcknowledge`, message type 9, as an asynchronous
message. The Device Clear transaction description in section 6.12 contradicts
this: step 7 of the client procedure reads "Wait for the server to respond with
DeviceClearAcknowledge on the synchronous channel", and the message summary
table also gives the channel as synchronous.

This implementation follows the transaction description and treats the message
as synchronous. See `TestDeviceClearAcknowledgeChannel`. The intent is to
confirm the behaviour against independent third-party HiSLIP clients before the
server is released.

## Licence

MIT. See [LICENSE.txt](LICENSE.txt).

[gotmc]: https://github.com/gotmc
[ivi61]: https://www.ivifoundation.org/downloads/Protocol%20Specifications/IVI-6.1_HiSLIP-2.0-2020-04-23.pdf

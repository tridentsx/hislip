# Design Specification: GoTMC HiSLIP + TinyGo PoE-to-GPIB Adapter

**Status:** Draft design specification — Revision 3  
**Date:** 2026-09-21  
**Target:** GoTMC ecosystem + RP2350/TinyGo PoE-to-GPIB instrument adapter  
**Primary use case:** one Ethernet/PoE adapter per GPIB instrument, plugged directly into the instrument's IEEE-488 connector

---

## 1. Executive summary

The system consists of two related deliverables:

1. **A native Go HiSLIP implementation for GoTMC**
   - Standards-oriented HiSLIP client implementation.
   - Server/device-side implementation designed so that the protocol engine can run under TinyGo.
   - Integration with `gotmc/visa`.
   - Optional GoTMC vendor extension for an explicit read request, needed to make a generic GPIB bridge fully transparent for non-SCPI instruments.
   - Shared wire codec and state-machine code between desktop Go and TinyGo firmware.

2. **A low-cost PoE Ethernet-to-GPIB adapter**
   - WIZnet W5500-EVB-Pico2 (RP2350 + W5500).
   - WIZPoE-P1 isolated IEEE 802.3af PoE module.
   - TI SN75160B data-bus transceiver.
   - TI SN75161B management-bus transceiver.
   - 24-pin male right-angle PCB-mount IEEE-488 connector.
   - TinyGo firmware exposing HiSLIP on TCP 4880 and raw SCPI on TCP 5025.
   - Adapter is System Controller and Controller-in-Charge for one locally attached GPIB instrument.

The central architectural rule is:

> HiSLIP, raw SCPI, USB debug and any future management interface never manipulate GPIB pins directly. They submit operations to one serialized GPIB engine.

This guarantees deterministic IEEE-488 bus ownership, simplifies timeout handling, and keeps protocol code separate from timing-sensitive bus code.

A second important design rule is:

> The reusable HiSLIP server core must not depend on `net.Conn`, an operating system, reflection-heavy code, or unbounded allocation.

The device-facing server core therefore depends on small `io`-like stream interfaces plus a `Device` abstraction. The normal Go client may use `net`, while TinyGo imports only the protocol and server-core packages.

---


## Revision 2 changes

This revision makes **HiSLIP Synchronized Mode mandatory for the PoE-to-GPIB device profile**.

Normative profile rules:

- the generic `gotmc/hislip` client **MUST** support both Synchronized Mode and Overlap Mode as required for a general-purpose HiSLIP client;
- the reusable server library **MAY** support both modes;
- the PoE-to-GPIB firmware profile **MUST support Synchronized Mode**;
- the PoE-to-GPIB firmware profile **MUST NOT support Overlap Mode**;
- the bridge **MUST advertise/prefer Synchronized Mode during initialization**;
- the bridge **MUST negotiate Synchronized Mode during Device Clear** and decline an Overlap proposal;
- all GPIB request/response exchange semantics, MAV handling, interrupted-response handling and bridge-side response tracking are defined against Synchronized Mode.

This change is intentional: one physical IEEE-488 instrument is fundamentally a serialized message-based resource. Emulating overlapping command/response execution in the bridge would add buffering and ambiguity without improving compatibility with the underlying GPIB device.

Note on the cost of this decision: IVI-6.1 section 3 already states that "HiSLIP servers shall support either synchronized or overlapped mode or both", and a HiSLIP session begins in Synchronized Mode. The profile rule above is therefore conformant by construction and cheap to implement — it is the `Prefer Overlap` bit of the InitializeResponse control code held at 0, plus feature bit 0 held at 0 in the Device Clear feature negotiation. The expensive part of Synchronized Mode is not mode selection but the RMT and interrupted machinery specified in §11.3.1.

---

## Revision 3 changes

Revision 3 retains the TinyGo firmware path, adds PIO as a required firmware mechanism, and closes the gaps found in the Revision 2 design review. It adds no new product scope.

**Protocol completeness (Part I)**
- Added the complete message-type table, the vendor-specific type range, and the IVI-defined error-code tables, transcribed from IVI-6.1 (§4.1, §4.3, §21.1).
- Added the normative MessageID arithmetic that Revision 2 described only conceptually (§4.2).
- Recorded a defect in IVI-6.1 Table 4 that implementers will hit (§4.4).
- Added the RMT-expected / RMT-delivered mechanism, which is mandatory for a Synchronized Mode server and was absent from Revision 2 (§11.3.1).
- Added the two-message Interrupted transaction (§11.3.2).
- Added the normative Synchronized Mode MAV rule, which is a function of MessageID equality and not merely of buffer occupancy (§11.4.1, §16.4).
- Added the Device Clear message sequence, channel assignment and timeout guidance (§13.1).
- Defined bridge behaviour when a response is expected but the instrument produces none (§17.3).
- Specified the vendor extension wire format and its capability negotiation (§17.4).

**Concurrency and scheduling (Part I / Part III)**
- Corrected the operation priority model. HiSLIP ordering is a property of the channel a message arrives on, not of its perceived urgency; Trigger is a synchronous-channel message and MUST NOT overtake queued data (§47).
- Added a mechanism allowing asynchronous operations to preempt an in-flight GPIB transfer between bytes, so that status query and device clear remain responsive while the instrument is slow (§47.1).
- Added the TinyGo runtime constraints the firmware must be designed against: cooperative scheduling within a core, no goroutine-to-core affinity, stop-the-world GC across cores, and the resulting zero-allocation rule (§5.2, §23.1).

**PIO (Part III)**
- PIO is promoted from a deferred optimization to the required implementation of the three-wire byte handshake in firmware v1. It is the only mechanism on this platform that makes handshake timing independent of the Go scheduler and garbage collector. A direct-GPIO implementation is retained as a bring-up and diagnostic path (§40).
- The GPIO map is now constrained by PIO pin-grouping rules, and the rationale is recorded so that later revisions do not silently break PIO feasibility (§28.1).

**Network stack (Part III)**
- Rewrote the W5500 socket budget against the device's actual listen model, in which a listening socket becomes the connection and there is no BSD-style accept queue. HiSLIP requires two connections to the same port (§42).
- Added an application-level session idle timeout, because the W5500 driver does not implement socket options and therefore offers no TCP keepalive; without this, an unplugged client holds a session and its sockets indefinitely (§42.2, §49.1).
- Recorded DHCP and mDNS as firmware-owned implementation work rather than platform features (§42.3).

**Hardware (Part II)**
- Added the IOVDD precondition for 5.5 V fault tolerance, and the power-sequencing requirement that follows from it (§27.1).
- Added erratum RP2350-E9 and its consequences for pull configuration on exactly the pads this design relies on (§27.2).
- Added the required external resistors and the defined bus state for MCU reset, unprogrammed and brown-out conditions (§27.3).
- Made a numeric power budget mandatory rather than asserted (§27.4).
- Recorded that the SN75161B choice makes the adapter permanently System Controller, with the resulting labelling requirement (§26.5).
- Recorded the secondary-addressing decision and the concrete controller address (§38.1).

**Document control**
- Added conformance language and requirement identifiers (§1.1).
- Added measurable throughput and latency targets so that Milestone 8 has an exit criterion (§62).
- Corrected the hardware acceptance criterion that tested the wrong pins (§64).
- Added a threat model (§66), a firmware update path (§51.1), a risk register (§67) and a requirement traceability index (§68).

---

## 1.1 Conformance language and requirement identifiers

The key words **MUST**, **MUST NOT**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT** and **MAY** are to be interpreted as described in RFC 2119.

Where earlier revisions of this document wrote "recommended", read SHOULD. Where they wrote "preferably", read MAY with a stated preference. Statements of preference are not testable and MUST NOT be cited by acceptance criteria.

Testable requirements carry an identifier:

```text
[R-<AREA>-<NNN>]

AREA = PROTO   HiSLIP wire format and codec
       SYNC    Synchronized Mode semantics
       SRV     Server and session behaviour
       DEV     Device abstraction and GPIB mapping
       FW      Firmware and platform
       HW      Hardware
       SEC     Security
       DOC     Process and deliverables
```

[R-DOC-010] Every requirement identifier MUST be defined exactly once and MUST be referenced by at least one acceptance criterion in Part V. §68 holds the index.

---

# PART I — GoTMC HiSLIP

## 2. Goals

The GoTMC HiSLIP implementation shall:

- Implement IVI-6.1 HiSLIP wire framing and session semantics.
- Work as a normal Go client on Linux, macOS and Windows.
- Fit naturally into the current GoTMC stack:

```text
GoTMC application
       |
       v
   gotmc/ivi
       |
       v
   gotmc/visa
       |
       +-- USBTMC
       +-- ASRL
       +-- TCPIP/SOCKET
       +-- TCPIP/HiSLIP
```

- Provide a server core usable by TinyGo.
- Avoid ONC/Sun RPC entirely.
- Support standard VISA resource addresses such as:

```text
TCPIP0::192.168.1.42::hislip0::INSTR
```

- Preserve non-SCPI/binary transfers.
- Support HiSLIP device clear, trigger, status byte, SRQ and remote/local semantics.
- Support HiSLIP locking in the server.
- Make payload handling streaming rather than requiring the whole message in RAM.
- Be testable without physical instruments.

## 3. Non-goals for the first production milestone

The first firmware release does not need:

- VXI-11.
- ONC RPC.
- HiSLIP secure connection/TLS/SASL.
- HiSLIP authentication.
- IPv6.
- Multiple virtual instruments behind a single adapter.
- Multiple physical GPIB instruments on one adapter.
- Parallel poll.
- Secondary (extended) GPIB addressing — see §38.1.
- Pass control — precluded by the transceiver choice, see §26.5.
- Network firmware update — see §51.1.
- HTTP configuration or LXI identification server — see §42.2.
- HS488 non-interlocked handshake — see §40. Not precluded by the PIO approach, but not claimed to be achievable either.
- Full LXI certification.

These are not architectural exclusions. The interfaces must leave room for later addition.

Overlap Mode is a different case: it is a permanent normative exclusion for this device profile (§11), not a deferred feature.

---

## 4. Standards baseline

The implementation is based on **IVI-6.1 HiSLIP 2.0**. The protocol uses two TCP connections to the same server port, conventionally TCP **4880**:

- synchronous channel: normal data, END semantics, trigger, synchronized protocol traffic;
- asynchronous channel: device clear, status, service request, locking and related out-of-band operations.

Every HiSLIP packet begins with a 16-byte big-endian header:

```text
Offset  Size  Field
0       2     "HS"
2       1     Message type
3       1     Control code
4       4     Message parameter
8       8     Payload length
16      N     Payload
```

The implementation shall parse both protocol 1.x and 2.0 negotiation.

Mode support is deliberately split between the reusable library and the GPIB bridge profile:

```text
gotmc/hislip client
    MUST support Synchronized Mode
    MUST support Overlap Mode

generic gotmc/hislip server
    MUST support Synchronized Mode
    MAY support Overlap Mode

PoE-to-GPIB server profile
    MUST support Synchronized Mode
    MUST NOT support Overlap Mode
```

The recommended rollout is:

- **Milestone A:** HiSLIP 1.0 General capability, Synchronized Mode.
- **Milestone B:** full lock behavior, robust interrupted handling, complete synchronized Device Clear behavior.
- **Milestone C:** HiSLIP 2.0 General capability and descriptors, while the GPIB device profile remains Synchronized-only.
- **Optional later:** Secure Connection capability, TLS and SASL.

The TinyGo adapter should initially advertise a highest supported version of **1.0**, even though the common codec understands the 2.0 header/message namespace. This prevents the firmware from claiming HiSLIP 2.0 behavior before the additional 2.0 transactions have been validated.

### 4.1 Message types

Transcribed from IVI-6.1 Table 4. "Capability" is `General` for messages every server must support and `SC` for messages available only when the server advertises the Secure Connection capability. The `Min` column is the minimum negotiated protocol version.

```text
 #  Message                            Channel   Cap  Min
 0  Initialize                         Sync      Gen  1.0
 1  InitializeResponse                 Sync      Gen  1.0
 2  FatalError                         Either    Gen  1.0
 3  Error                              Either    Gen  1.0
 4  AsyncLock                          Async     Gen  1.0
 5  AsyncLockResponse                  Async     Gen  1.0
 6  Data                               Sync      Gen  1.0
 7  DataEnd                            Sync      Gen  1.0
 8  DeviceClearComplete                Sync      Gen  1.0
 9  DeviceClearAcknowledge             Sync      Gen  1.0   see §4.4
10  AsyncRemoteLocalControl            Async     Gen  1.0
11  AsyncRemoteLocalResponse           Async     Gen  1.0
12  Trigger                            Sync      Gen  1.0
13  Interrupted                        Sync      Gen  1.0
14  AsyncInterrupted                   Async     Gen  1.0
15  AsyncMaximumMessageSize            Async     Gen  1.0
16  AsyncMaximumMessageSizeResponse    Async     Gen  1.0
17  AsyncInitialize                    Async     Gen  1.0
18  AsyncInitializeResponse            Async     Gen  1.0
19  AsyncDeviceClear                   Async     Gen  1.0
20  AsyncServiceRequest                Async     Gen  1.0
21  AsyncStatusQuery                   Async     Gen  1.0
22  AsyncStatusResponse                Async     Gen  1.0
23  AsyncDeviceClearAcknowledge        Async     Gen  1.0
24  AsyncLockInfo                      Async     Gen  1.0
25  AsyncLockInfoResponse              Async     Gen  1.0
26  GetDescriptors                     Either    Gen  2.0
27  GetDescriptorsResponse             Either    Gen  2.0
28  StartTLS                           Sync      SC   2.0
29  AsyncStartTLS                      Async     SC   2.0
30  AsyncStartTLSResponse              Async     SC   2.0
31  EndTLS                             Sync      SC   2.0
32  AsyncEndTLS                        Async     SC   2.0
33  AsyncEndTLSResponse                Async     SC   2.0
34  GetSaslMechanismList               Sync      SC   2.0
35  GetSaslMechanismListResponse       Sync      SC   2.0
36  AuthenticationStart                Sync      SC   2.0
37  AuthenticationExchange             Sync      SC   2.0
38  AuthenticationResult               Sync      SC   2.0

39-127    reserved for HiSLIP extensions
128-255   VendorSpecific, either channel
```

The message name in IVI-6.1 is `DataEnd`. Earlier revisions of this document wrote `DataEND`; the two refer to message type 7.

[R-PROTO-010] The codec MUST reject an unknown or reserved message type with a non-fatal `Error`, code 1, and MUST NOT close the session.

[R-PROTO-011] The codec MUST reject a message that arrives on a channel other than the one listed above. Messages marked `Either` are legal on both.

[R-PROTO-012] The server MUST reject a message whose minimum version exceeds the negotiated protocol version.

[R-PROTO-013] Vendor-specific messages MUST use a type in 128–255. The GoTMC extension MUST NOT be emitted before capability negotiation succeeds (§17.4).

The firmware profile implements types 0–25 only. Types 26–38 are out of scope for the first production milestone (§3) but MUST be rejected per R-PROTO-012 rather than silently ignored.

### 4.2 MessageID arithmetic

Revision 2 described MessageID correlation conceptually. The following values are normative and are a common source of interoperability failure.

```text
initial value                  0xffffff00
increment                      +2, unsigned 32-bit, wrap-around permitted
set by client on               Data, DataEnd, Trigger
reset to 0xffffff00 on         Initialization, and after Device Clear
```

[R-PROTO-020] The client MUST maintain a MessageID counter initialised to `0xffffff00`, MUST place the current value in the message parameter field of every `Data`, `DataEnd` and `Trigger` message, and MUST then increment it by two in an unsigned 32-bit sense.

[R-PROTO-021] The client MUST reset the counter to `0xffffff00` on initialization and after Device Clear completes.

[R-PROTO-022] When the server sends `DataEnd` it MUST set the MessageID to that of the client message containing the end-of-message which generated the response.

[R-PROTO-023] When the server sends `Data` it MAY set the MessageID as in R-PROTO-022, but only when that end-of-message is at the end of the identified message. Otherwise the server MUST set the MessageID to `0xffffffff`.

[R-PROTO-024] In `AsyncStatusQuery` the client MUST send the MessageID of its most recent `Data`, `DataEnd` or `Trigger`. Immediately after Initialization and after Device Clear, before any such message has been sent, the client MUST use `0xfffffefe`, that is `0xffffff00 - 2`.

[R-PROTO-025] The client MUST validate the MessageID of received `Data` and `DataEnd` against its most recently sent value, and MUST discard the offending message and any buffered partial response when they do not match. `Data` carrying `0xffffffff` is exempt from this check.

R-PROTO-023 is the rule that lets the bridge chunk a large GPIB response: intermediate `Data` packets may be sent with `0xffffffff` while the terminating `DataEnd` carries the true MessageID.

### 4.3 Error codes

Fatal errors, IVI-6.1 Table 14. Sending a `FatalError` message is followed by closing both channels.

```text
0        Unidentified error
1        Poorly formed message header
2        Attempt to use connection without both channels established
3        Invalid Initialization Sequence
4        Server refused connection due to maximum number of clients exceeded
5        Secure connection failed
6-127    reserved for HiSLIP extensions
128-255  device defined
```

Non-fatal errors, IVI-6.1 Table 16. After sending `Error` the sender returns to normal processing.

```text
0        Unidentified error
1        Unrecognized Message Type
2        Unrecognized control code
3        Unrecognized Vendor Defined Message
4        Message too large
5        Authentication failed
6-127    reserved for HiSLIP extensions
128-255  device defined
```

The payload of both messages is a human-readable ASCII description. A zero-length payload is legal.

[R-PROTO-030] The firmware MUST use the numeric codes above and MUST NOT invent codes in the reserved range 6–127. Bridge-specific conditions MUST use 128–255 and MUST be documented in §21.1.

### 4.4 Known defect in IVI-6.1 Table 4

Table 4 lists `DeviceClearAcknowledge` (type 9) as an **asynchronous** message. The Device Clear transaction description contradicts this: the client procedure in IVI-6.1 section 6.12 step 7 reads "Wait for the server to respond with DeviceClearAcknowledge on the synchronous channel", and the message summary table lists its channel as synchronous.

[R-PROTO-040] The implementation MUST treat `DeviceClearAcknowledge` as a synchronous-channel message, matching the transaction description and observed commercial behaviour, and the channel-legality check of R-PROTO-011 MUST accept it only on the synchronous channel.

[R-PROTO-041] The interoperability tests of §24.4 MUST confirm this against at least two independent third-party HiSLIP clients before Milestone 2 closes. If a client is found that sends or expects it on the asynchronous channel, this requirement is to be revisited rather than worked around silently.

---

## 5. Repository/package structure

Recommended repository:

```text
github.com/gotmc/hislip
|
+-- protocol/
|   +-- constants.go
|   +-- version.go
|   +-- header.go
|   +-- codec.go
|   +-- errors.go
|   +-- messages.go
|   +-- messageid.go
|
+-- client/
|   +-- client.go
|   +-- connect.go
|   +-- sync.go
|   +-- async.go
|   +-- lock.go
|   +-- status.go
|   +-- clear.go
|   +-- trigger.go
|   +-- remote.go
|
+-- server/
|   +-- server.go
|   +-- session.go
|   +-- device.go
|   +-- sync.go
|   +-- async.go
|   +-- locks.go
|   +-- status.go
|   +-- clear.go
|   +-- rmt.go            RMT-expected / RMT-delivered tracking, §11.3.1
|   +-- interrupted.go    Interrupted transaction, §11.3.2
|   +-- errors.go
|
+-- extension/
|   +-- gotmc/
|       +-- protocol.go
|       +-- negotiate.go
|       +-- readrequest.go
|
+-- internal/
|   +-- teststream/
|   +-- testdevice/
|   +-- vectors/          language-neutral conformance vectors, §24.1
|
+-- examples/
    +-- query/
    +-- server/
```

### 5.1 TinyGo dependency boundary

TinyGo firmware shall import:

```text
github.com/gotmc/hislip/protocol
github.com/gotmc/hislip/server
```

It shall **not** import the desktop client package.

The `protocol` and `server` packages shall stay inside a TinyGo-friendly subset:

- `context`
- `errors`
- `io`
- `sync` only where TinyGo behavior is verified
- `time`
- simple slices and fixed arrays
- no `reflect`
- no `encoding/gob`
- no dependency on OS syscalls
- no mandatory `net`
- no unsafe pointer tricks
- no large per-message allocation

The wire codec should preferably encode/decode fixed header fields explicitly rather than using reflection-based serialization.

Note on `net`: the TinyGo W5500 driver is a `netdev` implementation, so TinyGo's `net` package and `net.Conn` are in fact available on this target. The `Stream` abstraction of §7 is therefore retained for testability and for host/target symmetry, not because `net` is unavailable. Firmware MAY wrap `net.Conn`.

#### 5.1.1 Enforcing the boundary

Stating the boundary as a rule is not sufficient. The realistic failure is not that someone imports the client package deliberately; it is that `fmt.Errorf` appears in an error path eighteen months from now and the firmware build breaks, or silently grows.

[R-DOC-030] The repository MUST contain a test that walks the transitive import graph of the device-side packages and fails when it contains a package outside an allowlist. The allowlist is the TinyGo-friendly subset named above.

[R-DOC-031] The check MUST cover `protocol` and `server` and every package they import within this module, and MUST reject `fmt`, `net`, `os`, `reflect`, `encoding/gob`, `unsafe` and the `client` package by name so that the failure message is unambiguous.

[R-DOC-032] CI MUST additionally run a `tinygo build` of the device-side packages. The import-graph test catches the cause; the TinyGo build catches everything the allowlist did not anticipate.

[R-DOC-033] Adding a package to the allowlist MUST be a deliberate, reviewed change, and the justification MUST be recorded in the test file itself rather than in a commit message.

A nested Go module for the device-side packages, with its own `go.mod` and no external requires, would make the boundary structural rather than tested. That is a stronger guarantee at the cost of separate versioning and tooling friction. It is the fallback if the tested boundary proves insufficient in practice; it is not the starting point.

### 5.2 Toolchain and firmware build profile

The firmware depends on TinyGo runtime behaviour that is version-sensitive, so the toolchain is part of the design, not an environment detail.

[R-FW-010] The firmware build MUST pin an exact TinyGo version. The pinned version MUST be recorded in the firmware repository and reported by the `show config` diagnostic (§52).

[R-FW-011] The firmware MUST compile and pass its functional tests under the target default scheduler (`-scheduler=tasks`, single core, cooperative). Multi-core operation MUST remain an optimization that can be withdrawn without functional change.

[R-FW-012] When the multi-core scheduler is enabled, the build MUST use `-scheduler=cores` and a TinyGo version in which the known cores-scheduler defects are fixed. At the time of writing that means 0.42.0 or later, which contains fixes for cores-scheduler timer starvation, the leaking-GC build, the all-core GC stop, and an RP2 USB CDC transmit race.

[R-FW-013] The firmware MUST NOT assume goroutine-to-core affinity. See §23.1.

[R-FW-014] The `go` directive in `go.mod` MUST NOT exceed the Go language version supported by the pinned TinyGo release. TinyGo 0.42.0 supports Go 1.27, so a directive of `go 1.25.0` is compatible. This constraint is easy to violate accidentally, because raising the directive breaks only the firmware build while the host build continues to succeed.

Release cadence is approximately one TinyGo minor release per four months, and patch releases are rare. The project MUST therefore be able to build from a pinned upstream commit or carry a local patch, and MUST NOT plan around an unreleased fix arriving on a schedule.

Rationale for pinning rather than tracking latest: an instrument adapter has a long service life and a slow validation cycle. The cost of re-qualifying the firmware against a new toolchain exceeds the benefit of routine upgrades.

### 5.3 Repository placement within the GoTMC ecosystem

HiSLIP is delivered as a **new standalone module, `github.com/gotmc/hislip`**, a sibling of the existing transports.

The GoTMC convention is one module per transport, each carrying its own `visa.go` for resource-string parsing, with `gotmc/visa` acting as the resource manager that imports them:

```text
gotmc/visa      resource manager; requires asrl, lxi, usbtmc
gotmc/asrl      serial
gotmc/lxi       TCPIP SOCKET, raw TCP
gotmc/usbtmc    USB
gotmc/prologix  Prologix GPIB controllers
gotmc/hislip    TCPIP INSTR, HiSLIP            <- new
```

**Why not inside `gotmc/lxi`.** HiSLIP is formally an LXI Extended Function, so the name is semantically tempting, but that repository is the raw-socket transport rather than a general LXI library. Its resource parser accepts only the `SOCKET` resource class and returns a dedicated error for anything else, so admitting HiSLIP would change its published contract. It is also a two-file module, and HiSLIP with protocol, client, server and extension packages would dominate it. Finally, `gotmc/visa` depends on `gotmc/lxi`, so coupling a rapidly changing HiSLIP implementation to it would force version churn through the resource manager during Milestones 0 to 7.

**Why not inside `gotmc/visa`.** The resource manager selects drivers; it does not implement transports. Putting a protocol implementation there inverts the existing dependency direction.

**The decisive argument is the dependency graph.** The device-side packages must compile under TinyGo (§5.1). Owning the module means owning every dependency the firmware can see. A shared module invites a desktop-only import into a package the firmware compiles, which §5.1.1 exists to prevent.

[R-DOC-040] The HiSLIP implementation MUST be a standalone Go module at `github.com/gotmc/hislip`.

[R-DOC-041] `gotmc/hislip` MUST provide its own `visa.go` following the structure of the existing transports, parsing the `INSTR` resource class with a HiSLIP subaddress and the optional explicit-port form, so that `gotmc/visa` can select on resource class without any change to `gotmc/lxi`.

[R-DOC-042] Client-side mDNS discovery, if implemented, belongs in `gotmc/lxi` because it is transport-agnostic LXI discovery. The firmware's mDNS responder (§44) is separate code with different constraints and MUST NOT share an implementation with it.

[R-DOC-043] The optional capability interfaces of §19 are a change to `gotmc/ivi`, not part of this module.

[R-DOC-044] The firmware is a separate repository (§34) and is product-specific rather than a library. It MUST NOT be placed inside `gotmc/hislip`.

**Upstream dependency.** Creating a repository in the `gotmc` organisation requires the organisation owner. Until that happens, development proceeds in a local repository using the final module path `github.com/gotmc/hislip`, which requires no upstream repository to exist. This is recorded as R19 in §67.

[R-DOC-045] Local development MUST use the final module path from the outset, so that no import rewriting is needed when the upstream repository is created.

[R-DOC-046] The repository MUST follow the conventions of the existing GoTMC modules — MIT licence with the project's own copyright line, the four-line file header comment, `.golangci.yaml`, and a `Justfile` — so that an upstream contribution needs no restructuring.

---

## 6. Core wire API

Recommended header representation:

```go
type MessageType uint8

type Header struct {
    Type       MessageType
    Control    uint8
    Parameter  uint32
    Length     uint64
}

const HeaderSize = 16
```

Do not store `"HS"` in every structure instance. It is invariant.

Suggested functions:

```go
func ReadHeader(r io.Reader) (Header, error)
func WriteHeader(w io.Writer, h Header) error

func ReadPayload(r io.Reader, n uint64, dst []byte, consume func([]byte) error) error
func WriteMessage(w io.Writer, h Header, payload []byte) error
```

`ReadHeader` must:

1. read exactly 16 bytes;
2. verify `H`, `S`;
3. parse all multibyte fields in network byte order;
4. validate message type for the current protocol version;
5. validate channel legality at the session layer;
6. reject or drain oversized messages correctly.

The codec must never do:

```go
payload := make([]byte, header.Length)
```

because `Length` is 64-bit and controlled by the peer.

Payloads are streamed through a fixed scratch buffer, e.g. 512–2048 bytes on TinyGo.

---

## 7. Stream abstraction for the server

The server core should not require `net.Conn`.

```go
type Stream interface {
    Read([]byte) (int, error)
    Write([]byte) (int, error)
    Close() error
}
```

Optional capability:

```go
type DeadlineStream interface {
    Stream
    SetDeadline(time.Time) error
}
```

A host Go adapter wraps `net.Conn`.

A TinyGo adapter wraps the W5500 TCP socket implementation.

This means the same HiSLIP session state machine can be used on:

```text
net.TCPConn          on desktop/server Go
W5500 connection     on TinyGo
net.Pipe             in unit tests
memory test stream   in deterministic protocol tests
```

---

## 8. Device-facing server API

The HiSLIP server must not know that the device is GPIB.

Recommended minimum interface:

```go
type Device interface {
    Write(ctx context.Context, p []byte, end bool) error
    Read(ctx context.Context, p []byte) (n int, end bool, err error)

    Clear(ctx context.Context) error
    Trigger(ctx context.Context) error
    ReadStatusByte(ctx context.Context) (byte, error)
    RemoteLocal(ctx context.Context, mode RemoteLocalMode) error
}
```

### 8.1 Write semantics

`end=false` means that more bytes belong to the current input message.

`end=true` means end-of-message/END has been delivered by HiSLIP.

For a GPIB backend:

```text
HiSLIP Data       -> GPIB bytes, no EOI on final byte
HiSLIP DataEND    -> final GPIB byte transferred with EOI asserted
```

When DataEND contains zero payload, it still terminates the logical input message.

### 8.2 Read semantics

`Read` returns data plus an `end` flag.

The server maps:

```text
end=false -> HiSLIP Data
end=true  -> HiSLIP DataEND
```

The GPIB backend sets `end=true` when:

- EOI is observed with the final received byte; or
- a configured EOS terminator is used and accepted as message termination.

### 8.3 Optional service-request source

Recommended optional interface:

```go
type ServiceRequestSource interface {
    ServiceRequests() <-chan byte
}
```

The byte is the status byte to report in `AsyncServiceRequest`.

For TinyGo, a fixed-capacity channel of 1 is sufficient. Repeated SRQ events while one event is pending should coalesce rather than allocate unbounded queue entries.

### 8.4 Optional reset/diagnostic interfaces

```go
type DeviceResetter interface {
    InterfaceClear(ctx context.Context) error
}

type DeviceInfo interface {
    DeviceName() string
}
```

These are not required by HiSLIP but are useful for the adapter.

---

## 9. HiSLIP session model

One logical HiSLIP session contains:

```go
type Session struct {
    ID              uint16
    Version         protocol.Version
    Mode            Mode
    Sync            Stream
    Async           Stream

    LastRxMessageID uint32
    LastTxMessageID uint32

    MaxRxMessage    uint64
    MaxTxMessage    uint64

    lockState       ...
    clearState      ...
    rqsLatched      bool
    srqStatus       byte
}
```

A session becomes usable only after both TCP channels have been associated by session ID.

The server must reject normal operations before the asynchronous channel is established.

The firmware profile supports **one active HiSLIP logical session** initially. The library itself must not impose that restriction.

---

## 10. Connection initialization

Server behavior:

```text
TCP connection #1
    |
    +-- Initialize
            |
            +-- allocate SessionID
            +-- validate subaddress
            +-- negotiate version
            +-- advertise/prefer Synchronized Mode
            +-- InitializeResponse

TCP connection #2
    |
    +-- AsyncInitialize(SessionID)
            |
            +-- associate with session
            +-- AsyncInitializeResponse
            +-- session READY
```

For the PoE-to-GPIB device profile:

```go
const SupportsSynchronized = true
const SupportsOverlap = false
```

The server shall indicate Synchronized Mode preference/capability during initialization. If a client later proposes Overlap Mode as part of Device Clear negotiation, the bridge shall decline that mode and return/retain Synchronized Mode.

Default subaddress:

```text
hislip0
```

A null subaddress should also select the only/default device.

Other subaddresses shall return an initialization error.

---

## 11. Mandatory Synchronized Mode for the GPIB device profile

The PoE-to-GPIB firmware **shall operate exclusively in HiSLIP Synchronized Mode**.

This is a normative compatibility requirement, not merely an implementation shortcut.

A physical IEEE-488 instrument is a serialized message-based endpoint:

```text
program message N
      |
      v
GPIB write
      |
      v
instrument execution
      |
      v
GPIB read / response N
      |
      v
program message N+1
```

The bridge shall preserve this ordering at the HiSLIP boundary.

### 11.1 Why Overlap Mode is not exposed

Overlap Mode would require the bridge to create concurrency that generally does not exist in the underlying GPIB instrument:

```text
HiSLIP query A ----\
HiSLIP query B -----+--> bridge-side queues/buffers --> one serialized GPIB device
HiSLIP query C ----/
```

That would require the bridge to invent command/response queueing, response ownership and buffering rules on behalf of a legacy instrument. It adds ambiguity and memory pressure without improving compatibility.

Therefore the GPIB device profile:

```text
MUST support:      Synchronized Mode
MUST NOT support:  Overlap Mode
```

The reusable host-side `gotmc/hislip` client remains a general HiSLIP client and therefore supports both modes.

### 11.2 MessageID correlation

In Synchronized Mode, the bridge shall keep a direct association between the completed client program message and any response generated from it.

Conceptually:

```text
HiSLIP MessageID N
       |
       +--> GPIB WRITE
       |
       +--> optional GPIB READ
       |
       +--> HiSLIP Data/DataEND response associated with N
```

This association must survive internal chunking. A large GPIB response may be returned as multiple HiSLIP Data packets followed by one DataEND, while retaining the correct synchronized request/response ownership.

### 11.3 Interrupted query/response behavior

The state machine shall detect an attempt to begin a new synchronized program message while a previous response remains pending.

It must not silently merge, discard or reorder response data.

The state machine shall model at least:

```text
Idle
ReceivingInput
InputComplete
ResponsePending
SendingResponse
Interrupted
Clearing
Closed
```

An interrupted-response condition must transition through the HiSLIP-defined recovery behavior and remain observable as a protocol condition rather than being hidden inside the GPIB backend.

#### 11.3.1 RMT-expected and RMT-delivered

This mechanism is the substance of Synchronized Mode and was missing from Revision 2. It is mandatory for a Synchronized Mode server.

The RMT-delivered flag is carried in **control code bit 0** of client-to-server `Data`, `DataEnd`, `Trigger`, `AsyncStatusQuery`, `AsyncStartTLS` and `AsyncEndTLS` messages. The client sets it to 1 on the first such message after it has delivered a response message terminator to its application layer, and only once per terminator.

```text
bit 0 = 0   RMT was not delivered
bit 0 = 1   RMT was delivered
```

The server maintains one boolean per session.

[R-SYNC-010] The server MUST set RMT-expected true whenever it sends a `DataEnd` message, that is whenever it delivers an RMT.

[R-SYNC-011] The server MUST clear RMT-expected when it receives `AsyncStatusQuery`, `AsyncStartTLS` or `AsyncEndTLS` with RMT-delivered set true.

[R-SYNC-012] On receiving `Data`, `DataEnd` or `Trigger`, when RMT-expected and RMT-delivered are both true or both false, the server MUST clear RMT-expected.

[R-SYNC-013] On receiving `Data`, `DataEnd` or `Trigger`, when RMT-expected and RMT-delivered differ, the server MUST declare an interrupted error. This error is reported only through the server's internal error mechanism. No indication is sent to the client.

R-SYNC-013 is deliberately silent on the wire. Implementers routinely assume every interrupted condition produces an `Interrupted` message; that is true only for the case in §11.3.2.

[R-SYNC-014] The interrupted error of R-SYNC-013 MUST increment a diagnostic counter (§52) so that a misbehaving client is observable in the field without a protocol analyser.

#### 11.3.2 The Interrupted transaction

The second interrupted case arises when the server's application layer offers a response terminator while unread client input is still queued.

[R-SYNC-020] When the server application layer requests that a response message terminator be sent, the server MUST first verify that no data is in the server input queue.

[R-SYNC-021] If input is queued, the server MUST declare an interrupted error and MUST:

1. report the interrupted error through its internal error mechanism;
2. clear the response just received from the application layer, and any other messages buffered for transmission to the client;
3. send the Interrupted transaction, comprising **both** the `Interrupted` message on the synchronous channel and the `AsyncInterrupted` message on the asynchronous channel.

[R-SYNC-022] Both messages MUST be sent. Sending only one leaves a conforming client permanently stalled, because a client that sees `Interrupted` first must not send further messages until `AsyncInterrupted` arrives, and a client that sees `AsyncInterrupted` first must discard server data until `Interrupted` arrives.

[R-SYNC-023] After interrupted error processing completes, the server MUST resume normal operation without closing the session.

For the GPIB bridge, "the server application layer offers a response terminator" corresponds to the point at which the GPIB engine has completed a read with EOI or an accepted EOS. The check of R-SYNC-020 therefore happens in the server layer, after the GPIB operation completes and before the terminating `DataEnd` is emitted.

[R-SYNC-024] Response bytes already removed from the instrument MUST be discarded, not retained for a later transaction, when R-SYNC-021 applies. The bridge MUST NOT attempt to re-deliver them, because their MessageID association is void.

The transaction is two messages, both from the server, in this order:

```text
step  sender  message            channel  control  parameter
1     server  AsyncInterrupted   async    0        MessageID
2     server  Interrupted        sync     0        MessageID
```

[R-SYNC-025] Both messages MUST carry, in the message parameter field, the MessageID of the `Data`, `DataEnd` or `Trigger` message that interrupted the server's response. This is the interrupting message, not the message whose response was discarded. Revision 2 did not state that these messages carry a MessageID at all.

[R-SYNC-026] `AsyncInterrupted` SHOULD be sent before `Interrupted`, per the order of IVI-6.1 Table 30. A conforming client tolerates either order — it has defined behaviour for detecting each first — but following the table avoids exercising the less-travelled path in third-party clients.

[R-SYNC-027] The control code of both messages MUST be zero. Neither carries a status or feature value.

### 11.4 MAV semantics

MAV handling in the bridge shall be defined exclusively against Synchronized Mode.

The bridge must maintain its own logical output state because it may have already removed bytes from the physical GPIB instrument into an adapter buffer.

Therefore the externally reported MAV bit reflects:

> whether response data for the active synchronized HiSLIP transaction is available/pending at the HiSLIP server boundary,

not merely whether the legacy instrument currently asserts MAV internally.

#### 11.4.1 Normative MAV computation

MAV is bit position 4 of the status byte. Revision 2 described MAV as a function of bridge buffer occupancy. That is incomplete: in Synchronized Mode, MAV is primarily a function of MessageID equality.

[R-SYNC-030] On receiving `AsyncStatusQuery`, the server MUST compare the MessageID it carries with the MessageID of the last `Data`, `DataEnd` or `Trigger` it received. If they are not equal, the server MUST report MAV false, regardless of whether response bytes are held in the bridge.

[R-SYNC-031] The server MUST set MAV false when the client indicates RMT-delivered in any RMT-delivered flag-carrying message.

[R-SYNC-032] Only when R-SYNC-030 and R-SYNC-031 do not force MAV false does the bridge's own output state determine MAV, per §16.3.

The inequality case of R-SYNC-030 means the server either has no data to deliver or is about to be interrupted by a pending synchronous message. The server is not required to detect and report that interrupted error in the status response; normal interrupted processing (§11.3.2) will report it subsequently.

[R-SYNC-033] Because MAV depends on the MessageID of the status query, MAV MUST be computed in the server layer. The `Device` interface of §8 deliberately carries no MessageID, and a GPIB backend MUST NOT be asked to compute MAV.

Consequence worth recording: the status byte returned by `viReadSTB` and the value an instrument returns to a `*STB?` query travelling through the normal data path can legitimately differ in bit 4, because the former is subject to R-SYNC-030 and the latter is not. This is inherent to bridging and MUST be documented in the product manual rather than smoothed over.

### 11.5 Device Clear mode negotiation

Device Clear is also the point at which HiSLIP peers may negotiate mode.

For the PoE-to-GPIB profile:

```text
client proposes Synchronized  -> accept
client proposes Overlap       -> decline; remain Synchronized
```

After Device Clear completes, the session remains in Synchronized Mode.

No firmware configuration option shall enable Overlap Mode on this product profile.

---

## 12. Locks

HiSLIP locking belongs in the server layer, not the GPIB driver.

Implement:

- exclusive lock;
- shared lock string;
- lock release;
- lock info;
- timeout handling.

For the single-client TinyGo profile this appears unnecessary, but standards-compatible VISA clients may still issue lock operations. Therefore the server shall implement the protocol even when only one application session is permitted.

The firmware can use a simplified lock manager internally while preserving protocol-visible behavior.

---

## 13. Device Clear

The server must implement the full HiSLIP device-clear transaction, including the asynchronous acknowledgement and synchronous completion handshake.

For the PoE-to-GPIB profile, Device Clear must also enforce the mode policy:

```text
post-clear mode = Synchronized
```

If the peer proposes Overlap Mode, the bridge declines it and continues in Synchronized Mode.

After protocol buffers are cleared, the server calls:

```go
device.Clear(ctx)
```

The GPIB backend maps that to **Selected Device Clear (SDC)** for the configured instrument, not blindly to the universal DCL command.

The adapter may expose a separate maintenance function for whole-interface DCL/IFC.

Device Clear must also:

- cancel a pending GPIB read;
- discard buffered response bytes;
- reset HiSLIP message-state tracking;
- clear pending query/read state;
- leave the TCP session usable.

### 13.1 Message sequence and feature bitmap

```text
step  sender  message                        channel  control code
1     client  AsyncDeviceClear               async    0
2     server  AsyncDeviceClearAcknowledge    async    feature preference
3     client  DeviceClearComplete            sync     feature request
4     server  DeviceClearAcknowledge         sync     feature setting   (see §4.4)
```

Server obligations, in order, on receiving `AsyncDeviceClear`:

1. complete any partially complete asynchronous transactions without waiting for timeouts;
2. send `AsyncDeviceClearAcknowledge` carrying the preferred feature bitmap;
3. finish sending any partially sent messages to the client, with normal behaviour and without waiting for timeouts;
4. abandon any buffered unsent transactions;
5. clear any well-formed messages already received on the synchronous channel;
6. accept and discard subsequent synchronous messages until `DeviceClearComplete` is found, while continuing to require well-formed messages;
7. send `DeviceClearAcknowledge` carrying the agreed feature bitmap;
8. resume normal operation.

The feature bitmap occupies the control code of the three messages that carry it:

```text
bit 0   Overlapped        false = Synchronized Mode, true = Overlap Mode
bit 1   Encryption mode   true = encryption mandatory
bit 2   Initial encryption true = secure connection must follow initialization
```

[R-SRV-010] The server MUST propose its preferred features in `AsyncDeviceClearAcknowledge` and MUST support every capability it proposes.

[R-SRV-011] The server MUST accept the value the client requests in `DeviceClearComplete` when it is capable of supporting it, and MUST return the resulting setting in `DeviceClearAcknowledge`.

[R-SRV-012] The PoE-to-GPIB profile MUST hold feature bit 0 at false in both `AsyncDeviceClearAcknowledge` and `DeviceClearAcknowledge`, irrespective of the client's request. This is the entire implementation of the Overlap Mode prohibition.

[R-SRV-013] The server MUST reset its MessageID tracking to `0xffffff00` when the transaction completes (§4.2).

[R-SRV-014] On encountering a poorly formed message at any point during device clear, the server MUST send `FatalError` with code 1 and perform fatal-error processing.

[R-SRV-015] If the peer does not respond in a timely fashion during device clear, the server MUST send `FatalError` and perform fatal-error processing. IVI-6.1 gives 40 to 120 seconds as reasonable values for this determination. The firmware MUST use a value in that range and MUST make it a named constant rather than an inline literal.

[R-SRV-016] Steps 1 to 7 MUST complete even when a GPIB operation is in progress. The device clear path therefore depends on the asynchronous preemption mechanism of §47.1.

R-SRV-016 is the requirement that makes device clear useful. A device clear that cannot interrupt a stuck instrument is the case the operator most needs it for.

---

## 14. Trigger

HiSLIP `Trigger` maps directly to GPIB **GET (Group Execute Trigger)** targeted at the configured instrument.

The GPIB backend sequence is:

```text
ATN asserted
UNL
listener address of target
GET
ATN released
```

Exact command sequencing should be encapsulated in the GPIB controller package and unit-tested against expected bus-command bytes.

---

## 15. Remote/local mapping

HiSLIP remote/local control maps to the GPIB system-controller operations based on:

- REN
- listener addressing
- GTL
- LLO

Do not emulate remote/local by sending SCPI strings such as `SYST:REM` or `SYST:LOC`; many legacy GPIB instruments do not implement those commands.

The GPIB backend must preserve the semantics of the HiSLIP control code as closely as possible.

---

## 16. Status byte and SRQ

### 16.1 Status query

`AsyncStatusQuery` corresponds to VISA `viReadSTB`.

For the GPIB backend, this is implemented with a GPIB serial poll:

```text
ATN
SPE
address instrument to talk
read one status byte
SPD
restore bus state
```

### 16.2 SRQ handling

Physical GPIB SRQ is level-signaled. It does not directly contain the status byte.

When SRQ is detected:

1. the GPIB engine performs a serial poll;
2. the returned status byte is cached;
3. firmware emits an internal service-request event;
4. the HiSLIP server sends `AsyncServiceRequest` with the cached byte.

Because serial poll may clear the physical instrument's RQS indication, the adapter must maintain its own HiSLIP-side RQS latch until the HiSLIP client performs `AsyncStatusQuery`.

This preserves the externally visible HiSLIP behavior even though the bridge had to serial-poll the legacy instrument to discover why SRQ was asserted.

### 16.3 MAV

MAV is subtle in a bridge.

The HiSLIP server must compute HiSLIP-side MAV from its output state, because the physical instrument may already have been read into an adapter buffer.

Therefore:

```text
returned STB =
    backend status bits
    with MAV bit adjusted to represent HiSLIP response availability
```

For the PoE-to-GPIB device profile, only the Synchronized Mode rule applies. The generic `gotmc/hislip` library may still implement both Synchronized and Overlap MAV semantics for use with non-bridge servers.

### 16.4 Wire-level status and service request

```text
client  AsyncStatusQuery      <RMT-delivered><MessageID><0>
server  AsyncStatusResponse   <status><0><0>
server  AsyncServiceRequest   <status><0><0>      not acknowledged
```

In all three the 8-bit value travels in the **control code** field, not the payload.

[R-SRV-020] The server MUST read both fields of `AsyncStatusQuery`: the RMT-delivered flag from the control code (§11.3.1) and the MessageID from the message parameter (§11.4.1). Revision 2 treated this message as parameterless; it is not.

[R-SRV-021] `AsyncServiceRequest` MUST NOT be acknowledged and MUST NOT be retried.

[R-SRV-022] The server MUST NOT clear any bit of the status register it reports in `AsyncServiceRequest`, including RQS. Per IVI-6.1 section 6.13, the client clears RQS by performing `AsyncStatusQuery`.

[R-SRV-023] The bridge MUST NOT send a further `AsyncServiceRequest` until the client has performed `AsyncStatusQuery`. Combined with §16.2, this is what the bridge-side RQS latch implements, and it also bounds service request traffic to one message per client acknowledgement.

R-SRV-023 gives the coalescing rule of §8.3 a normative basis. It also means a single-entry SRQ channel is sufficient by specification and not merely by memory budget.

Fidelity limit worth recording: because the bridge must serial-poll the instrument to discover why SRQ was asserted, and because SRQ is level-signalled, two instrument events occurring before the client issues `AsyncStatusQuery` collapse into one reported status byte. The second status byte is lost. This is inherent to bridging a level-signalled bus onto a message protocol and MUST be documented in the product manual.

---

## 17. The generic GPIB-read problem

This remains the most important semantic issue in the complete bridge even after Synchronized Mode is made mandatory.

Synchronized Mode solves request/response ownership and interrupted-response compatibility, but it does **not** by itself tell a generic bridge when the application has called a low-level VISA read.

A HiSLIP client does not send a standard protocol message saying:

> "the application has now called viRead(); please address the GPIB instrument as talker."

A native HiSLIP instrument already knows when its command parser has generated response data.

A generic GPIB gateway does not.

For SCPI, a bridge can infer that a completed program message containing a query requires a response. That does not work reliably for all pre-SCPI GPIB instruments.

Therefore the adapter shall support two read modes.

### 17.1 Standard HiSLIP compatibility mode

For normal third-party VISA software:

- parse completed SCPI/IEEE-488.2 program messages sufficiently to detect query program headers;
- when a query is complete, schedule a GPIB read;
- buffer/stream the resulting GPIB response into HiSLIP Data/DataEND.

The parser need not understand the command tree. It only needs lexical awareness sufficient to distinguish a real query marker from `?` inside quoted or binary data.

This mode works with normal SCPI instruments and normal NI/Keysight/PyVISA clients.

### 17.2 GoTMC explicit-read extension

For GoTMC-to-GoTMC operation, define an optional VendorSpecific HiSLIP message that explicitly expresses a read request.

Conceptual payload:

```go
type ReadRequest struct {
    MaxBytes uint32
    Flags    uint16
    TermChar byte
}
```

Possible flags:

```text
USE_EOI
USE_TERMCHAR
SUPPRESS_TERMCHAR
```

The extension is only used after positive capability negotiation between GoTMC client and server.

It shall never be sent unconditionally to third-party instruments.

This allows GoTMC to support old instruments whose read operation cannot be inferred from SCPI syntax.

The normal standardized HiSLIP path remains fully standards-compliant; the extension is a separate optional profile.

The project should obtain an official two-character HiSLIP/VXIplug&play vendor ID from the IVI Foundation before assigning production vendor-specific semantics.

### 17.3 When a response is expected but none arrives

This is the most frequent failure in any GPIB gateway: a mistyped query, a query the instrument rejected, or a command the heuristic misclassified. Revision 2 did not define it. Undefined behaviour here strands the client until its own timeout expires, which is the symptom users report as "the adapter hangs".

The bridge cannot honestly claim a response exists, and it cannot stay silent.

[R-DEV-010] When the read policy has scheduled a GPIB read and the GPIB response timeout expires with zero bytes received, the server MUST, in this order:

1. send a non-fatal `Error` message with a device-defined code from §21.1 and a short ASCII description;
2. terminate the synchronized transaction by sending a zero-length `DataEnd` carrying the MessageID of the originating client message;
3. increment the `query without response` diagnostic counter;
4. restore a safe bus state per §49 and remain in the session.

Step 2 is what allows a conforming client's read to complete. Step 1 is what makes the cause visible rather than presenting an empty reading as valid data. A client that ignores `Error` still makes progress; a client that surfaces it gives the operator the real diagnosis.

[R-DEV-011] When bytes were received before the timeout, the bridge MUST deliver them and MUST terminate with `DataEnd`. A partial response MUST NOT be discarded, and the truncation MUST be counted and logged.

[R-DEV-012] The bridge MUST NOT reboot, reset the interface, or drop the TCP session as part of this recovery (§49).

The inverse error, a query the detector classified as `NoResponseExpected` when the instrument did generate a response, leaves the instrument holding data that will be misattributed to the next query. Two mitigations are available and both are policy-controlled:

[R-DEV-013] The firmware MUST provide a configurable stale-response flush. When enabled, before addressing the instrument as listener for a new program message the bridge performs a serial poll, and if MAV is set it reads and discards the pending response, counting the event.

[R-DEV-014] The `GOTMC_EXPLICIT` and `ALWAYS_READ` read policies (§48) avoid the misclassification entirely and MUST be documented as the remedy for instruments where the heuristic proves unreliable.

### 17.4 Vendor extension wire format

Vendor-specific message types are not namespaced by vendor ID, so the extension is only safe after an explicit handshake. A third-party server that does not implement it answers with non-fatal `Error` code 3, `Unrecognized Vendor Defined Message`, which is the negative result the client needs.

One message type is consumed, and the sub-operation travels in the control code.

```text
type 128 (0x80)   GoTMC extension envelope

control code   name                  channel   direction
0              ExtHello              async     client -> server
1              ExtHelloResponse      async     server -> client
2              ExtReadRequest        sync      client -> server
```

`ExtHello` payload, 8 bytes, big-endian:

```text
offset size field
0      4    magic, ASCII "GTMC"
4      2    minimum extension version supported by client
6      2    maximum extension version supported by client
```

`ExtHelloResponse` payload, 12 bytes, big-endian:

```text
offset size field
0      4    magic, ASCII "GTMC"
4      2    negotiated extension version
6      2    reserved, MUST be zero
8      4    feature bitmap; bit 0 = ExtReadRequest supported
```

`ExtReadRequest` payload, 8 bytes, big-endian:

```text
offset size field
0      1    version, MUST equal the negotiated version
1      1    flags
            bit 0  USE_EOI
            bit 1  USE_TERMCHAR
            bit 2  SUPPRESS_TERMCHAR
2      1    termination character, valid only when USE_TERMCHAR
3      1    reserved, MUST be zero
4      4    maximum bytes to read; zero means server policy decides
```

[R-PROTO-050] `ExtReadRequest` MUST be sent on the synchronous channel and MUST carry a MessageID in the message parameter field, allocated from the same counter as `Data`, `DataEnd` and `Trigger` (§4.2). Without this, the RMT and MAV logic of §11.3.1 and §11.4.1 cannot correlate the resulting response.

[R-PROTO-051] The response to `ExtReadRequest` MUST be ordinary `Data` and `DataEnd` messages. The extension introduces no new response framing.

[R-PROTO-052] The client MUST NOT send `ExtReadRequest` before receiving `ExtHelloResponse` with feature bit 0 set.

[R-PROTO-053] A server that does not implement the extension MUST answer `ExtHello` with non-fatal `Error` code 3. The client MUST treat that as a permanent negative for the session and MUST fall back to the standard path of §17.1.

[R-PROTO-054] Reserved fields MUST be zero on transmission and MUST be ignored on receipt, so that a later version can use them without a new message type.

[R-PROTO-055] Until an official vendor ID is assigned by the IVI Foundation, the extension MUST be disabled by default in released firmware and MUST be enabled only by explicit configuration. The `GTMC` magic is the interim guard against colliding with another vendor's use of type 128.

---

## 18. Client API

The Go client should feel similar to other GoTMC transports.

```go
dev, err := client.Dial(ctx, client.Config{
    Address:    "192.168.1.42:4880",
    SubAddress: "hislip0",
})
```

High-level API:

```go
Command(ctx, format string, args ...any) error
Query(ctx, command string) (string, error)
ReadBinary(ctx, p []byte) (int, error)
WriteBinary(ctx, p []byte) (int, error)

Clear(ctx) error
Trigger(ctx) error
ReadStatusByte(ctx) (byte, error)
RemoteLocal(ctx, mode RemoteLocalMode) error

Lock(ctx, opts LockOptions) error
Unlock(ctx) error
Close() error
```

The type should satisfy GoTMC IVI's existing transport contract:

```go
type Transport interface {
    Command(context.Context, string, ...any) error
    Query(context.Context, string) (string, error)
    ReadBinary(context.Context, []byte) (int, error)
    WriteBinary(context.Context, []byte) (int, error)
}
```

---

## 19. Add optional capability interfaces to `gotmc/ivi`

Do **not** enlarge the mandatory `ivi.Transport` interface.

Add narrow optional interfaces:

```go
type Clearer interface {
    DeviceClear(context.Context) error
}

type Triggerer interface {
    Trigger(context.Context) error
}

type StatusByteReader interface {
    ReadStatusByte(context.Context) (byte, error)
}

type RemoteLocalController interface {
    RemoteLocal(context.Context, RemoteLocalMode) error
}
```

USBTMC/USB488, HiSLIP and future native GPIB transports can implement the same capability interfaces.

SCPI-only raw sockets simply do not implement them.

This avoids forcing all transports to pretend they support operations they cannot represent.

---

## 20. `gotmc/visa` integration

The current GoTMC VISA resource manager already selects registered drivers by interface type. HiSLIP should fit into the TCPIP driver without changing IVI drivers.

Resource parser requirements:

```text
TCPIP0::host::hislip0::INSTR
TCPIP0::host::hislip0,4880::INSTR
TCPIP0::host::5025::SOCKET
```

The TCPIP VISA driver chooses:

```text
...::SOCKET             -> gotmc/lxi/raw TCP implementation
...::hislip...::INSTR   -> gotmc/hislip client
```

A driver registration design remains preferable to a hard-coded dependency, preserving the existing GoTMC "import only what you use" model.

### 20.1 Dispatch mechanism

`gotmc/lxi` parses only the `SOCKET` resource class and returns a dedicated error for anything else, so dispatch is by resource class and requires no change to that module.

```text
TCPIP0::host::5025::SOCKET            -> gotmc/lxi     existing parser
TCPIP0::host::hislip0::INSTR          -> gotmc/hislip  new parser
TCPIP0::host::hislip0,4880::INSTR     -> gotmc/hislip  explicit port
TCPIP0::host::INSTR                   -> gotmc/hislip  null subaddress, §10
```

[R-SRV-060] `gotmc/hislip` MUST expose a resource parser mirroring the structure used by the other transports: a `VisaResource` type with accessors, sentinel errors for each failure mode, and a canonical `String()` form.

[R-SRV-061] The parser MUST accept the `hislip<n>` subaddress, the null subaddress, and the `subaddress,port` form. It MUST default to TCP 4880 when no port is given.

[R-SRV-062] The parser MUST reject the `SOCKET` resource class with a distinct error, so that `gotmc/visa` can distinguish "this is not mine" from "this is malformed".

[R-SRV-063] `gotmc/visa` MUST select between `lxi` and `hislip` on resource class alone. It MUST NOT inspect the port number to guess the protocol, because 4880 is a convention rather than a requirement.

---

## 21. Error model

Public errors should be typed or sentinel-based:

```go
ErrInvalidHeader
ErrInvalidPrologue
ErrProtocolVersion
ErrInvalidMessage
ErrWrongChannel
ErrMessageTooLarge
ErrSessionNotReady
ErrInterrupted
ErrLocked
ErrTimeout
ErrUnsupported
```

Protocol error packets must preserve the distinction between:

- fatal synchronization/session errors;
- non-fatal unsupported or malformed operations;
- backend/device errors.

A backend GPIB timeout is normally an operation error, not automatically a fatal HiSLIP synchronization error.

### 21.1 Mapping to wire error codes

Revision 2 listed Go sentinels but no numeric codes. Conformance is defined on the numbers, so the mapping is normative.

```text
sentinel             wire result                             code
ErrInvalidPrologue   FatalError                              1
ErrInvalidHeader     FatalError                              1
ErrProtocolVersion   FatalError                              3
ErrSessionNotReady   FatalError                              2
ErrInvalidMessage    Error, non-fatal                        1
ErrWrongChannel      Error, non-fatal                        1    see note
ErrMessageTooLarge   Error, non-fatal                        4
ErrUnsupported       Error, non-fatal                        1 or 3
ErrInterrupted       Interrupted + AsyncInterrupted          n/a  §11.3.2
ErrLocked            AsyncLockResponse, failure              n/a  §12
ErrTimeout           Error, non-fatal, device-defined        128+ see below
```

Bridge-specific device-defined codes, from the 128–255 range that IVI-6.1 reserves for the device:

```text
128  GPIB handshake timeout
129  GPIB response timeout, no response to an expected query   §17.3
130  no listener detected on the GPIB bus
131  instrument did not respond to serial poll
132  read disposition could not be determined and policy is to fail
133  operation rejected while the interface is in recovery
```

[R-SRV-030] The firmware MUST use the codes above and MUST NOT emit a code in the reserved range 6–127.

[R-SRV-031] `ErrWrongChannel` MUST be reported as non-fatal so that a confused but recoverable client is not disconnected. The server MUST count these, and MUST escalate to `FatalError` code 0 after a configurable threshold, because a peer that repeatedly misuses channels is not going to recover.

[R-SRV-032] `ErrTimeout` arising from the GPIB backend MUST NOT be reported as a `FatalError`. A slow or absent instrument is an operation error and MUST leave the HiSLIP session usable.

[R-SRV-033] Every `FatalError` and `Error` emitted MUST increment its corresponding diagnostic counter (§52), separated into fatal and non-fatal totals.

---

## 22. Memory strategy

The host implementation may use larger buffers.

The TinyGo profile shall use fixed capacities:

```text
Header buffer:              16 bytes
Async payload scratch:     256 bytes minimum
Sync stream scratch:      1024 or 2048 bytes
SCPI lexical buffer:       configurable, preferably <= 2048 bytes
GPIB response ring:        4-16 KiB depending firmware build
```

Large binary transfers must stream.

After connection, the client should use the HiSLIP Maximum Message Size transaction. Firmware should advertise a conservative maximum synchronous packet payload such as 4096 or 8192 bytes while still allowing an arbitrarily larger logical instrument transfer through multiple Data packets.

[R-FW-130] No limit MAY be imposed on the total size of a logical instrument transfer in either direction. Only the per-packet payload is bounded. A waveform or screen image larger than any buffer in the adapter MUST transfer successfully.

[R-FW-131] Packet and buffer boundaries MUST NOT stall the pipeline. The GPIB engine MUST be able to continue transferring bytes while a previously filled buffer is being transmitted over the network, and the reverse for writes. Double buffering or an equivalent arrangement is required.

[R-FW-132] Buffer sizes MUST be chosen so that sustained throughput is limited by the bus, the link or the instrument, never by buffer turnaround. Compliance is demonstrated by R-FW-124 and R-FW-126.

[R-FW-133] The fixed capacities of this section are minima, not design ceilings. They MAY be increased within the memory budget of R-FW-105 if measurement shows a benefit, and such a change MUST trigger re-measurement per R-FW-121.

---

## 23. Concurrency model

Desktop Go client:

```text
sync-reader goroutine
async-reader goroutine
serialized sync writer
serialized async writer
request/session state
```

TinyGo server:

```text
accept/session manager
       |
       +-- sync RX
       +-- async RX
       +-- synchronized response pump
       +-- SRQ pump
       |
       v
serialized Device/GPIB command queue
```

The synchronized response pump may emit response data only for the currently active synchronized input transaction. It may not interleave response streams belonging to separate client program messages.

No goroutine may directly toggle GPIB pins except the GPIB worker/engine.

Ordering and priority between operations is governed by the channel a message arrived on, as specified in §47. Revision 2's looser statement that "asynchronous operations get priority over normal data operations" was the source of a defect and is replaced.

### 23.1 TinyGo runtime constraints

The firmware design must be correct under the runtime it actually has. The following are properties of the toolchain, not of the RP2350 silicon, and they are verified rather than assumed.

**Scheduling.** The RP2350 target defaults to `-scheduler=tasks`: a single-core, cooperatively scheduled runtime. A goroutine that neither performs I/O nor blocks will hold the only thread indefinitely. Multi-core operation exists but is opt-in via `-scheduler=cores` (§5.2).

**No affinity.** The multi-core scheduler uses a shared run queue and wakes an idle core when work appears. There is no API to pin a goroutine to a core, and Go semantics do not offer one. "Run the GPIB engine on core 1" is therefore not expressible.

**Garbage collection stops every core.** The collector pauses all cores before scanning stacks and globals. An allocation on the core running the HiSLIP session can therefore stall a GPIB operation running on the other core.

**Preemption is not coming.** Cooperative scheduling within a core is a stable property of this platform, not a temporary gap. The project MUST NOT design against a future release that introduces preemption.

The requirements that follow:

[R-FW-020] Every wait loop in the firmware MUST yield. A loop that polls a deadline MUST also perform a scheduler yield or a blocking operation on each iteration. The maximum uninterrupted busy-wait duration MUST be stated as a constant in the board package and MUST be verified by test.

[R-FW-021] The HiSLIP data path and the GPIB data path MUST perform zero heap allocation in steady state. All buffers MUST be pre-allocated at start-up per §22. This is a timing requirement, not only a memory requirement: no allocation means no collection cycle, and no collection cycle means no cross-core pause during a transfer.

[R-FW-022] No component MAY assume that two goroutines run on different cores, or that a given goroutine runs on a particular core.

[R-FW-023] The three-wire byte handshake MUST NOT depend on Go scheduling for its timing. It is implemented in PIO (§40).

[R-FW-024] Worst-case GC pause and scheduler wake-up latency MUST be measured on hardware, and the GPIB handshake and response timeouts of §49 MUST be set above them with margin. Until measured, the firmware MUST NOT ship with timeout values derived from bus specifications alone.

[R-FW-025] USB CDC diagnostic output MUST NOT block or stall other tasks when no host is attached (§52).

R-FW-024 exists because the failure mode is misleading: a GC pause that trips a handshake timeout is reported to the user as a bus fault, and the operator will look for a cable problem that does not exist.

Note on why this does not threaten correctness: the IEEE-488 three-wire handshake is fully interlocked and has no minimum transfer rate. A scheduler stall or GC pause can only slow a transfer, never corrupt it. The exposure is spurious timeouts and reduced throughput, which is why the mitigation is a measured timeout budget plus PIO, rather than a real-time operating system.

---

## 24. Testing strategy for `gotmc/hislip`

### 24.1 Wire tests

Golden byte sequences for every implemented message type.

Vectors are held as language-neutral data files in `internal/vectors/`, not as Go literals, so that the same corpus can validate a future implementation in another language and can be shared with the firmware test suite.

Test:

- exact 16-byte header;
- big-endian encoding;
- payload length handling;
- invalid prologue;
- truncated header;
- giant payload rejection;
- unknown type;
- reserved type in 39–127;
- wrong channel for every type in §4.1;
- message whose minimum version exceeds the negotiated version;
- MessageID sequence: initial `0xffffff00`, increment by two, wrap-around;
- MessageID reset after initialization and after device clear;
- `Data` carrying `0xffffffff` accepted without correlation failure;
- `AsyncStatusQuery` carrying `0xfffffefe` immediately after initialization and after device clear;
- RMT-delivered flag encoding in control code bit 0;
- vendor envelope: `ExtHello` accepted, unknown sub-opcode answered with `Error` code 3;
- every fatal and non-fatal error code of §4.3 encodes and decodes.

### 24.2 Session tests

Using in-memory streams:

```text
client <-> net.Pipe/teststream <-> server
```

Test:

- initialization pair;
- invalid SessionID;
- Data/DataEND;
- status query;
- clear;
- trigger;
- lock;
- interrupted behavior;
- SRQ.

### 24.3 Device tests

Mock `Device` records all operations.

Example assertion:

```text
client Trigger
     ->
server Trigger handler
     ->
mock Device.Trigger exactly once
```

### 24.4 Interoperability tests

Interoperability is the test that finds protocol misreadings. Unit tests confirm that the implementation does what its author intended; only an independent implementation confirms that the intention was right.

The project uses software implementations rather than a purchased instrument. No HiSLIP-capable instrument is available on the development bench, and the available software provides independent implementations on both sides of the protocol, which is what conformance actually requires.

**Independent servers, for testing the client**

```text
lxi-tools/libhislip     C library with both client and server APIs
Keysight PC software    Infiniium, FlexDCA and FlexOTO run a HiSLIP SCPI
                        server on port 4880; check licensing for offline use
luksan/hislip-server    Python HiSLIP server
```

**Independent clients, for testing the server**

```text
NI-VISA                       free
Keysight IO Libraries Suite   free; includes Connection Expert
pyvisa-py                     free, pure Python, implements HiSLIP
lxi-tools/libhislip           C client API
```

[R-DOC-060] The client MUST be tested against at least one independent third-party server implementation. It MUST NOT be validated solely against this project's own server.

[R-DOC-061] The server MUST be tested against at least two independent third-party client implementations. Two is the minimum because a single client's interpretation of an ambiguous requirement is indistinguishable from the requirement itself.

[R-DOC-062] R-PROTO-041, the confirmation of the `DeviceClearAcknowledge` channel defect, MUST be settled using two of the clients above.

[R-DOC-063] The version or commit of every third-party implementation used for a release's interoperability testing MUST be recorded in the validation report, because these are moving targets and a later failure needs a known baseline.

[R-DOC-064] Testing against a commercial HiSLIP instrument is deferred, not cancelled. What it would add over the software implementations is coverage of vendor-specific behaviour, real-world timing, and the mDNS discovery path as a shipping product implements it. It SHOULD be done before the server library is declared stable.

Note on why software is sufficient for now: the risk a physical instrument uniquely addresses is that a real vendor implementation deviates from the standard in ways software written from the same standard does not. That risk is real but it is reduced, not created, by first passing against three independent software implementations written by three unrelated authors.

### 24.5 Firmware tests

Run the same protocol conformance vectors:

- native Go on CI;
- TinyGo compile in CI;
- hardware-in-loop on RP2350 for release candidates.

---

# PART II — PoE-to-GPIB hardware

## 25. Hardware architecture

```text
                    RJ45
                      |
             Ethernet magnetics
                      |
             W5500-EVB-Pico2
                W5500 + RP2350
                      |
              SPI internal link
                      |
           +----------+-----------+
           |                      |
        RP2350 GPIO            USB-C
           |                 flash/debug
           |
      terminal-side logic
           |
    +------+-------+
    |              |
SN75160B        SN75161B
data bus       management bus
    |              |
    +------+-------+
           |
  IEEE-488 24-pin male
  right-angle PCB connector
           |
        Instrument


PoE pairs/centertaps
        |
   WIZPoE-P1
   isolated 5 V
        |
   +----+----------------+
   |                     |
W5500-EVB VSYS      SN75160/161 VCC
```

---

## 26. Main components

### 26.1 MCU/Ethernet module

**WIZnet W5500-EVB-Pico2**

Relevant features:

- RP2350
- dual Cortex-M33, up to 150 MHz
- 520 KiB SRAM
- W5500 hardwired IPv4 TCP/UDP engine
- 8 hardware sockets
- W5500 uses GPIO16–GPIO21 internally
- TinyGo supports the RP2350/Pico 2 target and RP2350 PIO

This module is preferred over the earlier ESP32 options because it gives a much cleaner TinyGo path.

### 26.2 PoE

**WIZPoE-P1**

- IEEE 802.3af PD
- Mode A and B
- isolated output
- nominal 5 V
- sufficient power margin for RP2350/W5500 + two GPIB transceivers

Feed the module into **VSYS**, not USB VBUS.

### 26.3 GPIB physical layer

**SN75160B**
- eight bidirectional DIO lines.

**SN75161B**
- ATN
- SRQ
- REN
- IFC
- EOI
- DAV
- NDAC
- NRFD

The SN75161B is appropriate because this adapter is intentionally a **single-controller** design.

### 26.4 Connector

Production requirement:

- IEEE-488 / micro-ribbon 24 position
- male
- right-angle
- through-hole PCB mount
- plugs directly into the rear connector of the instrument
- mechanical retention preferred with M3.5 hardware

A FUYCONN FUY57139-24P variant with M3.5 retention is a candidate.

NorComp 112-024-113R001 is a practical distributor-stocked 24-pin right-angle male PCB connector, but commonly comes with 4-40 retention hardware and therefore does not exactly match the M3.5 preference.

### 26.5 Consequence: the adapter is permanently System Controller

The SN75161B derives the direction of ATN, IFC and REN from the DC pin rather than from TE. There is no arrangement in which this design relinquishes those lines. The adapter is therefore the System Controller for as long as it is powered, and it cannot:

- be plugged onto a GPIB bus that already has a controller;
- participate in pass control;
- act as a non-controller device on someone else's bus.

This is consistent with the one-adapter-per-instrument product concept (§1) and with design decision 7, but it is a usage constraint that users will violate if it is not stated, because the connector physically stacks onto other GPIB hardware.

[R-HW-010] The enclosure MUST be labelled to indicate that the adapter is a GPIB System Controller and must not be connected to a bus with another controller.

[R-HW-011] The product documentation MUST state this restriction, and MUST state that daisy-chaining the adapter onto an existing GPIB chain is unsupported.

[R-HW-012] The firmware MUST detect the most common symptom of this misuse. When IFC is observed asserted by another device, or when bus arbitration fails at start-up, the firmware MUST refuse to drive the bus, MUST signal the condition on the status LED, and MUST report it through diagnostics rather than contending for control.

If a multi-controller variant is ever required, the SN75162B is the upgrade path, since it separates the system-controller function onto its own pin. That would be a hardware revision, not a firmware option.

---

## 27. Important RP2350 voltage-level decision

This design does **not require a generic level-shifter array** between RP2350 and SN75160B/SN75161B if the GPIO allocation is done correctly.

Why:

- SN75160/161 terminal-side logic uses TTL-compatible thresholds; VIH is 2.0 V, so a 3.3 V RP2350 output is valid high.
- RP2350 "Fault Tolerant" digital GPIOs can tolerate up to 5.5 V when IOVDD is powered at 3.3 V.
- GPIO0 through GPIO22 are fault-tolerant digital IO on the RP2350A.
- The ADC-capable GPIO26–GPIO29 on RP2350A are not the same fault-tolerant digital pin type.

Therefore:

> Every terminal-side pin that may ever be driven from a 5 V SN75160/161 output must use a fault-tolerant RP2350 GPIO.

The two pure direction-control outputs, TE and DC, can safely use GPIO26/27 because the signals only travel RP2350 -> transceiver and are never driven back into the MCU.

This produces a very clean pin map.

### 27.1 The IOVDD precondition and power sequencing

The datasheet qualifies the tolerance figure. Fault Tolerant Digital pins tolerate up to 5.5 V **provided IOVDD is powered to 3.3 V**. Outside that condition the tolerance does not apply.

This design has a sequencing hazard, because the two rails come up at different times:

```text
+5V_POE     -> SN75160B / SN75161B VCC        available first
   |
   +-> W5500-EVB-Pico2 VSYS -> on-board regulator -> +3V3 -> IOVDD   available later
```

During PoE ramp, PoE brown-out, USB/PoE source changeover (§30.1) and any 3.3 V regulator fault, the transceivers are powered and driving their terminal-side outputs while IOVDD is absent or below specification. The exposed pins are precisely the fault-tolerant ones the design relies on.

[R-HW-020] The transceiver 5 V supply MUST NOT be present while IOVDD is absent. The design MUST implement either a supply-sequencing element that gates SN75160B and SN75161B VCC on 3V3-good, or a series element on every terminal-side net sized so that the pad clamp current stays within specification when IOVDD is absent.

[R-HW-021] The chosen approach MUST be verified by measurement across PoE power-up, PoE removal, brown-out, and USB-to-PoE changeover, and the measurement MUST be recorded in the hardware validation report.

[R-HW-022] The 3V3-good threshold and the resulting sequencing delay MUST be documented, and MUST be compatible with the boot sequence of §41.

Gating VCC is preferred over series resistance, because it also removes the possibility of the transceivers driving the GPIB bus before the MCU can place them in a safe state (§27.3).

### 27.2 Erratum RP2350-E9

Erratum RP2350-E9 causes a Bank 0 GPIO configured as an input to latch at approximately 2.15 V rather than reading low. It was initially described as affecting only the internal pull-down case and was subsequently broadened in the datasheet to a description in terms of increased input leakage current. It is attributed to the fault-tolerant pad IP block, which is the same pad type this design depends on for all sixteen GPIB terminal signals.

The transceivers drive their terminal side actively in both directions, so steady-state reads are not at risk. The risk is confined to intervals in which a terminal pin is not actively driven: direction turnaround, transceiver disable, and reset.

[R-HW-030] The firmware MUST NOT rely on RP2350 internal pull-ups or pull-downs on GPIO0–GPIO15. Any required bias MUST be provided externally.

[R-HW-031] Direction turnaround MUST be analysed to identify every interval in which a terminal pin is undriven, and each such interval MUST either be shown to be shorter than the sampling window or be covered by external bias.

[R-HW-032] The silicon revision used for validation and for production MUST be recorded, and the design MUST be re-verified if the revision changes.

### 27.3 Safe bus state at reset and when unprogrammed

Revision 2 required in §64 that "GPIB bus remains electrically idle during MCU reset" but specified no mechanism. RP2350 pads come up high impedance, and on the SN75161B the direction of ATN, IFC and REN follows DC rather than TE. A floating or incorrectly biased DC pin can therefore leave the adapter asserting IFC or ATN on the bus while the MCU is held in reset, unprogrammed, or crashed.

[R-HW-040] TE and DC MUST have external resistors that place both transceivers in a non-driving or receive orientation whenever the MCU is not actively driving them. The resistor values and the resulting logic states MUST be stated in the schematic.

[R-HW-041] The terminal-side nets for ATN, IFC and REN MUST have external bias that de-asserts them, these being active-low signals, whenever the MCU is not driving.

[R-HW-042] The safe state MUST hold under: power-on before firmware starts, RUN pin reset, watchdog reset, an unprogrammed device, and a crashed or halted MCU under a debugger.

[R-HW-043] Compliance MUST be verified with an oscilloscope on ATN, IFC, REN, DAV and the DIO lines across each of the conditions in R-HW-042, and the captures MUST be retained in the validation report.

[R-HW-044] Preferred implementation is to gate transceiver VCC per R-HW-020, since an unpowered transceiver cannot drive the bus at all. External bias per R-HW-040 and R-HW-041 is then a second line of defence rather than the only one.

### 27.4 Power budget

Revision 2 asserted "sufficient power margin" without numbers. 802.3af compliance is a headline product claim and must be supported by calculation.

[R-HW-050] A written power budget MUST be produced before PCB layout, and MUST include:

- RP2350 and W5500 quiescent and active current at the intended clock;
- W5500 PHY active current with link up at 100 Mbit/s;
- SN75160B and SN75161B quiescent current;
- source current into remote bus terminations for all sixteen driven lines in the worst case;
- sink current from local terminations when remote devices drive lines low;
- total 5 V load, with margin against the WIZPoE-P1 rated output;
- total input power, with margin against the 802.3af power available at the PD.

[R-HW-051] PoE inrush and converter soft-start MUST be analysed against the chosen board-level bulk capacitance, and the capacitance value MUST be justified rather than chosen by habit. §30 currently proposes 22–47 µF pending this analysis.

[R-HW-052] The budget MUST be validated by measurement on the prototype under worst-case bus loading, and the measured figures MUST replace the estimates in the released document.

[R-HW-053] A thermal check MUST be performed in the intended sealed enclosure at maximum load and maximum ambient, because the PoE converter dissipates into a small volume hanging off an instrument connector (§33).

---

## 28. Proposed GPIO map

W5500 internally consumes GPIO16–21.

Recommended assignment:

```text
GPIO0   DIO1 terminal
GPIO1   DIO2 terminal
GPIO2   DIO3 terminal
GPIO3   DIO4 terminal
GPIO4   DIO5 terminal
GPIO5   DIO6 terminal
GPIO6   DIO7 terminal
GPIO7   DIO8 terminal

GPIO8   ATN terminal
GPIO9   SRQ terminal
GPIO10  REN terminal
GPIO11  IFC terminal
GPIO12  EOI terminal
GPIO13  DAV terminal
GPIO14  NDAC terminal
GPIO15  NRFD terminal

GPIO16  W5500
GPIO17  W5500
GPIO18  W5500
GPIO19  W5500
GPIO20  W5500
GPIO21  W5500

GPIO22  spare fault-tolerant digital GPIO

GPIO26  TE
GPIO27  DC
GPIO28  optional button/config input or analogue test point
```

Tie the TE pin of SN75160B and the TE pin of SN75161B to the same RP2350 TE signal.

This is a conventional arrangement and reduces the required control GPIO count.

### 28.1 PIO pin-grouping constraint

Because PIO now implements the byte handshake (§40), the pin map is constrained by PIO addressing rules, not only by fault tolerance. A PIO state machine addresses pins as contiguous groups counted from a base for its `in`, `out`, `set` and side-set operations.

The map above satisfies this, and the property must be preserved deliberately:

```text
DIO1..DIO8      GPIO0..GPIO7     contiguous   -> PIO out/in base 0, count 8
EOI             GPIO12           contiguous   -> PIO side-set / in base 12, count 4
DAV             GPIO13
NDAC            GPIO14
NRFD            GPIO15
```

[R-HW-060] The eight DIO terminal signals MUST occupy eight consecutive GPIOs with DIO1 at the lowest number.

[R-HW-061] EOI, DAV, NDAC and NRFD MUST occupy consecutive GPIOs so that one PIO state machine can drive and sample the handshake group.

[R-HW-062] Any future reassignment of GPIO0–GPIO15 MUST be validated against the PIO program's pin-group requirements before the schematic changes. This requirement exists because the grouping is invisible in a pin table and is easy to destroy by tidying.

ATN, SRQ, REN and IFC remain on GPIO8–GPIO11 and are driven from Go, not from the PIO program, consistent with the division of responsibility in §40.

### 28.2 Consequences of the map

[R-HW-063] GPIO0 and GPIO1 are DIO1 and DIO2, so the default UART0 pins are unavailable. Diagnostics MUST use USB CDC (§52), and the design MUST NOT assume a hardware serial console.

[R-HW-064] A visible status LED MUST be allocated. The module's on-board LED is the intended choice; its GPIO assignment MUST be confirmed against the W5500-EVB-Pico2 schematic and recorded in the board package rather than assumed from the Pico 2 pinout.

[R-HW-065] GPIO22 remains spare and MUST be left unconnected or brought to a test pad. GPIO28 is the optional configuration input of §33 and MUST have a defined state when unused.

---

## 29. SN75160B PE

For the initial design:

```text
PE = VCC
```

This selects three-state behavior on the SN75160B data bus.

Make PE accessible through a resistor option/test pad so passive-pullup mode can be evaluated during validation without a PCB respin.

---

## 30. Power design

Use two named low-voltage rails:

```text
+5V_POE
+3V3
```

`+5V_POE` powers:

- W5500-EVB-Pico2 VSYS
- SN75160B VCC
- SN75161B VCC

The W5500-EVB-Pico2 generates 3.3 V for the RP2350/W5500.

Each SN7516x device gets:

```text
100 nF ceramic directly at VCC/GND
4.7 uF local bulk near the pair
```

Add a larger board-level bulk capacitor on 5 V, for example 22–47 uF, after verifying PoE module startup behavior.

### 30.1 USB debug and PoE

USB-C is valuable for flashing and serial diagnostics.

The PCB shall not directly short PoE-derived 5 V to USB VBUS.

PoE power should feed the module's **VSYS** input according to WIZnet's board-power arrangement.

The GPIB 5 V rail should include either:

- power-source selection jumper; or
- diode/ideal-diode ORing

so that a debug USB cable cannot unintentionally back-power the PoE converter.

---

## 31. Isolation and grounding

The WIZPoE-P1 provides isolated PoE power, while Ethernet magnetics provide data isolation.

On the instrument side:

```text
RP2350 logic ground
SN75160/161 logic ground
IEEE-488 signal ground
```

are the same local ground.

Do not insert optocouplers between the GPIB transceivers and MCU in the first design.

Connector shield/chassis treatment should be explicit. Recommended PCB options:

```text
shield -> chassis pad / mounting structure
optional 0-ohm link to digital ground
optional RC/EMI coupling footprint
```

This allows EMC testing to decide the final connection strategy.

---

## 32. Protection

Recommended provisions:

- TVS/ESD footprint near RJ45 if not already adequately handled on the module.
- GPIB connector shell tied mechanically and with controlled chassis strategy.
- small series resistors (22–47 ohm) may be left as optional footprints on MCU-side high-speed digital lines for edge damping/debug.
- test points for:
  - TE
  - DC
  - ATN
  - DAV
  - NRFD
  - NDAC
  - SRQ
  - IFC
  - 5 V
  - 3.3 V
  - GND

Do not add arbitrary pullups to GPIB bus lines; the SN7516x family contains the physical-layer behavior needed for IEEE-488 and external resistor changes can alter bus loading.

---

## 33. Mechanical requirements

The product is intended to behave more like an intelligent plug than a traditional GPIB cable.

Requirements:

- center of mass kept close to the instrument connector;
- Ethernet cable exits rearward or downward without levering the GPIB connector;
- connector retention screws usable;
- PCB supported by enclosure, not by signal pins alone;
- USB-C reachable for development but can be hidden/recessed in production;
- status LED visible;
- optional reset/config aperture.

Because heavy classic GPIB cables are intentionally being avoided, enclosure and RJ45 placement should preserve that advantage.

---

# PART III — TinyGo firmware

## 34. Firmware repository structure

Recommended:

```text
gpib-poe/
|
+-- cmd/
|   +-- adapter/
|       +-- main.go
|
+-- internal/
|   +-- board/
|   |   +-- pico2.go
|   |   +-- pins.go
|   |
|   +-- ethernet/
|   |   +-- w5500.go
|   |   +-- dhcp.go
|   |
|   +-- gpib/
|   |   +-- bus.go
|   |   +-- controller.go
|   |   +-- address.go
|   |   +-- handshake.go
|   |   +-- commands.go
|   |   +-- serialpoll.go
|   |   +-- srq.go
|   |   +-- remote.go
|   |   +-- errors.go
|   |
|   +-- device/
|   |   +-- gpibdevice.go
|   |   +-- querydetect.go
|   |   +-- readpolicy.go
|   |
|   +-- services/
|   |   +-- hislip.go
|   |   +-- rawscpi.go
|   |   +-- mdns.go
|   |
|   +-- config/
|   |   +-- config.go
|   |   +-- storage.go
|   |
|   +-- diag/
|       +-- counters.go
|       +-- log.go
|
+-- go.mod
+-- README.md
```

---

## 35. Firmware layers

```text
Application services
    HiSLIP / raw SCPI / discovery
                  |
                  v
             Device API
                  |
                  v
          GPIB operation queue
                  |
                  v
          GPIB controller logic
                  |
                  v
        bus/pin abstraction layer
                  |
                  v
          TinyGo machine GPIO
                  |
                  v
         SN75160 + SN75161
```

Only the bottom layer knows physical pin numbers.

Only the GPIB engine knows IEEE-488 electrical/handshake state.

Only the service layer knows TCP ports and HiSLIP.

---

## 36. GPIB bus abstraction

```go
type Bus interface {
    SetData(byte)
    Data() byte

    ATN(bool)
    IFC(bool)
    REN(bool)
    EOI(bool)
    DAV(bool)

    SRQ() bool
    NRFD() bool
    NDAC() bool

    SetTalkEnable(bool)
    SetDirectionControl(bool)
}
```

The exact interface may be split between read/write methods to prevent illegal use, but the conceptual boundary should remain.

---

## 37. GPIB controller API

High-level firmware API:

```go
type Controller interface {
    InterfaceClear(ctx context.Context) error

    Write(ctx context.Context, addr uint8, p []byte, eoi bool) error
    Read(ctx context.Context, addr uint8, p []byte) (n int, eoi bool, err error)

    DeviceClear(ctx context.Context, addr uint8) error
    Trigger(ctx context.Context, addr uint8) error
    SerialPoll(ctx context.Context, addr uint8) (byte, error)

    Remote(ctx context.Context, addr uint8) error
    Local(ctx context.Context, addr uint8) error
    LocalLockout(ctx context.Context, addr uint8) error
}
```

All operations are serialized.

---

## 38. Addressing model

Although the physical product has one attached instrument, the GPIB address remains configurable because the instrument still expects normal IEEE-488 addressing.

Configuration:

```go
type Config struct {
    GPIBAddress uint8
    EOS         byte
    UseEOS      bool
    ReadPolicy  ReadPolicy
}
```

Typical range:

```text
0..30
```

Adapter/controller address can remain an internal constant and must not collide with the configured instrument address.

Before data write:

```text
ATN
UNL
UNT
address instrument as listener
address controller as talker if required by implementation
ATN off
transfer bytes
EOI on last byte when requested
```

Before read:

```text
ATN
UNL
UNT
address instrument as talker
address controller as listener
ATN off
receive until EOI/EOS/limit
```

### 38.1 Concrete addressing decisions

Revision 2 left "address controller as talker if required by implementation" undefined. The values below are normative so that the command sequences of §14 and §16.1 can be unit-tested against expected bus bytes.

```text
controller address              0
controller talk address  MTA    0x40
controller listen address MLA   0x20

instrument talk address   TAD   0x40 + GPIBAddress
instrument listen address LAD   0x20 + GPIBAddress

UNL   0x3F      unlisten
UNT   0x5F      untalk
GTL   0x01      go to local
SDC   0x04      selected device clear
GET   0x08      group execute trigger
LLO   0x11      local lockout
DCL   0x14      device clear, universal
SPE   0x18      serial poll enable
SPD   0x19      serial poll disable
```

[R-DEV-020] The controller address MUST be 0 and MUST NOT be configurable, because there is exactly one controller and one instrument.

[R-DEV-021] The firmware MUST reject a configured instrument address of 0 at validation time, and MUST refuse to start bus operations with a colliding address rather than failing later in an obscure way.

[R-DEV-022] The valid configured instrument address range MUST be 1 to 30 inclusive. Revision 2's stated range of 0 to 30 conflicts with R-DEV-020 and is corrected here.

[R-DEV-023] Address 31 MUST be rejected, being the untalk/unlisten encoding.

[R-DEV-024] The command byte constants above MUST be defined once in the GPIB command package and MUST be covered by golden tests asserting the exact byte sequence emitted for write, read, trigger, device clear, serial poll, remote, local and local lockout.

**Secondary addressing.** Extended addressing is not implemented in firmware v1. This is a gap Revision 2 neither implemented nor declared.

[R-DEV-025] Secondary addressing MUST be listed as a first-milestone non-goal in §3.

[R-DEV-026] The controller API of §37 and the configuration record of §51 MUST reserve space for a secondary address so that adding it later does not change the interface or invalidate stored configuration. A sentinel value MUST indicate "no secondary address".

[R-DEV-027] The product documentation MUST state that instruments requiring extended addressing are unsupported in v1, because this affects exactly the class of pre-SCPI instruments that §17.2 exists to serve.

---

## 39. Three-wire handshake implementation

The byte handshake is implemented in PIO for firmware v1 (§40). A direct-GPIO implementation of the same primitive is retained as a bring-up and diagnostic path. Both MUST satisfy the state descriptions below and MUST pass the same tests.

Do not implement DAV/NRFD/NDAC as three independent goroutines. In the direct-GPIO path this is one synchronous state machine; in the PIO path it is one PIO program.

Write-byte concept:

```text
1. put data on DIO
2. wait until listener(s) Ready For Data
3. assert DAV
4. wait until data accepted
5. release DAV
6. proceed to next byte
```

Read-byte concept:

```text
1. indicate ready
2. wait for DAV
3. sample DIO and EOI
4. acknowledge accepted
5. wait for DAV release
6. restore ready state
```

Each wait has a deadline.

The state machine must restore a safe bus state on timeout.

---

## 40. PIO strategy

Revision 2 treated PIO as a deferred optimization. Revision 3 makes it required for firmware v1.

The reason is not throughput. It is that PIO is the only mechanism on this platform that makes byte-handshake timing independent of the Go scheduler and the garbage collector (§23.1). A cooperatively scheduled runtime with a stop-the-world collector across both cores cannot offer a bounded response time for a bit-level protocol; a PIO state machine can, because it runs regardless of what the CPU is doing.

Division of responsibility:

```text
PIO state machine owns         Go owns
-----------------------        ----------------------------------
DIO1..DIO8 during transfer     ATN, REN, IFC assertion
DAV assertion and release      SRQ observation
NRFD / NDAC sampling           addressing and command sequencing
EOI on the final byte          serial poll sequencing
byte-level timing              timeouts, deadlines and recovery
                               all HiSLIP protocol logic
```

```text
TinyGo GPIB controller
        |
        v
      FIFO
        |
        v
   PIO state machine
        |
DIO / DAV / NRFD / NDAC / EOI
```

Two programs are required: a source handshake for the talker role and an acceptor handshake for the listener role.

[R-FW-030] The three-wire byte handshake MUST be implemented as a PIO program in firmware v1.

[R-FW-031] ATN, REN and IFC MUST remain under Go control and MUST NOT be driven by the PIO program. Command bytes are therefore sent by asserting ATN in Go, transferring the byte through the same PIO source-handshake program, and releasing ATN in Go. This keeps one handshake implementation for both command and data bytes.

[R-FW-032] The PIO program MUST NOT implement timeouts. Deadlines remain in Go per §49. The Go side MUST be able to abort a running state machine at any point, drain its FIFOs, and restore a safe bus state per §39.

[R-FW-033] The PIO program MUST be abortable between bytes without loss of bus integrity, so that the asynchronous preemption mechanism of §47.1 can insert a serial poll or a device clear.

[R-FW-034] EOI MUST be asserted with the final byte of a transfer by the PIO program, not by a separate Go operation, because the two cannot be sequenced reliably from the CPU side.

[R-FW-035] The direct-GPIO implementation MUST remain buildable and selectable at run time. It is used for Milestone 4 bring-up, for a diagnostic slow mode when investigating instrument-specific handshake problems, and as a cross-check that the PIO program is correct.

[R-FW-036] Both implementations MUST pass an identical test suite, including golden bus-byte sequences and timeout behaviour, and the test suite MUST be able to run against either without source changes.

[R-FW-037] The PIO library dependency MUST be pinned by version or commit alongside the toolchain (§5.2). `tinygo.org/x/pio` carries explicit RP2350 support; it is a small project and its stability MUST be treated as a project risk (§67).

[R-FW-038] The PIO program MUST be documented instruction by instruction with the IEEE-488 state it implements, because a PIO program is the least reviewable code in the firmware and the handshake is the least forgiving part of the protocol.

Addressing, ATN, REN, IFC, serial poll and protocol logic remain in Go. This keeps the complicated part observable and testable while offloading only the repetitive timing-critical primitive.

Consequence for §62: throughput measurement remains a milestone, but its purpose changes from "decide whether to adopt PIO" to "confirm the PIO implementation clears its floor, measure how far above it the design reaches, and identify the limiting element by measurement rather than assumption".

[R-FW-039] The PIO program MUST NOT be the limiting element for throughput at the design target of R-FW-120. Where the state machine's own clock or instruction count would bound the rate below that figure, the program MUST be revised rather than the target relaxed.

A secondary benefit of implementing the handshake in PIO is that it does not foreclose HS488, the non-interlocked handshake that raises the bus ceiling to 8 MB/s. HS488 is out of scope for v1 (§3) and this document makes no claim that it is achievable here — it would require validating transceiver edge rates, cable behaviour and PIO timing at roughly 125 ns per byte. The point is only that the choice of PIO leaves it a firmware question rather than a hardware one.

---

## 41. Boot sequence

1. confirm the supply sequencing condition of §27.1 before enabling transceiver drive;
2. configure TE/DC outputs into a safe receive/non-driving state, consistent with the external bias of §27.3;
3. configure all bidirectional terminal pins as inputs before enabling transceivers, using no internal pulls per R-HW-030;
4. initialize USB CDC diagnostics, non-blocking per R-FW-025;
5. load and start the PIO handshake programs, leaving them idle and not driving;
6. load persistent configuration, falling back to defaults on any validation failure per R-FW-070;
7. initialize W5500;
8. obtain DHCP address;
9. configure GPIB controller state;
10. pulse IFC for at least 100 us;
11. assert REN according to configured startup policy;
12. probe instrument identity if safe/configured;
13. start mDNS;
14. bind and listen on the HiSLIP port with at least the number of sockets required by §42.2;
15. start raw SCPI listener only if enabled by configuration per R-SEC-013.

A failed network configuration must not leave GPIB lines actively driven.

[R-FW-110] Steps 1 to 5 MUST complete before any step that can drive the bus, and a failure in any of them MUST leave the bus in the safe state of §27.3 rather than continuing.

[R-FW-111] Failure of step 7 or 8 MUST NOT prevent steps 4 and 6 from having taken effect, so that a unit with no network remains configurable over USB (§65).

[R-FW-112] Step 12 MUST be skippable by configuration, because an identity probe writes to the bus and some instruments respond badly to an unexpected `*IDN?`.

---

## 42. Network design and W5500 socket budget

W5500 supplies eight hardware sockets. Treat them as a scarce resource, and note that they do not behave like BSD sockets.

### 42.1 The W5500 listen model

There is no accept queue and no minting of new descriptors. A socket is individually configured, placed in LISTEN, and when a peer connects **that same socket becomes the connection**. The TinyGo driver reflects this: `Listen` ignores its backlog argument because the hardware has no such concept, and `Accept` returns once the listening socket has been connected.

HiSLIP requires **two TCP connections to the same port**, the synchronous and asynchronous channels. The consequence is direct and was missed by Revision 2:

> A single listening socket on port 4880 can accept the first channel and will then be unavailable for the second. A HiSLIP session cannot be established.

Revision 2's budget of "1 HiSLIP listening socket + 2 HiSLIP active session channels" is therefore not implementable and is replaced.

### 42.2 Corrected socket budget

```text
3  bound and listening on TCP 4880
     two become the synchronous and asynchronous channels of the active session
     one remains listening, to accept a reconnection or to refuse a second client
1  bound and listening on TCP 5025, raw SCPI
1  UDP, mDNS
1  UDP, DHCP client during lease operations
-------------------------------
6  planned use, 2 spare
```

[R-FW-040] At least two sockets MUST be simultaneously bound and listening on the HiSLIP port before the service is advertised, otherwise no session can be established.

[R-FW-041] A listening socket MUST remain available on the HiSLIP port while a session is active, so that a reconnection after an abnormal disconnect can be accepted and a second client can be answered rather than ignored.

[R-FW-042] When a second logical session is attempted, the server MUST refuse it with `FatalError` code 4, "Server refused connection due to maximum number of clients exceeded", and MUST close that connection. Silently accepting and stalling is not acceptable; a user connecting from a second PC must get a diagnosable error.

[R-FW-043] A connection that does not complete the initialization transaction within a bounded time MUST be closed and its socket returned to LISTEN. Without this, any port scan or abandoned connection permanently consumes one of three HiSLIP sockets. The timeout MUST be short, on the order of seconds, and MUST be separate from the session idle timeout.

[R-FW-044] When a channel closes, its socket MUST be re-armed into LISTEN as part of session teardown, and the firmware MUST count re-arm failures.

[R-FW-045] The firmware MUST handle the two channels arriving in either order and MUST NOT assume that the synchronous channel connects first. A connection that sends `AsyncInitialize` for an unknown SessionID MUST be rejected per §10.

[R-FW-046] No HTTP server is present in firmware v1 by default. Configuration and debugging use USB serial (§52).

### 42.3 Session idle timeout

The W5500 driver's `SetSockOpt` is not supported by the hardware and always returns an error, so `SO_KEEPALIVE` is unavailable. HiSLIP has no application-level heartbeat. A client PC that is unplugged, powered off, or crashes therefore leaves a half-open session holding two of the three HiSLIP sockets indefinitely, and the adapter becomes unreachable until power-cycled. Revision 2's acceptance criterion in §65 required recovery from a disconnected TCP client but specified no mechanism.

[R-FW-047] The firmware MUST implement an application-level session idle timeout. When no HiSLIP message has been received on either channel of a session for the configured interval, the session MUST be torn down, both sockets re-armed, and the event counted.

[R-FW-048] The idle timeout MUST be configurable and MUST default to a value long enough not to disturb legitimate long instrument operations. It MUST be documented alongside the timeout categories of §49.

[R-FW-049] Tearing down an idle session MUST restore a safe GPIB bus state and MUST release any HiSLIP lock held by that session, otherwise a crashed client leaves the instrument locked.

R-FW-049 matters more than it looks: a lock held by a dead session is indistinguishable from a lock held by a working one, and is the failure that most often requires a power cycle in LAN instrument deployments.

### 42.4 DHCP and mDNS are firmware work

The W5500 driver configures a static MAC, IP, subnet mask and gateway and provides a DNS resolver hook. It contains **no DHCP client**. Revision 2 listed `ethernet/dhcp.go` in the firmware tree but presented DHCP as a boot step (§41) and as an acceptance criterion (§65) without recording that it must be written.

[R-FW-050] The DHCP client MUST be treated as firmware-owned work with its own implementation and test task, including lease renewal, rebinding, and behaviour on lease expiry with an active HiSLIP session.

[R-FW-051] The mDNS/DNS-SD responder MUST likewise be treated as firmware-owned work. No existing TinyGo library is assumed.

[R-FW-052] Loss of the DHCP lease MUST NOT leave GPIB lines actively driven, consistent with §41.

[R-FW-053] A network bring-up milestone MUST precede HiSLIP firmware work, with the exit criterion that the adapter obtains a DHCP lease, advertises over mDNS, and holds two concurrent inbound TCP connections open on port 4880 while a third is refused. This is the earliest point at which the platform assumptions of this section are proven.

R-FW-053 exists because every requirement in §42 depends on driver behaviour that is cheaper to verify in a week of bring-up than to discover during HiSLIP integration.

---

## 43. DHCP and hostname

Default:

```text
DHCP enabled
hostname = gpib-<short-id>
```

Fallback options:

- stored static IPv4 configuration;
- optional link-local support later.

The hardware has no dependency on external DNS for local discovery.

---

## 44. mDNS / DNS-SD

Advertise:

```text
_hislip._tcp
port 4880
```

TXT records should include the LXI HiSLIP fields where information is known:

```text
txtvers=1
Manufacturer=...
Model=...
SerialNumber=...
FirmwareVersion=...
```

For an adapter, these values should represent the attached instrument when a valid IEEE-488.2 `*IDN?` response is available.

If the attached instrument does not support `*IDN?`, identify the bridge honestly rather than inventing instrument identity.

Example fallback:

```text
Manufacturer=GoTMC
Model=PoE-GPIB
SerialNumber=<adapter serial>
FirmwareVersion=<firmware>
```

---

## 45. Raw SCPI service

TCP 5025 is intentionally simple.

Responsibilities:

- accept one TCP stream;
- collect bytes until configured terminator, normally LF;
- submit one GPIB write with EOI;
- detect query using the same read-policy module as standard HiSLIP compatibility mode;
- send any resulting GPIB response back to the socket.

Raw SCPI is:

- a debugging path;
- a fallback for software that supports SOCKET resources;
- useful with `nc`;
- not a replacement for HiSLIP's status/trigger/SRQ/locking semantics.

---

## 46. GPIB worker queue

Recommended operation union:

```go
type OpKind uint8

const (
    OpWrite OpKind = iota
    OpRead
    OpClear
    OpTrigger
    OpReadSTB
    OpRemote
    OpLocal
    OpIFC
)
```

Fixed-size queue:

```text
capacity 8 or 16
```

Avoid unbounded channel growth.

Every request contains:

```text
operation
deadline
address
payload reference / buffer
completion channel
```

The queue is the sole owner of the GPIB controller.

---

## 47. Priority and ordering

Revision 2 ordered operations by perceived urgency and placed "trigger/remote-local control" above "normal write/read data". That is a defect.

`Trigger` is a **synchronous-channel** message. It carries a MessageID from the same counter as `Data` and `DataEnd`, and it participates in the RMT-expected check of §11.3.1. Allowing it to overtake queued write data reorders the synchronous stream and corrupts MessageID and RMT sequencing.

The correct rule is that ordering is a property of the channel a message arrived on:

> Synchronous-channel operations execute in strict arrival order relative to each other. Only asynchronous-channel operations may be reordered ahead of them.

### 47.1 Priority classes

```text
class 0   no bus access; answered by the server layer, never queued
          AsyncLock, AsyncLockInfo, AsyncMaximumMessageSize,
          AsyncInterrupted, AsyncDeviceClearAcknowledge

class 1   preempting bus operations that abandon in-flight work
          hard abort and interface recovery (IFC)
          HiSLIP Device Clear (SDC)

class 2   preempting bus operations that resume in-flight work
          serial poll for AsyncStatusQuery
          serial poll for SRQ discovery

class 3   non-preempting bus operations, executed at a transfer boundary
          remote / local control

class 4   synchronous-channel operations, strict arrival order
          Data, DataEnd, Trigger, ExtReadRequest

class 5   background
          diagnostics, identity probe
```

[R-SRV-040] Class 0 operations MUST NOT be placed on the GPIB operation queue. They require no bus access and MUST be answered by the server layer even while a GPIB transfer is in progress. Revision 2's single queue would have delayed `AsyncLock` and `AsyncMaximumMessageSize` behind instrument I/O for no reason.

[R-SRV-041] Class 4 operations MUST be executed in the order they were received on the synchronous channel. No class 4 operation MAY overtake another.

[R-SRV-042] Trigger MUST be treated as a class 4 operation.

[R-SRV-043] Class 3 operations MUST NOT interrupt a byte transfer. They execute when no transfer is in progress, or after the current transfer completes or its deadline expires, and the HiSLIP response reports the outcome.

[R-SRV-044] Repeated SRQ servicing MUST NOT starve class 4 traffic. A minimum interval between successive serial polls arising from SRQ MUST be enforced, and §16.2's RQS latch together with R-SRV-023 bounds the rate at which service requests are generated.

### 47.2 Asynchronous preemption of an in-flight transfer

Revision 2 ordered the queue but provided no way to interrupt an operation already running. That left `viReadSTB` and Device Clear blocked for the whole GPIB timeout — precisely when the operator needs them, because the instrument is stuck.

IEEE-488 permits the controller to take control between bytes. The firmware exploits this.

[R-SRV-050] The byte transfer primitive MUST expose a suspend point between bytes. The PIO program MUST be abortable there per R-FW-033, and the Go controller MUST be able to enter that state with a defined bus condition.

[R-SRV-051] Class 1 operations MUST preempt at the next suspend point and MUST abandon the in-flight operation. The abandoned operation MUST return an operation error to its requester, MUST NOT be retried automatically, and MUST NOT be reported as a fatal HiSLIP error (§21.1).

[R-SRV-052] Class 2 operations MUST preempt at the next suspend point, perform the serial poll, and then resume the suspended transfer by re-addressing the instrument and continuing with the remaining bytes.

[R-SRV-053] Resumption per R-SRV-052 MUST be policy-controlled, because not every legacy instrument tolerates being interrupted mid-message. The firmware MUST provide:

```text
PREEMPT_RESUME   default; preempt, poll, re-address, resume
PREEMPT_ABORT    preempt, poll, abandon the transfer with an operation error
DEFER            complete or time out the transfer first, then poll
```

[R-SRV-054] The selected policy MUST be reported by diagnostics, and preemption events MUST be counted separately from timeouts so that an instrument that misbehaves under preemption is identifiable in the field.

[R-SRV-055] The worst-case latency from receipt of an asynchronous request to the start of its bus operation MUST be documented and measured. It is bounded by one byte handshake plus the poll sequence, not by the operation timeout. This figure is an acceptance criterion in §65.

[R-SRV-056] Preemption MUST leave the bus in a state from which either resumption or recovery is possible. A preemption that cannot restore bus state MUST escalate to class 1 recovery rather than leaving the bus indeterminate.

---

## 48. SCPI query detection

For third-party HiSLIP/raw-SOCKET compatibility the firmware needs a small lexical detector.

Requirements:

- detect `?` in program header;
- ignore `?` inside quoted strings;
- preserve semicolon-separated command sequences;
- handle CR/LF;
- do not parse IEEE binary block bodies as text;
- permit multiple queries in one message.

The detector returns:

```go
type QueryDisposition uint8

const (
    NoResponseExpected QueryDisposition = iota
    ResponseExpected
    Unknown
)
```

`Unknown` behavior is configurable.

Read policies:

```text
SCPI_QUERY        default
SERIAL_POLL_MAV   optional for devices that reliably expose MAV
ALWAYS_READ       special legacy-device profile
GOTMC_EXPLICIT    only with GoTMC vendor extension
```

---

## 49. Cancellation and timeouts

Every externally initiated operation receives a `context.Context`.

GPIB wait loops do not need to inspect the context on every CPU cycle; they can check a monotonic deadline periodically.

Timeout categories:

```text
TCP session timeout
HiSLIP operation timeout
GPIB handshake timeout
GPIB response timeout
lock timeout
```

They must be separately configurable internally even if the initial UI exposes only one or two settings.

On GPIB handshake timeout:

- release driven handshake lines;
- restore controller bus state;
- return operation error;
- increment diagnostic counter.

Do not reboot the adapter as normal timeout recovery.

### 49.1 Timeout budget

The categories of §49 are extended and given an ordering constraint.

```text
GPIB handshake timeout        per byte, shortest
GPIB response timeout        per read operation
HiSLIP operation timeout     per client-visible operation
lock timeout                 client-supplied, per AsyncLock
device clear peer timeout    40 to 120 s per R-SRV-015
initialization timeout       per R-FW-043, seconds
session idle timeout         per R-FW-047, longest
```

[R-FW-060] The timeouts MUST satisfy `handshake < response < HiSLIP operation < session idle`. A configuration that violates this ordering MUST be rejected at start-up rather than producing confusing failures.

[R-FW-061] The GPIB handshake timeout MUST exceed the measured worst-case garbage-collection pause plus scheduler wake-up latency, with margin, per R-FW-024. It MUST NOT be derived from IEEE-488 bus timing alone.

[R-FW-062] Every timeout MUST be a named constant with its rationale recorded, and MUST be reportable through `show config` (§52).

[R-FW-063] Each timeout category MUST have its own diagnostic counter. A single aggregated timeout count is insufficient to distinguish a slow instrument from a stalled firmware task.

[R-FW-064] Timeout recovery MUST NOT drop the TCP session, reset the interface, or reboot. Only class 1 recovery (§47.1) may touch the interface, and only on explicit request or after a defined escalation.

Rationale for R-FW-061 being stated twice: a handshake timeout set from bus specifications will be a few hundred microseconds to milliseconds, while a stop-the-world collection on this platform can exceed that. The resulting failure presents to the user as an intermittent GPIB cable fault, which is the most expensive possible way to learn about a software timing bug.

---

## 50. SRQ implementation

SRQ is one signal where interrupt-assisted detection is useful.

Recommended:

- configure SRQ terminal-side GPIO input;
- edge/level interrupt schedules an SRQ event;
- do not perform the full serial poll inside an interrupt handler;
- GPIB worker handles the serial poll at high priority;
- service request coalesces while pending.

If TinyGo interrupt support on the chosen path proves awkward, polling SRQ at a modest interval is acceptable initially because SRQ is level-sensitive.

---

## 51. Persistent configuration

Minimum persistent configuration:

```text
GPIB address
secondary address, with a "none" sentinel      R-DEV-026
hostname
DHCP/static mode
static IPv4 data
read policy
stale-response flush enable                    R-DEV-013
preemption policy                              R-SRV-053
EOS enable/value
raw SCPI enable
HiSLIP enable
session idle timeout                           R-FW-048
startup REN behavior
vendor extension enable                        R-PROTO-055
```

Do not require a filesystem.

Use a small versioned config record in flash:

```go
type ConfigV1 struct {
    Magic   uint32
    Version uint16
    Length  uint16
    ...
    CRC32   uint32
}
```

Two-slot or journaled storage is preferred to survive power loss during update.

[R-FW-070] Unknown or out-of-range stored values MUST be replaced by defaults with a logged warning, and MUST NOT prevent the adapter from starting. A configuration error must never render the unit unreachable, because the unit may be physically inaccessible.

[R-FW-071] Raw SCPI MUST default to disabled, per §66.

### 51.1 Firmware update path

Revision 2 specified configuration persistence but no way to update the firmware. This is a serviceability requirement for a device wedged behind an instrument in a rack, and it must be decided rather than discovered.

Firmware v1 uses the RP2350 bootrom UF2 path over USB. This is deliberate: it needs no bootloader of our own, it cannot be bricked by a bad image, and it requires no network stack to work.

[R-FW-080] The update mechanism for v1 MUST be UF2 over USB via the bootrom mass-storage mode.

[R-FW-081] The enclosure MUST allow the update to be performed without destructive disassembly. If the USB-C connector is recessed per §33, an aperture MUST be provided, and the BOOTSEL entry method MUST be documented.

[R-FW-082] BOOTSEL entry MUST be possible without removing the adapter from the instrument, or the documentation MUST state that removal is required. Either is acceptable; an undocumented procedure is not.

[R-FW-083] The persistent configuration record MUST survive a firmware update. The magic, version, length and CRC32 fields exist for this purpose, and a newer firmware MUST migrate an older record rather than discarding it.

[R-FW-084] The running firmware version MUST be reported through diagnostics (§52) and in the mDNS TXT record (§44), so that a deployed unit's version is discoverable over the network without physical access.

[R-FW-085] Network firmware update is explicitly out of scope for v1 and MUST be listed in §3. If added later it MUST be authenticated, which per §66 is a larger change than it appears.

---

## 52. Diagnostics

Expose through USB serial initially:

```text
show config
show network
show gpib
show sessions
counters
idn
spoll
ifc
clear
trigger
```

Useful counters:

```text
boots
HiSLIP sessions
HiSLIP sessions refused                    R-FW-042
HiSLIP fatal errors                        R-SRV-033
HiSLIP nonfatal errors                     R-SRV-033
wrong-channel messages                     R-SRV-031
interrupted errors, silent (RMT mismatch)  R-SYNC-014
interrupted transactions sent              R-SYNC-021
orphan connections closed                  R-FW-043
idle sessions torn down                    R-FW-047
socket re-arm failures                     R-FW-044
DHCP lease acquisitions and renewals       R-FW-050
TCP disconnects
GPIB writes
GPIB reads
GPIB bytes TX
GPIB bytes RX
GPIB handshake timeouts                    R-FW-063
GPIB response timeouts                     R-FW-063
queries without response                   R-DEV-010
truncated responses                        R-DEV-011
stale-response flushes                     R-DEV-013
preemptions, class 1                       R-SRV-051
preemptions, class 2                       R-SRV-052
preemption resume failures                 R-SRV-054
PIO aborts                                 R-FW-032
SRQ events
serial polls
device clears
```

[R-FW-090] Every counter above MUST be implemented. A requirement that mandates counting an event (§21.1, §42, §47.1, §49.1) is not satisfied by a log line, because logs are not retained and the diagnostic serial port is normally disconnected.

Diagnostic commands are extended with:

```text
show counters
show timeouts
show scheduler        reports scheduler mode, TinyGo version, NumCPU
readpolicy [value]
preempt [policy]
```

No production feature should depend on debug serial being connected.

---

## 53. Logging

Use fixed-level logging:

```text
ERROR
WARN
INFO
DEBUG
TRACE
```

Default production level:

```text
INFO
```

TRACE may log protocol headers but should not dump arbitrary large binary payloads.

Avoid `fmt.Sprintf` in timing-sensitive GPIB paths.

---

# PART IV — Development sequence

## Execution order

The milestones below are deliverables, not a schedule. Their numbering is stable and is referenced throughout Part V, so it does not change. The order in which they are executed does.

Milestone 2, the host server, is executed **before** Milestone 1, the client:

```text
M0  protocol codec          complete
M2  host Go server          next
M1  Go HiSLIP client
M3  VISA integration
M4  GPIB electrical prototype
M5  TinyGo HiSLIP server
M6  SRQ and control semantics
M7  GoTMC explicit-read extension
M8  throughput and latency verification
```

Four reasons, in order of weight:

1. **A client has nothing to talk to.** No HiSLIP-capable instrument is available on the development bench, so a completed client would be unusable until either a server or a purchase exists. A completed server is immediately testable against free third-party clients (§24.4).
2. **The server attracts more independent scrutiny.** Three free third-party clients are available against one and a half servers. Hours spent on the server buy more interoperability evidence per hour.
3. **R-PROTO-041 needs the server.** The one open protocol question in this document, the `DeviceClearAcknowledge` channel defect, can only be settled by third-party clients talking to our server.
4. **The server is on the critical path to the product.** The client is a by-product of the library; the GPIB bridge is the deliverable.

[R-DOC-070] Milestone 2 MUST be executed before Milestone 1. The milestone numbers MUST NOT be renumbered to match, because Part V references them.

[R-DOC-071] The client MUST NOT be validated solely against this project's own server. See R-DOC-060. Executing the server first creates the temptation to do exactly that, and a client and server written by one author from one reading of the standard will agree with each other whether or not that reading is correct.

[R-DOC-072] The conformance vectors of §24.1 are the independent oracle for both sides and MUST be the first thing either side is tested against, before any cross-testing.

## 54. Milestone 0 — protocol codec

Deliver:

- `protocol.Header`
- message constants
- header marshal/unmarshal
- golden tests
- fuzz tests for decoder
- no networking yet

Exit criterion:

> malformed network input cannot cause large allocation or panic.

## 55. Milestone 1 — Go HiSLIP client

Implement:

- initialize
- async initialize
- Synchronized Mode
- Overlap Mode (generic client requirement)
- Data/DataEND
- Query/Command
- clear
- trigger
- status
- close

Test against an independent third-party HiSLIP server (§24.4). Executed after Milestone 2; see the execution order note at the head of Part IV.

Exit criterion:

```go
inst.Query(ctx, "*IDN?")
```

works reliably against an independent third-party HiSLIP server, and the Data, DataEnd, clear, trigger and status transactions behave identically against that server and against this project's own server.

Revision 2 required a "real commercial HiSLIP instrument/software endpoint". The instrument half of that is deferred per R-DOC-064. The requirement that survives, and the one that matters, is independence: the client must be proven against an implementation it does not share an author with.

## 56. Milestone 2 — host Go server

Implement server with mock Device. Executed before Milestone 1; see the execution order note at the head of Part IV.

The first complete server profile shall be Synchronized Mode. Generic server support for Overlap Mode is optional and must not delay the GPIB bridge.

This milestone delivers the Synchronized Mode machinery that everything else depends on, and which Revision 2 omitted:

- RMT-expected and RMT-delivered tracking, with the two interrupted cases behaving differently (§11.3.1);
- the Interrupted transaction, sending both messages (§11.3.2);
- MAV computed from the AsyncStatusQuery MessageID (§11.4.1);
- the four-message Device Clear sequence with its feature bitmap (§13.1);
- channel-ordered operation dispatch, with class 0 operations never queued (§47.1).

Validate with:

- NI-VISA
- Keysight IO Libraries / Connection Expert
- pyvisa-py
- lxi-tools/libhislip client
- GoTMC client, once Milestone 1 exists

Exit criteria:

third-party VISA client can open:

```text
TCPIP0::<host>::hislip0::INSTR
```

and query the mock instrument; and R-PROTO-041 is settled, with the `DeviceClearAcknowledge` channel behaviour confirmed against two independent clients and the result recorded in §4.4.

## 57. Milestone 3 — VISA integration

Add GoTMC TCPIP HiSLIP resource parsing and driver selection.

Exit criterion:

an IVI driver receives the same `ivi.Transport` whether the resource is USBTMC, raw TCP or HiSLIP.

## 58. Milestone 4 — GPIB electrical prototype

Hardware:

```text
W5500-EVB-Pico2
WIZPoE-P1
SN75160B
SN75161B
right-angle IEEE-488 male
```

Firmware initially has no HiSLIP.

Implement USB-console commands:

```text
ifc
write <hex/text>
read
idn
spoll
trigger
```

Exit criterion:

three different real GPIB instruments can be controlled reliably.

## 59. Milestone 5 — TinyGo HiSLIP server

Compile shared `protocol` + `server` code for RP2350.

The firmware build profile must compile with:

```text
SupportsSynchronized = true
SupportsOverlap      = false

TinyGo version       pinned, recorded, reported   R-FW-010
scheduler            tasks by default             R-FW-011
                     cores optional, >= 0.42.0    R-FW-012
handshake            PIO required                 R-FW-030
                     direct GPIO selectable       R-FW-035
pio library          pinned by commit             R-FW-037
```

Exit criterion:

NI/Keysight/PyVISA can perform `*IDN?` through:

```text
PC -> HiSLIP -> TinyGo -> GPIB -> instrument
```

## 60. Milestone 6 — SRQ and control semantics

Implement:

- serial poll
- SRQ
- clear
- trigger
- remote/local
- lock

Exit criterion:

corresponding VISA operations work end-to-end.

## 61. Milestone 7 — GoTMC explicit-read extension

Add capability negotiation and vendor-defined read request.

Exit criterion:

a non-SCPI GPIB instrument can be driven by GoTMC without relying on a query-marker heuristic.

## 62. Milestone 8 — throughput and latency verification

Because PIO is now required for firmware v1 (§40), the purpose of this milestone changes. It is no longer "decide whether to adopt PIO" but "confirm the implementation meets its floor, measure how far above it the design actually reaches, and identify the limiting element".

The limiting element MUST be established by measurement rather than predicted. Both candidates are plausible and the published evidence is ambiguous:

- **GPIB.** The three-wire interlocked handshake has a nominal ceiling of 1 MB/s, and good hardware controllers sustain 1 to 1.5 MB/s over short cables. With a direct connector and no cable, this design is in the favourable case.
- **W5500 link.** The hardware reaches roughly 11 to 15 Mbit/s, about 1.4 to 1.8 MB/s, on a fast MCU at 30 MHz SPI, which is above GPIB. But a naive driver is catastrophically slower — published Arduino-library figures fall to 15 to 20 kB/s. The TinyGo driver's bulk-transfer efficiency is unmeasured.
- **The instrument.** Usually the real limit in service. Legacy instruments commonly sustain 5 to 50 kB/s because their own processor formats the response.

Measure:

- GPIB byte rate, read and write, adapter-limited
- GPIB byte rate with each of the three real instruments from Milestone 4
- CPU load and per-core distribution
- W5500 SPI and TCP throughput and latency, measured independently of GPIB
- worst-case GC pause and scheduler wake-up latency (R-FW-024)
- steady-state allocation count (R-FW-021)
- large waveform transfer, end to end
- asynchronous preemption latency (R-SRV-055)

### 62.1 Targets

Revision 2 set no numbers, so the milestone had no exit criterion. Revision 3 sets a floor that must be met to release, a design target that the implementation is expected to reach, and an explicit rule that nothing in the design may impose a ceiling.

The three are different in kind and MUST NOT be conflated:

```text
floor           must be met to release; a release gate
design target   expected; a shortfall requires a documented reason
ceiling         none; measured and recorded, never designed in
```

**Floor.** These are release gates.

[R-FW-100] Sustained adapter-limited GPIB read MUST be at least 100 kB/s.

[R-FW-101] Sustained adapter-limited GPIB write MUST be at least 100 kB/s.

[R-FW-102] A short query round trip, measured LAN interface to LAN interface and excluding instrument processing time, MUST be at most 5 ms.

[R-FW-103] `AsyncStatusQuery` latency while a transfer is in flight MUST be at most 10 ms.

[R-FW-104] Device Clear completion while a transfer is stuck MUST be at most 50 ms.

[R-FW-105] Free SRAM after all buffers are allocated MUST be at least 50 percent.

[R-FW-106] Heap allocations during a sustained transfer MUST be zero.

The floor is set at roughly a tenth of the GPIB nominal ceiling. It is chosen so that the adapter is not the limiting element for the legacy instruments this product primarily serves, and it is deliberately not set at the achievable maximum, because a gate that only just passes a good implementation will fail a good implementation on a bad day.

**Design target.**

[R-FW-120] Sustained adapter-limited GPIB read and write SHOULD each reach 500 kB/s. Falling short is permitted only with a documented identification of the limiting element, per R-FW-126.

The design target exists because the floor alone would be satisfied by a poor implementation. A PIO handshake feeding a hardware TCP/IP controller has no structural reason to stop at 100 kB/s, and if it does, the reason is worth knowing.

**No ceiling.**

[R-FW-121] No upper limit on throughput is specified, and none MUST be introduced. The measured adapter-limited ceiling MUST be recorded in the validation report whatever it turns out to be, and MUST be re-measured when the toolchain, PIO program, driver or buffer sizing changes.

[R-FW-122] The byte transfer primitive MUST NOT insert fixed delays, sleeps or rate limiting beyond the settling time IEEE-488 requires. Throughput MUST be bounded by handshake response from the instrument, not by firmware pacing.

[R-FW-123] The PIO clock divider MUST be chosen so that the state machine is limited by bus handshake response rather than by its own clock rate, with the calculation recorded.

[R-FW-124] Buffer sizes and packet chunking MUST NOT bound sustained throughput. If measurement shows a buffer size to be limiting, the buffer MUST be enlarged within the memory budget rather than the target lowered.

[R-FW-125] No configuration option MAY throttle throughput below the floor. Diagnostic slow modes, including the direct-GPIO handshake path of R-FW-035, are exempt but MUST be clearly identified as diagnostic and MUST NOT be reachable by default.

[R-FW-126] The limiting element MUST be identified by measurement and named in the validation report: GPIB handshake, W5500 SPI, W5500 TCP, firmware data path, or the instrument. An unexplained throughput figure is not an acceptable milestone result.

**Method.**

[R-FW-107] Adapter-limited figures MUST be measured against a cooperative fast endpoint, so that they characterise the adapter rather than a legacy instrument. The per-instrument figures MUST be reported separately and MUST NOT be used to judge R-FW-100, R-FW-101 or R-FW-120.

[R-FW-108] R-FW-103 and R-FW-104 MUST be measured with the instrument deliberately stalled, because that is the condition under which the preemption mechanism of §47.1 exists.

[R-FW-109] All measurements MUST be repeated under both scheduler modes of §5.2, and the results MUST be recorded, so that the decision to ship with `cores` or `tasks` is made on data.

[R-FW-127] W5500 throughput MUST be measured independently of GPIB, by transferring to and from a host with the GPIB path idle. Without this, a firmware data-path limit is indistinguishable from a link limit and R-FW-126 cannot be satisfied.

Note on which metric matters to users: the dominant workload is short SCPI queries returning tens of bytes, which is entirely latency-bound. R-FW-102 therefore governs perceived responsiveness, and throughput becomes visible only on waveform, trace and screen-image transfers.

---

# PART V — Acceptance criteria

## 63. HiSLIP library

A release is acceptable when:

- wire framing is covered by golden tests, held as language-neutral vectors (§24.1);
- decoder is fuzz-tested;
- no inbound length field can force arbitrary allocation;
- standard Synchronized Mode HiSLIP session works;
- the message-type table and channel legality of §4.1 are enforced — R-PROTO-010 to R-PROTO-013;
- MessageID arithmetic is correct, including the `0xffffffff` chunking case and the `0xfffffefe` post-clear status query — R-PROTO-020 to R-PROTO-025;
- RMT-expected and RMT-delivered are tracked, and both interrupted cases behave differently and correctly — R-SYNC-010 to R-SYNC-014;
- the Interrupted transaction sends both messages — R-SYNC-020 to R-SYNC-024;
- MAV is computed from the AsyncStatusQuery MessageID, not from buffer occupancy alone — R-SYNC-030 to R-SYNC-033;
- `DeviceClearAcknowledge` channel handling is confirmed against two third-party clients — R-PROTO-040, R-PROTO-041;
- generic client passes tests for both Synchronized and Overlap Mode;
- PoE-to-GPIB server declines Overlap Mode and remains Synchronized — R-SRV-012;
- device clear works, including the full four-message sequence and feature bitmap — R-SRV-010 to R-SRV-016;
- trigger works, and is shown not to overtake queued synchronous data — R-SRV-041, R-SRV-042;
- status byte works, including both fields of AsyncStatusQuery — R-SRV-020;
- SRQ works, is not acknowledged, and does not re-fire before AsyncStatusQuery — R-SRV-021 to R-SRV-023;
- lock protocol works, and lock operations are answered while a GPIB transfer is in progress — R-SRV-040;
- wire error codes match §21.1 — R-SRV-030 to R-SRV-033;
- a query that produces no response terminates the transaction and reports the cause — R-DEV-010 to R-DEV-012;
- the vendor extension is refused gracefully by a non-supporting server — R-PROTO-053;
- client implements `ivi.Transport`;
- server compiles under the TinyGo profile;
- client proven against an independent third-party server — R-DOC-060;
- server proven against at least two independent third-party clients — R-DOC-061;
- the version of every third-party implementation used is recorded — R-DOC-063;
- third-party VISA interoperability is demonstrated.

## 64. Hardware

Prototype acceptance:

- powered solely by standards-compliant 802.3af PoE;
- stable 5 V under worst expected load, against a written and measured power budget — R-HW-050 to R-HW-053;
- no damaging USB/PoE backfeed;
- **no overvoltage stress on fault-tolerant pins during any interval in which IOVDD is absent or out of specification**, verified across PoE power-up, PoE removal, brown-out and USB-to-PoE changeover — R-HW-020 to R-HW-022;
- no voltage above specification on the non-fault-tolerant pins GPIO26–GPIO29 under any condition;
- no dependence on RP2350 internal pulls on GPIO0–GPIO15, and direction-turnaround intervals analysed against erratum RP2350-E9 — R-HW-030 to R-HW-032;
- GPIB bus remains electrically idle during MCU reset, during watchdog reset, when unprogrammed, and when halted under a debugger, verified by oscilloscope capture — R-HW-040 to R-HW-044;
- silicon revision recorded for the validated unit — R-HW-032;
- the adapter is labelled as System Controller and refuses to contend with another controller — R-HW-010 to R-HW-012;
- PIO pin grouping is intact and documented — R-HW-060 to R-HW-062;
- direct connector fit does not mechanically overload instrument socket;
- thermal check passed in the production enclosure at maximum load and ambient — R-HW-053;
- sustained GPIB transfer without handshake errors;
- SRQ is detected reliably.

Revision 2's criterion read "no MCU overvoltage on non-fault-tolerant pins". That tested the wrong thing: the design has no 5 V signal routed to a non-fault-tolerant pin, so the criterion passed by construction while the actual exposure — fault-tolerant pins during the IOVDD-absent window — went untested.

## 65. Firmware

Firmware acceptance:

- DHCP boot and mDNS advertisement, both implemented as firmware — R-FW-050, R-FW-051;
- network bring-up milestone passed before HiSLIP integration: lease obtained, service advertised, two concurrent inbound connections held on 4880, third refused — R-FW-053;
- one HiSLIP session, with a listening socket retained and a second client refused with FatalError 4 — R-FW-040 to R-FW-042;
- a connection that never initializes is closed and its socket re-armed — R-FW-043, R-FW-044;
- an unplugged client's session is torn down by the idle timeout, its sockets re-armed and its lock released — R-FW-047 to R-FW-049;
- raw SCPI fallback, disabled by default — R-FW-071;
- no reboot required after a GPIB timeout — R-FW-064;
- device clear cancels pending operations, including a transfer in flight — R-SRV-016, R-SRV-051;
- asynchronous status query and device clear meet their latency targets while the instrument is stalled — R-FW-103, R-FW-104, R-SRV-055;
- throughput floor met — R-FW-100, R-FW-101, R-FW-102;
- design target reached, or the shortfall explained by a named limiting element — R-FW-120, R-FW-126;
- measured adapter-limited ceiling recorded, with W5500 throughput measured independently — R-FW-121, R-FW-127;
- no fixed pacing, buffer turnaround or configuration option limits throughput — R-FW-122 to R-FW-125, R-FW-132;
- a transfer larger than any internal buffer succeeds, without pipeline stalls at buffer boundaries — R-FW-130, R-FW-131;
- zero heap allocation during a sustained transfer — R-FW-021, R-FW-106;
- functional under the default scheduler as well as under `cores` — R-FW-011, R-FW-109;
- no wait loop exceeds its stated maximum uninterrupted duration — R-FW-020;
- handshake timeout exceeds measured worst-case GC pause plus scheduler latency — R-FW-024, R-FW-061;
- PIO and direct-GPIO handshake implementations pass the same test suite — R-FW-035, R-FW-036;
- USB serial recovery/configuration works with Ethernet unavailable, and does not stall when no host is attached — R-FW-025;
- flash configuration survives power loss and a firmware update — R-FW-070, R-FW-083;
- firmware update is possible without destructive disassembly, by a documented procedure — R-FW-080 to R-FW-082;
- firmware version is discoverable over the network — R-FW-084;
- all counters of §52 are implemented — R-FW-090;
- memory usage leaves comfortable headroom on RP2350 — R-FW-105;
- toolchain version pinned and reported — R-FW-010.

---

# PART VI — Design decisions that should remain stable

1. **No VXI-11/ONC RPC in the primary architecture.**
2. **HiSLIP is the primary standards-based LAN instrument interface.**
3. **The PoE-to-GPIB HiSLIP device profile is Synchronized Mode only. Overlap Mode is not supported by the bridge.**
4. **The generic `gotmc/hislip` client supports both Synchronized and Overlap Mode.**
5. **Raw SCPI/TCP 5025 is retained as a lightweight fallback/debug path, disabled by default.**
6. **One adapter is intended for one physical GPIB instrument.**
7. **Adapter is GPIB System Controller / Controller-in-Charge, permanently and by construction (§26.5).**
8. **One serialized GPIB engine owns the bus.**
9. **GoTMC HiSLIP wire/server core is designed for TinyGo from day one.**
10. **RP2350 fault-tolerant GPIOs are used for every bidirectional 5 V transceiver terminal signal, subject to the IOVDD precondition of §27.1.**
11. **W5500-EVB-Pico2 + WIZPoE-P1 is the reference development platform.**
12. **SN75160B + SN75161B is the reference IEEE-488 physical layer.**
13. **A generic non-SCPI GPIB bridge needs an explicit-read mechanism; GoTMC gets this through an optional vendor extension while normal VISA software uses standard HiSLIP plus SCPI query detection.**
14. **PIO implements the three-wire byte handshake in firmware v1.** Revised in Revision 3. PIO is not a throughput optimization but the mechanism that decouples handshake timing from the Go scheduler and garbage collector. A direct-GPIO implementation is retained for bring-up and diagnosis.
15. **Cooperative scheduling within a core is a permanent platform constraint.** The firmware never depends on preemption arriving in a future toolchain release, and every wait loop yields.
16. **Ordering is a property of the HiSLIP channel.** Synchronous-channel operations execute in arrival order; only asynchronous-channel operations may be reordered or preempt.
17. **Asynchronous operations remain answerable while the bus is busy.** Operations that need no bus access are never queued behind instrument I/O, and those that do can preempt between bytes.
18. **Toolchain, PIO library and driver versions are pinned.** Multi-core operation is an optimization that can be withdrawn without functional change.
19. **The bus is idle whenever the MCU is not in control.** Transceiver power is subordinate to IOVDD, and external bias defines the bus state during reset, brown-out and unprogrammed conditions.
20. **The HiSLIP and GPIB data paths perform no heap allocation in steady state.** This is a timing requirement as much as a memory one.
21. **Throughput has a floor and a design target but no designed ceiling.** The floor is a release gate, the target is an expectation, and the measured maximum is recorded rather than specified. Nothing in the firmware may pace, throttle or buffer-bound the transfer rate, and the limiting element is established by measurement rather than assumption.

---

# PART VII — Security, risk and traceability

## 66. Threat model

Revision 2 had no security section. The adapter exposes two unauthenticated TCP services and an mDNS responder on whatever network it is plugged into, and §3 defers TLS, SASL and authentication. That is a defensible position for laboratory instrumentation, but it must be stated rather than left implicit.

**Assets.** Measurement data in transit; instrument configuration and state; the adapter's own configuration; the instrument's physical safety, since GPIB can command outputs.

**Trust boundary.** The adapter trusts its Ethernet segment entirely. It performs no authentication, no authorization and no encryption.

**Adversaries considered.**

```text
A1  unauthenticated host on the same network segment
A2  malformed or hostile packets from any host
A3  accidental misuse: a second client, or a stale client holding a lock
A4  physical access to the GPIB connector or USB port
```

**What is in scope.**

[R-SEC-010] A1 is accepted as unmitigated at the protocol level. The product documentation MUST state that the adapter provides no access control and MUST recommend deployment on an isolated or segmented instrument network.

[R-SEC-011] A2 MUST be mitigated. The decoder MUST be fuzz-tested (§54), MUST NOT allocate based on a peer-supplied length, and MUST NOT be able to fault the firmware. This is an availability requirement: a malformed packet that halts the adapter mid-measurement costs a test run, and a unit that needs a power cycle to recover is worse than no unit at all.

[R-SEC-012] A3 MUST be mitigated by the mechanisms already required: refuse a second session with a diagnosable error (R-FW-042), and release locks when an idle session is torn down (R-FW-049).

[R-SEC-013] Raw SCPI on TCP 5025 MUST default to disabled. It bypasses HiSLIP's session, lock and status semantics entirely and is a debug path, so it MUST be opt-in.

[R-SEC-014] The mDNS advertisement MUST NOT include information beyond what §44 specifies. In particular it MUST NOT expose configuration state or serial numbers of the attached instrument beyond the identity fields.

[R-SEC-015] A4 is accepted as unmitigated. Physical access to an instrument connector is equivalent to physical access to the instrument.

**What is deliberately excluded, and the consequence.**

Adding HiSLIP 2.0 secure connection later is not a configuration change. It requires TLS on a TinyGo target, certificate provisioning and storage, and a SASL mechanism — which is why §3 lists it as a non-goal rather than a backlog item. Network firmware update (R-FW-085) MUST NOT be added before authentication exists, because an unauthenticated network update turns A1 into full device compromise.

## 67. Risk register

Risks are recorded with their trigger and the response already specified. This section exists so that the project can tell the difference between a risk that has been accepted and one that has been forgotten.

```text
ID   risk                                        response                     ref
R01  TinyGo cores scheduler defect blocks        ship with -scheduler=tasks;  §5.2
     multi-core operation                        cores is withdrawable        R-FW-011
R02  RP2350 multicore atomics issue affects      as R01; validate sync        §5.2
     sync primitives                             primitives explicitly       R-FW-012
R03  ~4-month TinyGo cadence, rare patch         pin version; be able to      §5.2
     releases, fix not available when needed     build from upstream commit   R-FW-010
R04  tinygo.org/x/pio is a small project;        pin commit; direct-GPIO      §40
     RP2350 support could regress                path remains buildable       R-FW-035
R05  W5500 driver lacks DHCP and socket          firmware-owned work with     §42.4
     options                                     its own milestone            R-FW-050
R06  W5500 listen model breaks a naive           three listening sockets;     §42.1
     two-connection design                       proven in bring-up           R-FW-053
R07  GC pause trips handshake timeout and        zero-allocation rule;        §23.1
     is misdiagnosed as a bus fault              measured timeout budget      R-FW-024
R08  legacy instrument misbehaves when a         PREEMPT policy options;      §47.2
     transfer is preempted for a serial poll     per-event counters           R-SRV-053
R09  query-detection heuristic misclassifies     defined no-response path;    §17.3
     a program message                           stale flush; explicit read   R-DEV-013
R10  IOVDD absent while 5 V transceivers         gate transceiver VCC on      §27.1
     drive terminal pins                         3V3-good; measure            R-HW-020
R11  erratum RP2350-E9 affects the pad type      no internal pulls;           §27.2
     the whole level strategy depends on         analyse turnaround           R-HW-030
R12  bus driven during reset or when             external bias plus VCC       §27.3
     unprogrammed                                gating; scope verification   R-HW-040
R13  802.3af power or thermal margin             mandatory written budget     §27.4
     insufficient in sealed enclosure            and thermal check            R-HW-050
R14  adapter connected to a bus that already     labelling; refuse to         §26.5
     has a controller                            contend; documentation       R-HW-012
R15  discovery fails with Connection Expert      accept degraded discovery    §42.2
     or NI MAX because no HTTP/LXI page          in v1; socket budget has     R-FW-046
                                                 margin to add it later
R16  IVI-6.1 Table 4 channel defect for          follow the transaction       §4.4
     DeviceClearAcknowledge                      text; verify against two     R-PROTO-041
                                                 third-party clients
R17  vendor message type 128 collides with       GTMC magic plus explicit     §17.4
     another vendor's use                        negotiation; off by default  R-PROTO-055
R18  no cost target exists despite "low-cost"    produce a BOM with a target  §68
     being a headline claim                      price before PCB release     R-DOC-020
R19  upstream gotmc/hislip repository cannot     develop locally on the       §5.3
     be created without the organisation owner   final module path            R-DOC-045
R20  device-side packages acquire a non-TinyGo   import-graph test plus       §5.1.1
     dependency through routine maintenance      tinygo build in CI           R-DOC-030
R21  own client and own server agree with each   independent implementations  §24.4
     other but not with the standard             both ways; vectors first     R-DOC-071
R22  no commercial instrument in the test set,   deferred not cancelled;      §24.4
     so vendor deviations go unseen              three independent software   R-DOC-064
                                                 implementations meanwhile
R23  a third-party test implementation changes   record version or commit     §24.4
     or becomes unavailable                      per release                  R-DOC-063
```

[R-DOC-011] The risk register MUST be reviewed at each milestone exit. A risk MUST NOT be closed without either evidence that it did not materialise or a record of the response taken.

## 68. Requirement traceability index

[R-DOC-020] A bill of materials with a unit cost target MUST exist before the PCB is released for manufacture. "Low-cost" appears in §1 as a product claim and is currently unsupported by any figure.

[R-DOC-021] This index MUST be regenerated whenever a requirement is added, and a requirement that no acceptance criterion references MUST be either removed or given one.

```text
area    range        defined in            verified by
PROTO   010-013      §4.1                  §63
PROTO   020-025      §4.2                  §63
PROTO   030          §4.3                  §63
PROTO   040-041      §4.4                  §63
PROTO   050-055      §17.4                 §63
SYNC    010-014      §11.3.1               §63
SYNC    020-027      §11.3.2               §63
SYNC    030-033      §11.4.1               §63
SRV     010-016      §13.1                 §63
SRV     020-023      §16.4                 §63
SRV     030-033      §21.1                 §63
SRV     040-044      §47.1                 §63, §65
SRV     050-056      §47.2                 §65
SRV     060-063      §20.1                 §63
DEV     010-014      §17.3                 §63
DEV     020-027      §38.1                 §63
FW      010-014      §5.2                  §65
FW      020-025      §23.1                 §65
FW      030-038      §40                   §65
FW      039          §40                   §65
FW      040-053      §42                   §65
FW      060-064      §49.1                 §65
FW      070-071      §51                   §65
FW      080-085      §51.1                 §65
FW      090          §52                   §65
FW      100-109      §62.1                 §65
FW      110-112      §41                   §65
FW      120-127      §62.1                 §65
FW      130-133      §22                   §65
HW      010-012      §26.5                 §64
HW      020-022      §27.1                 §64
HW      030-032      §27.2                 §64
HW      040-044      §27.3                 §64
HW      050-053      §27.4                 §64
HW      060-065      §28.1, §28.2          §64
SEC     010-015      §66                   §63, §65
DOC     010-011      §1.1, §67             review
DOC     020-021      §68                   review
DOC     030-033      §5.1.1                §63
DOC     040-046      §5.3                  review
DOC     060-064      §24.4                 §63, §65
DOC     070-072      Part IV               review
```

---

# References

- IVI Foundation, **IVI-6.1: IVI High-Speed LAN Instrument Protocol (HiSLIP), Revision 2.0**, 2020-04-23. Tables 4, 14, 16 and sections 3.1, 6.12, 6.13 and 6.14 are the authority for §4.1, §4.3, §11.3.1, §11.4.1, §13.1 and §16.4.
- IVI Foundation, **VPP-4.3 VISA Library Specification**.
- LXI Consortium, **LXI HiSLIP Extended Function**.
- WIZnet, **W5500-EVB-Pico2 documentation**.
- WIZnet, **WIZPoE-P1 documentation/datasheet**.
- WIZnet, **W5500 datasheet**, for the socket and listen model underlying §42.1.
- Raspberry Pi, **RP2350 Datasheet**, including the pin-type table for the Fault Tolerant Digital IOVDD precondition (§27.1) and erratum **RP2350-E9** (§27.2).
- TinyGo, **Raspberry Pi Pico 2 target documentation**.
- TinyGo, **Tips, Tricks and Gotchas**, for the cooperative single-core scheduling statement underlying §23.1.
- TinyGo, **CHANGELOG** and `targets/rp2350.json`, for the default `tasks` scheduler and the availability of `-scheduler=cores` (§5.2).
- TinyGo drivers, **W5500 driver** (`tinygo.org/x/drivers/w5500`), for the `netdev` interface, the absence of DHCP, and the unsupported `SetSockOpt` (§42.3, §42.4).
- TinyGo, **`tinygo.org/x/pio`**, RP2350 PIO support (§40).
- Texas Instruments, **SN75160B datasheet**.
- Texas Instruments, **SN75161B/SN75162B datasheet**.
- GoTMC, **ivi**, **visa**, **lxi**, and **usbtmc** repositories.


# Netlist Specification: PoE-to-GPIB Adapter

**Status:** Draft — Revision 2 (from-scratch discrete board)
**Date:** 2026-09-21
**Board:** GoTMC HiSLIP PoE-to-GPIB adapter — discrete RP2354A + W5500
**Parent document:** [design-spec.md](design-spec.md), Revision 3, Part II (§25–§33), §41, §64.

---

## 0. Revision 2 changes and divergence from design-spec

Revision 1 specified the board as a carrier for the WIZnet **W5500-EVB-Pico2 module**.
That module is retained only for firmware/protocol development; the production
board is **built from discrete silicon**. This revision replaces the module with a
discrete RP2354A + W5500 design and, in doing so, resolves the supply-sequencing
element differently. The changes below **diverge from design-spec.md Revision 3**
and are proposed for adoption in a design-spec **Revision 4** (§10):

1. **Module → discrete.** design-spec.md §26.1 and stable-decision 11 name the
   W5500-EVB-Pico2 as the platform. Here it is the *development* platform only;
   the product is discrete RP2354A + W5500. The GPIO map, the GPIB physical layer
   and every requirement `R-HW-*` are preserved.
2. **RP2350 → RP2354A.** The QFN-60 RP2354A is pin-identical to the RP2350A but
   adds **2 MB internal stacked flash**, so the external QSPI flash IC, its
   decoupling and the six QSPI nets are removed (RP "Hardware design with RP2350",
   §3: *use RP2354 by omitting the onboard flash and R10*). RP2354 ships on the
   **A4 stepping, which fixes erratum RP2350-E9**; this relaxes `R-HW-030`/`R-HW-031`
   (the Bank-0 input-latch hazard) — external bias is retained for direction
   control, not for E9.
3. **Gating element (decision 1) → all-discrete PG-driven gate.** The load-switch
   IC + supervisor of Revision 1 (TPS22918 + TPS3839) become a **P-FET high-side
   switch + N-FET level-shift driven by a 3V3-good signal** (§3.1). Because the
   board now owns its 3.3 V regulator, "3V3-good" comes from a supervisor on that
   rail rather than a bolt-on watching a module output.
4. **Assembly constraints.** Hand-soldered v1: **passives 0805 minimum**; ICs in
   QFN-60/LQFP/SOIC/SOT/SOT-223 (all hand-solderable). Every BOM line carries an
   **LCSC part number** so symbols/footprints pull via `easyeda2kicad` and the
   same BOM feeds a later JLCPCB PCBA revision.

---

## 1. Purpose and scope

The electrical specification of the discrete adapter board: component list with
LCSC part numbers, the nets, the values that make the board safe, and the trace
from each `R-HW-010`..`R-HW-065` to the parts and nets that implement it.

**Specification depth.** The *decision and safety-critical* subsystems — the
transceiver-rail gate (§3.1), the GPIB safe-state bias (§3.2), the GPIB signal
path, the RP2354↔transceiver GPIO map, the power rails, and the IEEE-488
connector — are specified **pin-precisely** and are machine-checked (§9). The
*standard* MCU and Ethernet support subsystems are specified as **component
blocks with their key nets**, to be entered from two proven references rather than
re-derived pin-by-pin:

- **RP2354A:** Raspberry Pi *"Hardware design with RP2350"* + the **RP2350A Minimal
  KiCad design** (omit flash for RP2354).
- **W5500:** the WIZnet **W5500 datasheet reference schematic (Figure 3)** and a
  W5500 KiCad reference design.

This split matches the parent's own latitude (design-spec.md §31 leaves magnetics/
grounding to EMC, §32 makes protection footprints optional) and the build flow:
the standard subsystems are pulled as reference blocks, the safety-critical parts
are authored here and verified.

RFC 2119 keywords per design-spec.md §1.1. This document defines no new `R-HW-*`
identifiers; it references and implements those of design-spec.md §26–§28.

---

## 2. Architecture

```text
        RJ45 (PoE MagJack J2, 802.3af)
        │  data pairs         centre taps + spare pairs
        │                     │
   Ethernet magnetics    ┌────┴─────┐
        │                │ WIZPoE-P1 │ M1  (isolated 802.3af PD)
   TXOP/TXON/RXIP/RXIN   └────┬─────┘
        │                     │ isolated 5 V
      W5500 U2  ◄── SPI ──►  +5V_POE ───┬─────────────────────┐
        │  (GPIO16-21)                   │                     │
        │                          U5 LDO 5V→3V3          Q1 P-FET gate
      RP2354A U1                        │                (on when 3V3 good)
        │  GPIO0-15 (GPIB)          +3V3 ─┬─ U6 supervisor      │
        │  GPIO26/27 (TE/DC)              │   └── 3V3-good ──► Q2 ──► Q1 gate
        │  USB (J3)                  (IOVDD, W5500, bias)       │
        │                                                  +5V_XCVR
   ┌────┴──────┐                                           │  (gated)
   │           │                                    ┌──────┴──────┐
 U3 SN75160B  U4 SN75161B  ◄── VCC = +5V_XCVR ──────┤             │
 (DIO1-8)     (ATN…NRFD)                            (off until 3V3 good)
   └────┬──────┘
     IEEE-488 J1 → instrument
```

Two rails: `+5V_POE` (always present when powered) and `+3V3` (from U5).
`+5V_XCVR` is `+5V_POE` gated by Q1 on 3V3-good. The adapter is permanently the
GPIB System Controller (design-spec.md §26.5).

---

## 3. Decisions resolved

### 3.1 Decision 1 — supply sequencing, all-discrete (`R-HW-020`..`R-HW-022`, `R-HW-044`)

**Gate the transceiver 5 V rail on 3V3-good, with discrete parts.** Same hazard as
Revision 1: the RP2354 Fault-Tolerant pads tolerate 5.5 V **only while IOVDD is at
3.3 V** (RP2350 datasheet §14.9, `VPIN_FT`). `+3V3`/IOVDD comes up after `+5V_POE`,
so the transceivers must not be powered until `+3V3` is good. The SN75160B/SN75161B
present *"No Loading of Bus When Device Is Powered Down (VCC = 0)"* (SLLS004B/005B),
so an unpowered transceiver also satisfies the primary safe-bus requirement
(`R-HW-044`).

Circuit:

```text
            +5V_POE ──┬───────────────[Q1 S]  AO3401 P-FET
                      │                 │
                    R_G1 100k          [Q1 D]──► +5V_XCVR  (U3/U4 VCC)
                      │                 │
            gate node ┴───────┬─────────┘
                      │      C_G 100nF (soft-start / inrush, R-HW-051)
                    [Q2 D]  2N7002 N-FET
                      │
            3V3-good ─[Q2 G]      [Q2 S]──GND
                │      │
              (U6)    R_G2 100k ──GND   (fail-safe: Q2 off when 3V3-good undriven)
```

| Ref | Part (LCSC) | Role |
|---|---|---|
| **U5** | LDO 5V→3.3V, AP2114H-3.3TRG1, SOT-223 (**C150716**); alt AMS1117-3.3 (**C6186**) | `+5V_POE → +3V3`, ≥1 A ≫ the ~180 mA load (§3.3) |
| **U6** | Supervisor **TPS3839G33DBZR**, SOT-23-3 (**C485802** ✔), `VIT-` = 3.003/**3.08**/3.126 V, `Vhys` 31 mV, **push-pull** active-low RESET | Emits **3V3-good** (RESET high when +3V3 ≥ threshold) |
| **Q1** | AO3401 P-FET, SOT-23 (**C15127**), −30 V, `Vgs(th)` ≈ −1.1 V | High-side switch `+5V_POE → +5V_XCVR` |
| **Q2** | 2N7002 N-FET, SOT-23 (**C8545**) | Level-shifts 3V3-good to the 5 V-referenced Q1 gate; allows full turn-off |
| **R_G1** | 100 kΩ, gate→+5V_POE | Default **off** (Q1 gate pulled to source) |
| **R_G2** | 100 kΩ, Q2 gate→GND | Fail-safe **off** when U6 is unpowered/high-Z |
| **C_G** | 100 nF, gate→+5V_POE (with R_G1) | Controlled turn-on ~10 µs–ms band, inrush limit (`R-HW-051`) |

Chain: **+3V3 ≥ 3.08 V → U6 RESET high → Q2 on → Q1 gate pulled low → Q1 on →
+5V_XCVR live.** When +3V3 is absent U6 is unpowered, `R_G2` holds Q2 off, `R_G1`
holds Q1 off, `+5V_XCVR` is dead — the transceivers cannot drive the FT pads or
the bus. This is the sequencing `R-HW-020` requires, built from a P-FET the way
you asked, with the "3V3-good" threshold (not mere presence) coming from U6.

**U6 = TPS3839G33DBZR (C485802), verified against SBVS193D.** Its `RESET` is
**push-pull**, so `PG_3V3` needs no pull-up and `R_G2` remains the sole fail-safe;
an open-drain supervisor would have required a pull-up whose rail choice is itself
a hazard (a pull-up to +5V would assert 3V3-good with no +3V3 present). `VIT-` =
3.003 / 3.08 / 3.126 V against the AP2114H's ±1.5 % band (3.25–3.35 V) leaves
≈124 mV between `VIT-`max and the rail's minimum, so the gate cannot chatter.

**Defect found while drawing the schematic (2026-09-21): `C_G` must return to
+5V_POE, not to GND.** The machine-readable table in `docs/netlist-spec.nets`
listed `C_G.2` on `GND` while the row above puts C_G "gate→+5V_POE". Those are
not interchangeable. A gate-to-GND capacitor holds the gate near 0 V while
`+5V_POE` rises, so `Vgs` follows the rail down to about −5 V and **Q1 turns on**
during the ramp — powering the transceivers with IOVDD absent, the exact
condition `R-HW-020` exists to prevent. Referenced to the source instead, C_G
holds `Vgs ≈ 0` (off) through the rail's rise and, with `R_G1`, sets the
turn-on ramp. The schematic follows this row; the net table has been corrected
to match, and `scripts/netlist-check.py` now fails if C_G is moved back.

**Consequence for the boot sequence (§41, `R-FW-110`):** the TPS3839 holds RESET
asserted for a further **≈200 ms** after +3V3 crosses `VIT- + Vhys`, so
`+5V_XCVR` — and therefore U3/U4 — is unpowered for roughly the first 200 ms of
board life. Firmware MUST NOT assume the transceivers are powered at reset; this
strengthens rather than weakens `R-HW-020`, but it is a timing fact the firmware
bring-up has to expect.

Why not a single integrated-PG LDO: the clean LCSC part (LM9076, LDO + delayed
reset) is rated **150 mA**, below the ~180 mA the W5500-dominated 3.3 V rail draws,
so a ≥1 A LDO plus a separate supervisor is used. `VIT-` = 3.0–3.08 V is high
enough that IOVDD is effectively 3.3 V for the FT precondition, low enough to
clear the LDO's regulation band. Compatible with the boot sequence (design-spec.md
§41): hardware enforces step 1 before firmware runs (`R-FW-110`).

### 3.2 Decision 2 — safe bus state (`R-HW-040`..`R-HW-044`), unchanged from Rev 1

The SN75161B direction table (SLLS005B): ATN/IFC/REN follow **DC** (driven onto the
bus only when DC = L); DAV/NDAC/NRFD follow **TE**. RP2354 pads come up
high-impedance, so TE and DC are biased externally; the RP2354 A4 stepping fixes
E9, but external bias is still required to define direction when the MCU is not
driving.

**Safe state: `TE = L`, `DC = H`** → SN75160B DIO high-impedance, ATN/IFC/REN
received (not driven). SRQ, NDAC, NRFD are the only lines the SN75161B drives in
this state; SRQ is de-asserted by bias, NDAC/NRFD settle to the benign
not-ready/not-accepted hold (no transfer without a DAV strobe, and DAV is a
receiver here).

| Net | Bias | Result |
|---|---|---|
| `TE`  (U1 GPIO26) | `R_TE` 10 kΩ **→ GND** | TE = L: nothing drives data or DAV |
| `DC`  (U1 GPIO27) | `R_DC` 10 kΩ **→ +3V3** | DC = H: ATN/IFC/REN receive |
| `ATN_T` (GPIO8)   | `R_ATN` 10 kΩ **→ +3V3** | de-assert if DC ever = L with MCU idle (`R-HW-041`) |
| `REN_T` (GPIO10)  | `R_REN` 10 kΩ **→ +3V3** | `R-HW-041` |
| `IFC_T` (GPIO11)  | `R_IFC` 10 kΩ **→ +3V3** | `R-HW-041` |
| `SRQ_T` (GPIO9)   | `R_SRQ` 10 kΩ **→ +3V3** | de-assert the SRQ driven when DC = H |

**Rail discipline (invariant):** every bias references `+3V3` or `GND`, **never
`+5V`**. TE/DC land on GPIO26/27, which are not fault-tolerant, so a 5 V pull-up is
forbidden (design-spec.md §64); the FT-pin pull-ups also stay on +3V3 so no 5 V is
injected during the IOVDD-absent window.

### 3.3 Decision 3 — power budget (`R-HW-050`..`R-HW-053`)

**3.3 V rail (from U5):**

| Load | Current | Source |
|---|---:|---|
| W5500 @100 Mbit/s TX | 132 mA | W5500 datasheet |
| RP2354A dual-core @150 MHz + PIO + SPI + IO | 30 mA | RP2350 datasheet Table 1446 + margin |
| Board housekeeping (status LED, U6, crystals) | 15 mA | est. |
| Safe-state bias (DC + ATN/REN/IFC/SRQ + CFG ≈ 6 × 0.33 mA) | 2 mA | computed |
| **+3V3 subtotal** | **≈ 180 mA** → 0.59 W | |

**U5 dissipation:** (5 − 3.3) × 0.18 ≈ **0.31 W** in the LDO — fine in SOT-223, but
it is the board's hot spot and is included in the thermal check (`R-HW-053`).

**5 V transceiver rail `+5V_XCVR`** (gated): SN75160B `ICC` 110 mA + SN75161B `ICC`
110 mA + bus-termination worst case ~30 mA ≈ **250 mA → 1.25 W** (SLLS004B/005B).

**Total on `+5V_POE`:** module-less, so: U5 input (0.59 W ÷ ~0.9 ≈ 0.15 A at 5 V
after LDO — an LDO is ~66 % efficient here, so ≈ 0.18 A) + `+5V_XCVR` 0.25 A +
housekeeping ≈ **≈ 0.44 A → 2.2 W**, +25 % margin ≈ **0.55 A → 2.75 W**.

**Against the sources:** WIZPoE-P1 rated 5 V / **8 W typ (≈ 1.6 A)** → load is
28–34 %, margin > 3×. 802.3af guarantees **12.95 W at the PD**; input ≈ 2.2 W ÷ 0.80
≈ **2.75 W** typical (≈ 3.4 W worst-case) → **Class 1 (≤ 3.84 W)**, margin > 3×.
M1's class is programmable, so the board can declare Class 1 and a PSE reserves
only that much rather than a module's full rating.

**Inrush / bulk (`R-HW-051`):** `C_BULK` 47 µF on `+5V_POE` (hold-up through the
`+5V_XCVR` turn-on step); `+5V_XCVR` local bulk 2 × 4.7 µF + 2 × 100 nF, charged
through the `R_G1`·`C_G` gate ramp on Q1 (soft-start), inrush a few tens of mA.
`R-HW-052` replaces these with measured figures on the prototype.

---

## 4. Component list (BOM)

Passives are **0805 minimum**, X7R/C0G, 1 % (R) / 10 % (C) unless noted. LCSC
numbers marked *(verify)* are the best current candidate to confirm on LCSC before
release; representative 0805 basic-part numbers are given for common values.

### 4.1 Active / key components

| Ref | Part | Package | LCSC | Function |
|---|---|---|---|---|
| **U1** | Raspberry Pi **RP2354A** | QFN-60 | **C41378174** | MCU, 2 MB internal flash (no external QSPI) |
| **U2** | WIZnet **W5500** | LQFP-48 | **C32843** ✔ | Hardwired TCP/IP + 10/100 PHY |
| **U3** | TI **SN75160BDW** | SOIC-20 | *(number to source)* | GPIB data transceiver (DIO1–8). Symbol/footprint from KiCad stock: `Interface:SN75160BDW` + `Package_SO:SOIC-20W_7.5x12.8mm_P1.27mm` |
| **U4** | TI **SN75161BDW** | SOIC-20 | **C139410** ✔ | GPIB management transceiver (C139410 is the **161**, confirmed) |
| **U5** | Diodes **AP2114H-3.3TRG1** | SOT-223 | **C150716** ✔ | 5 V→3.3 V LDO, 1 A (alt AMS1117-3.3, **C6186** ✔). **Not C166063** — see §4.3 |
| **U6** | TI **TPS3839G33DBZR** (`VIT-` 3.08 V typ, push-pull) | SOT-23-3 | **C485802** ✔ | 3V3-good signal. Pins: 1 GND, 2 `RESET`, 3 VDD |
| **Q1** | Alpha&Omega **AO3401** | SOT-23 | **C15127** | P-FET high-side gate |
| **Q2** | **2N7002** | SOT-23 | **C8545** | N-FET level-shift |
| **M1** | PoE PD front-end — SDAPO **DP9900LPB-5V1.4A**, isolated PD module + DC-DC, 36–57 V in, 5 V/1.4 A (7 W) out, 1500 V I/O isolation, **programmable power class**. Bridges are **not** included — see D_BR1/D_BR2 | SMD, **21.2 × 13.9 × 8.0 mm** (verified from the part's 3D model) | **C53065982** ✔ *(specs/geometry verified; stock not verified)* | Isolated 802.3af PD → +5V_POE. Pins: 1 +VDC → +5V_POE, 2 −VDC → GND, 3 ADJ (unused), 4 VIN+, 5 VIN− ← bridge outputs. Dimension- and pin-compatible with **Silvertel Ag9905-LPB** (second source) |
| **D_BR1, D_BR2** | MCC **MB6S-TP** bridge rectifier ×2, 600 V / 0.5 A | MBS (SOIC-4), 4.7 × 4.0 × 2.5 mm | **C85619** ✔ | Rectify each pair set for M1's VIN±: D_BR1 = Alternative A (RJ45 1&2, 3&6 taps), D_BR2 = Alternative B (4&5, 7&8). ~60 mA at 48 V against a 0.5 A rating; 600 V PIV is generous over 57 V + surge |
| **J1** | **NorComp 112-024-113R001** — Centronics-24 (IEEE-488) **male**, right-angle PCB; alt FUYCONN FUY57109A | 24-way THT, 2 × 12 on 2.16 mm pitch, 4.29 mm rows, 2 × 3.1 mm holes on 46.8 mm | *(distributor; not an LCSC line)* | Instrument connector, **direct plug** into the instrument's female port. Symbol `Connector_GPIB:Conn_GPIB_L-com_CIB24SRA` (pinout is gender-independent); footprint `adapter:CONN_112-024-113R001_NRC`, **verified** — see §4.4 item 2 |
| **J2** | PoE MagJack — candidate **USAKRO DGUK411Q211DB2A1DP2** (2.5G PoE+, 4-pair, 4 × CT + VC1–VC4) | RJ45 TH int. magnetics | **C19725138** ⚠ *(pin function unconfirmed — see §4.4)* | Ethernet + PoE tap |
| **J3** | USB-C receptacle **TYPE-C-31-M-12** | 16-pin SMD | **C165948** | USB (flash + CDC diag) |
| **X1** | **12 MHz** crystal, Abracon **ABM8-272-T3** (RP-recommended), 10 pF, ≤50 Ω ESR | 3.2×2.5 mm | *(verify; LCSC 12 MHz alt)* | RP2354 clock |
| **X2** | **25 MHz** crystal (W5500), ±30 ppm | 3.2×2.5 mm | *(verify)* | W5500 clock |
| **L1** | Abracon **AOTA-B201610S3R3-101-T**, 3.3 µH, shielded, polarity dot | 0806 | *(verify)* | RP2354 core SMPS inductor |

### 4.2 Passives and discretes (0805 minimum)

**Designator convention.** KiCad treats a reference with no trailing digit as
*unannotated*, and an annotate pass in eeschema would silently rename such parts
— breaking the correspondence between the schematic and this document. Every
designator therefore carries a numeric suffix in the schematic and in
`netlist-spec.nets`: `R_TE1`, `C_G1`, `TP_5V1`, `C_U3a1`, and so on. This
section's tables name the parts without the suffix for readability; the suffixed
form is the authoritative one.

| Ref(s) | Value | LCSC (representative) | Purpose |
|---|---|---|---|
| C_BULK | 47 µF, 16 V | C2011903 ✘ *(is a 16 MHz oscillator — reselect)* | +5V_POE bulk / hold-up (`R-HW-051`) |
| C_U3a, C_U4a | 100 nF ×2 | C49678 ✔ | VCC bypass at U3/U4 |
| C_U3b, C_U4b | 4.7 µF ×2 | C1779 ✔ (0805) | +5V_XCVR local bulk |
| C6, C7, C9 | 4.7 µF ×3 | C1779 ✔ | RP2354 core reg in/out (per RP minimal) |
| C_1V2 | 4.7 µF | C1779 ✔ | W5500 1V2O (datasheet-mandated) |
| C_ANA | 10 nF | C1710 ✔ (0805) | W5500 analog cap (datasheet-mandated) |
| C_MCU × n | 100 nF each | C49678 ✔ | RP2354 IOVDD/DVDD decoupling (per RP minimal) |
| C_ETH × n | 100 nF each | C49678 ✔ | W5500 AVDD/VDD decoupling |
| C_X1a/b | 15 pF ×2 | C1644 ⚠ *(value right, **0603** — breaks the 0805 rule)* | X1 (12 MHz) load caps |
| C_X2a/b | per X2 spec | *(verify)* | X2 (25 MHz) load caps |
| C_3V3 | 1 µF | C28323 ✔ | +3V3 local bypass |
| C_LDO | 1 µF in + 1 µF out | C28323 ✔ | U5 in/out (AP2114H) |
| RSET | 12.4 kΩ 1% | C218478 ✘ *(is 287 Ω — reselect)* | W5500 EXRES1 → AGND |
| R_G1 | 100 kΩ | C149161 ✘ *(no such part — reselect)* | Q1 gate pull-up → +5V_POE |
| R_G2 | 100 kΩ | C149161 ✘ *(no such part — reselect)* | Q2 gate pull-down → GND |
| C_G | 100 nF | C49678 ✔ | Q1 gate soft-start |
| R_TE | 10 kΩ | C17414 ✔ | TE pull-down → GND (`R-HW-040`) |
| R_DC | 10 kΩ | C17414 ✔ | DC pull-up → +3V3 (`R-HW-040`) |
| R_ATN,R_REN,R_IFC,R_SRQ | 10 kΩ ×4 | C17414 ✔ | de-assert bias → +3V3 (`R-HW-040`/`041`) |
| R_CFG | 10 kΩ | C17414 ✔ | CFG (GPIO28) defined state (`R-HW-065`) |
| R_CC1, R_CC2 | 5.1 kΩ ×2 | C23186 ⚠ *(value right, **0603**)* | USB-C CC pull-downs (device role) |
| R_PE | 0 Ω (opt pad) | C17168 ⚠ *(value right, **0402**)* | U3 PE→+5V_XCVR (3-state, §29) |
| R_RUN, R_BOOT | 10 kΩ ×2 | C17414 ✔ | RUN pull-up / BOOTSEL (per RP minimal) |
| SW_RUN, SW_BOOT | tact | *(verify)* | reset / BOOTSEL buttons |
| LED1 (+R_LED) | status LED + 1 kΩ | C2286 ⚠ *(**0603** LED)* / C17513 ✔ | status LED (`R-HW-064`), on a GPIO |
| R_SH / C_SH | 0 Ω / RC (opt) | C17168 ⚠ *(**0402**)* | connector shield → chassis (§31) |
| TP_* | test pads | — | TE,DC,ATN,DAV,NRFD,NDAC,SRQ,IFC,5V,3V3,GND (§32) |

### 4.3 LCSC verification log (2026-09-21)

Every number above was queried against the EasyEDA/LCSC component API before any
symbol was pulled; `✔` means the API returned the expected manufacturer part and
an 0805-or-larger package, `⚠` the right value in a package smaller than the
§0.4 hand-assembly rule, `✘` a number that does not describe the intended part at
all. Seven of the twenty checked were wrong, so **no number in this document may
be trusted without a query** — including the ones that were never marked *(verify)*.

| Number | Spec claimed | API returned | Verdict |
|---|---|---|---|
| C41378174 | RP2354A | RP2354A, QFN-60 | ✔ |
| C32843 | W5500 | W5500, LQFP-48 | ✔ |
| C139410 | SN75161BDW (160/161 unsure) | SN75161BDW, SOIC-20 | ✔ — it is the **161** |
| C166063 | AP2114H-3.3TRG1 | **SK1117-1.5** (SHIKUES), a **1.5 V** LDO | ✘ **wrong rail** |
| C150716 | — | AP2114H-3.3TRG1 (Diodes), SOT-223 | ✔ **use this for U5** |
| C53065982 | — | DP9900LPB-5V1.4A (SDAPO), 5 V/1.4 A PD module, 21.2 × 13.9 × **8.0 mm** from its 3D model | ✔ **use this for M1** |
| C53065979 | — | DP9900MTB-5V1.8A, same footprint, 13.4 mm tall | ⚠ rejected on height |
| C53065972 | — | DP1425-5V1.8A, bridges included, single-row edge-mount SIP | ⚠ rejected: height undocumented |
| C85619 | — | MB6S-TP (MCC), 600 V/0.5 A bridge, MBS 4.7 × 4.0 mm | ✔ **use this for D_BR1/D_BR2** |
| C6186 | AMS1117-3.3 (alt) | AMS1117-3.3, SOT-223 | ✔ |
| C15127 | AO3401 | AO3401A, SOT-23 | ✔ |
| C8545 | 2N7002 | 2N7002, SOT-23 | ✔ |
| C165948 | TYPE-C-31-M-12 | TYPE-C-31-M-12 | ✔ |
| C7217089 | AR11-3757I MagJack | *not in catalogue* | ✘ |
| C485802 | — | TPS3839G33DBZR (TI), SOT-23-3, `VIT-` 3.08 V, push-pull | ✔ **use this for U6** |
| C19725138 | — | DGUK411Q211DB2A1DP2 (USAKRO), 2.5G PoE+ RJ45 TH, 22 pins | ⚠ **J2 candidate**, pinout unconfirmed |
| C49678 | 100 nF | CC0805KRX7R9BB104, 0805 | ✔ |
| C17414 | 10 kΩ | 0805W8F1002T5E, 0805 1 % | ✔ |
| C1779 | 4.7 µF | CL21A475KAQNNNE, 0805 | ✔ |
| C28323 | 1 µF | CL21B105KBFNNNE, 0805 | ✔ |
| C1710 | 10 nF | CL21B103KBANNNC, 0805 | ✔ |
| C17513 | 1 kΩ | 0805W8F1001T5E, 0805 | ✔ |
| C23186 | 5.1 kΩ | 0603WAF5101T5E, **0603** | ⚠ |
| C17168 | 0 Ω | 0402WGF0000TCE, **0402** | ⚠ |
| C1644 | 15 pF | CL10C150JB8NNNC, **0603** | ⚠ |
| C2286 | status LED | KT-0603R, **0603** | ⚠ |
| C218478 | 12.4 kΩ 1 % | ARG05FTC2870 = **287 Ω** | ✘ |
| C2011903 | 47 µF 16 V | 653L16003C2T = **16 MHz oscillator** | ✘ |
| C149161 | 100 kΩ | *not found* | ✘ |

**The C166063 error was the dangerous one.** It was recorded as confirmed, not
*(verify)*, and it would have put a **1.5 V** regulator on the rail that feeds
IOVDD, every bias network and the W5500 — the board would not run, and the
`R-HW-020` gate precondition ("+3V3 ≥ 3.08 V") could never be met. Correct part:
**C150716**.

Still to source before the BOM is releasable: U3 (SN75160BDW), J1 (IEEE-488
connector), X1/X2 (12 MHz / 25 MHz crystals), L1 (3.3 µH), C_BULK (47 µF), RSET
(12.4 kΩ 1 %), R_G1/R_G2 (100 kΩ), and 0805 replacements for the four `⚠` parts.
The schematic can be authored ahead of these: U3 has a stock KiCad symbol, J1 has
an authored one, and the rest are generic `Device:` symbols whose footprint
assignment is the only thing that waits on the number.

### 4.4 Open hardware questions (blocking the Ethernet/PoE sheet)

**1. The PD front-end is now specified: SDAPO DP9900LPB-5V1.4A (C53065982) plus
two MB6S bridges (C85619).** Revision 2 delegated the whole 802.3af
powered-device function to the WIZPoE-P1, a WIZnet accessory for WIZnet's own
boards, now withdrawn. The replacement covers every function the standard
requires of a PD, but it is worth being precise about where each one lives,
because the module's own marketing is easy to misread:

- **inside M1**: the 25 kΩ detection signature, the classification current
  signature (programmable class), inrush and operating current limit, the
  hot-swap switch, 1500 V galvanic isolation, and the 48 V → 5 V conversion;
- **outside M1, ours to provide**: the **bridge rectifiers**. The DP9900 series
  presents `VIN+`/`VIN−` as *Direct Input* pins that, per its pin table,
  "connect to the … output of the input bridge rectifiers" — i.e. the bridges
  are external. D_BR1 and D_BR2 supply them, one per pair set, so the board
  accepts Alternative A and B feeds in either polarity.

The isolation living inside M1 is not an optimisation: the IEEE-488 shell bonds
to the instrument chassis and J3 is a second external interface, so the barrier
must sit upstream of board ground.

**Why this part.** It was selected on **height**, which is what a dongle
enclosure actually constrains — not footprint area, as an earlier revision of
this section wrongly implied. Verified from the part's own 3D model at
**21.2 × 13.9 × 8.0 mm**, matching Silvertel's published 21 × 14 × 8 mm for the
equivalent Ag9900LPB. The alternatives measured or reported as: DP9900MTB
(C53065979) 21.2 × 13.9 × **13.4 mm**, same footprint, 5.4 mm taller; DP1425
(C53065972) bridges-included but a single-row edge-mount SIP whose height is
documented only as a drawing, hence unverifiable and plausibly 15–25 mm. 8.0 mm
is about the height of the IEEE-488 connector body, so M1 fits under the same lid
rather than setting it.

Being dimensionally and functionally interchangeable with **Silvertel
Ag9905-LPB** gives a second source, which matters on a board that already
carries an EOL risk for U3/U4 (design-spec.md §67, R24).

7 W against the §3.3 load of 2.75 W is 2.5× margin, and the programmable class
means the board can declare Class 1 rather than reserving a PSE's full budget.

**The documented path to smaller and cheaper** is MPS **MP8017**: an 802.3af PD
interface (detection, classification, inrush and operating current limit, 100 V
hot-swap MOSFET) *plus* an active-clamp primary-side-regulation flyback
controller in a QFN-19 3 × 4 mm, with an MPS 5 V reference design
(EVL8017-L-00B). It is the right answer for a volume revision or if 8 mm is ever
too tall, at the cost of owning the transformer selection, the isolation-barrier
layout and the EMC — risk that buys nothing on a v1 whose purpose is proving the
protocol stack.

**Stock is unverified** for both M1 and the bridges: specifications, geometry and
pinouts were read from datasheets, the LCSC/EasyEDA catalogue and the parts' own
3D models, none of which report inventory. Confirm availability before release.

**2. J1's gender is settled: male, and the adapter is a direct-plug dongle.**
J1 is a **FUYCONN FUY57109A**, Centronics-24 male, right-angle PCB mount, which
plugs straight into the instrument's female GPIB port — the form the original
specification intended and the one commercial GPIB-Ethernet adapters take.

Two consequences follow.

*The vendored footprint cannot be used.* `gpib-kicad-library` supplies L-com
**female** receptacles only. Male and female pad numbering are mirror images, so
building on the female pattern would put every DIO line on the wrong pin — the
one error on this sheet that produces a board which looks right and is
electrically scrambled. J1's footprint is therefore **deliberately unassigned**
until FUYCONN's PCB drawing for this part is in hand. The symbol is unaffected:
the IEEE-488 pinout is gender-independent, and it was cross-checked against §5.4
pin for pin.

*Height is the binding enclosure constraint.* A dongle hangs off the instrument's
port, so the lid clearance is set by the tallest part, not by board area. This is
what selected the 8.0 mm DP9900LPB over the 13.4 mm MTB in item 1.

**J1's footprint is now resolved: `adapter:CONN_112-024-113R001_NRC`.** The part is
a **NorComp 112-024-113R001**, the male alternate this specification already
named, and its vendor-supplied KiCad footprint was measured before adoption:

- 24 through-hole pads, 0.99 mm drill, **pins 1–12 on one row and 13–24 on the
  other** — the IEEE-488 row split, not a header's alternating order;
- **2.16 mm** pitch along each row, rows **4.29 mm** apart;
- two non-plated **3.1 mm** mounting holes at x = −11.52 and +35.28, a
  **46.8 mm** span;
- courtyard present; no 3D model supplied.

Those numbers agree with the vendored L-com female footprint, and the two are
**mirror images** of each other: the NorComp male runs pin 1 → 12 in increasing x
with holes at −11.52/+35.28, the L-com female runs 1 → 12 in decreasing x with
holes at −35.28/+11.52. Two vendors, two independently sourced files, agreeing on
the grid and differing exactly by the mirror the gender change predicts. That is
the check that the earlier revision of this section said was required, and it is
now satisfied by measurement rather than assumption.

The generic `PinHeader_2x12_P2.54mm_Horizontal` placeholder that occupied this
slot has been replaced; nothing in the design references it any more.

FUYCONN FUY57109A remains recorded as the alternate, but **its footprint has not
been verified** — the vendor's PCB drawing was not obtainable — so substituting
it requires repeating the measurement above.

**3. J2 centre-tap mapping — CLOSED 2026-09-22 by documentation.**

Resolved from the connector's own datasheet (LCSC C19725138, Dongguan Usakro,
sheet 3) and the WIZnet WIZPoE-P1 datasheet; no ring-out needed.

The connector drawing labels its two columns "PCB SIDE TO PHY" and "CABLE SIDE",
and the cable side carries `J1 TX1+` .. `J8 TX4-`, i.e. the eight RJ45 contacts.
The turns-ratio notes give every transformer as `1CT:1CT`, so both windings have
centre taps: the four `CT` pins (1, 6, 7, 12) are the **PHY-side** taps and
`VC1`-`VC4` (13-16) are the **cable-side** taps, which is where PoE is extracted.
Note 8 confirms the part is 802.3at magnetics "for PSE or PD applications", rated
720 mA / 57 V DC continuous.

The WIZPoE-P1 datasheet pin table fixes the Alternative mapping outright:

| Module pin | Connects to the centre tap of | Alternative |
|---|---|---|
| `VC1 (+)` | the pair on RJ45 **1 & 2** | A |
| `VC1 (-)` | the pair on RJ45 **3 & 6** | A |
| `VC2 (+)` | the pair on RJ45 **4 & 5** | B |
| `VC2 (-)` | the pair on RJ45 **7 & 8** | B |

each "not polarity sensitive" and feeding the +/- of an input bridge rectifier.
This is why `D_BR1`/`D_BR2` exist and why `POE_VIN_N` is not ground: neither the
Alternative nor the polarity is known in advance. Our `POE_TAP_A1/A2` therefore
take `VC1`/`VC2` and `POE_TAP_B1/B2` take `VC3`/`VC4`, as wired.

The Bob Smith network (`4*22nF`, `4*75R`, `1000pF/2kV` to `SHIELD`) is internal to
this connector - confirmed against the drawing - so no external termination or
shield capacitor is fitted. The series 22 nF per tap is what stops the two
pair-sets being DC-coupled through the common 75 R node, which is what makes the
`VC` pins usable for PoE at all.


**This is convergent evidence, not the part's own datasheet**, so it is not yet
sufficient to wire 48 V. Two things remain unproven: the `CT`/`VC` split itself,
and *which* `VC` belongs to *which* RJ45 pair — the latter matters because each
bridge must see the two taps of one Alternative (A = pairs 1&2 and 3&6, B = pairs
4&5 and 7&8). Mixing a pair-set across bridges is a wiring defect.

**Ring-out procedure (decisive, needs one sample and an ohmmeter).** Each tap sits
at the electrical centre of one transformer winding, so it reads a fraction of an
ohm to *both* ends of that winding and open to the other side of the barrier:

- a **cable-side** tap reads ≈0.5–2 Ω to both RJ45 contacts of its pair (e.g.
  contacts 1 and 2) and open to that pair's `TD±` pins;
- a **PHY-side** tap reads ≈0.5–2 Ω to both `TD+` and `TD−` of its pair and open
  to the RJ45 contacts.

The same measurement also yields the pair mapping, since it identifies which RJ45
contacts each tap belongs to. Record the result here before the Ethernet sheet is
drawn.

**Safety rule regardless of outcome:** the PHY-side taps must never connect to the
PoE feed. They sit on the isolated side referenced to board ground, and bridging
them to the cable-side taps would short across the isolation barrier that M1
provides — defeating the property that keeps 48 V off the instrument chassis
through the IEEE-488 shell.

One more property of this jack worth carrying forward: it is a 4-pair 2.5G part
while the W5500 is 10/100. The extra pairs are harmless and in fact allow both
Alternative A and B to be tapped, but the magnetics' open-circuit inductance
should be checked against the W5500's requirement when §5.6 is entered.

---

### 4.5 Silent-failure modes observed in KiCad 10 / Konnect (2026-09-22)

Three defects in this session were invisible in the schematic editor's normal
feedback and were caught only by an explicit check. Each is a class of mistake
that produces a plausible-looking sheet and a wrong netlist, so each is recorded
with the check that detects it.

**1. Wires that miss sheet pins by a grid snap.** `import_sheet_pins` places
pins at half-grid positions (x = 150.00, y = 42.54), while wire endpoints are
snapped to the 1.27 mm grid. A wire drawn to a pin's exact coordinates therefore
lands at (149.86, 41.91) and does not connect. Nothing complains: the wires are
drawn, they simply conduct nothing, and the two sheets stay on separate nets with
identical net names. Detected by `validate_wire_connections` (36 floating
endpoints) and by two nets named `TE` in one exported netlist. Fixed by placing
each sheet's origin on the 1.27 mm grid, which makes every pin grid-aligned
because the pin offsets are multiples of 2.54 mm.

**2. Sheet pins outside the sheet outline are silently merged.** `import_sheet_pins`
stacks new pins below the existing ones without growing the sheet box. Six SPI
pins imported onto a 55 mm-tall block put the last three at y = 176.53, 179.07
and 181.61 against a bottom edge at 175.65. KiCad's netlister collapsed all
three into one net: `ETH_MOSI` and `ETH_RSTn` disappeared and `ETH_INTn` carried
GPIO19, GPIO20 and GPIO21 together — three SPI signals shorted. Konnect's own
connectivity model disagreed with KiCad throughout, reporting the three nets as
correctly separate, so the schematic *looked* right from inside the tool that
drew it. Detected by the netlist gate. Fixed by enlarging the sheet block to
contain every pin. **Check pin extents against the block after every
`import_sheet_pins` call.**

**3. Duplicate `#PWR` designators across sheets.** `add_power_symbol` numbers
`#PWR` references per sheet, so a power symbol added to a second sheet reuses
`#PWR001`. The only symptom is `kicad-cli`'s generic "schematic has annotation
errors" warning on the *root* export; each sheet exports cleanly on its own, ERC
reports nothing, and `annotate_schematic` reports zero unannotated symbols
because it does not detect duplicates spread across sheets. Ruled out before
finding it: unannotated instances, reference-prefix mismatches (confirmed
irrelevant by aligning every prefix on a scratch copy — the warning persisted),
and malformed instance paths (the `/<root-uuid>/<sheet-uuid>` form is correct).
Located by bisection: removing either of two sheets cleared the warning, which
is the signature of a duplicate pair. Detect with a cross-sheet scan of
`(reference "...")` outside the `lib_symbols` block; renumber the later sheet's
symbols.

## 5. Net list

Decision/safety subsystems are pin-precise; MCU/ETH support blocks cite their
reference. Pins are `REF.pin`; U1/U2 signal pins are named by function/GPIO as they
appear in the pulled `easyeda2kicad` symbols.

### 5.1 Power and gate nets (pin-precise)

| Net | Members | Safety |
|---|---|---|
| `+5V_POE` | M1.OUT, U5.IN, Q1.S, R_G1.a, C_BULK.+, C_LDOin.+, TP_5V | WIZPoE-P1 output |
| `+3V3` | U5.OUT, U1.IOVDD(×n), U1.VREG_VIN, U2.AVDD/VDD(×n), U6.VDD, RSET pull side n/a, R_DC.a, R_ATN.a, R_REN.a, R_IFC.a, R_SRQ.a, R_CFG.a, R_RUN.a, C_3V3.+, C_LDOout.+, all C_MCU/C_ETH.+, TP_3V3 | IOVDD; **bias reference — never +5 V** |
| `+5V_XCVR` | Q1.D, U3.VCC(20), U4.VCC(20), R_PE.a, C_U3a.+, C_U4a.+, C_U3b.+, C_U4b.+ | **`R-HW-020`/`044`**: off until 3V3-good |
| `GATE` | Q1.G, R_G1.b, C_G.a, Q2.D | Q1 high-side gate node |
| `PG_3V3` | U6.RESET, Q2.G, R_G2.a | **`R-HW-020`**: 3V3-good drives Q2; `R_G2` fail-safe |
| `GND` | U1.GND(×n)+VREG_PGND, U2.AGND(×n), U3.GND(10), U4.GND(10), U5.GND, U6.GND, Q2.S, M1.GND, RSET.b, R_TE.b, R_G2.b, C_G.b, all C.−, J1.18–24, J2 shield/GND, J3.GND/shield, TP_GND | single local ground (design-spec.md §31) |

### 5.2 Control nets (pin-precise)

| Net | Members | Safety |
|---|---|---|
| `TE` | U1.GPIO26, U3.TE(1), U4.TE(1), R_TE.a, TP_TE | **`R-HW-040`** TE=L |
| `DC` | U1.GPIO27, U4.DC(11), R_DC.b, TP_DC | **`R-HW-040`** DC=H |
| `PE` | U3.PE(11), R_PE.b | §29 3-state select |

### 5.3 GPIB data path (U3 SN75160B) — pin-precise, `R-HW-060`

DIO1..DIO8 = GPIO0..GPIO7 (contiguous, DIO1 lowest). U3 pins: TE=1, B1..B8=2..9
(bus), GND=10, PE=11, D8..D1=12..19 (terminal), VCC=20.

| Terminal net | Members | Bus net | Members |
|---|---|---|---|
| DIO1_T | U1.GPIO0, U3.19 | DIO1_B | U3.2, J1.1 |
| DIO2_T | U1.GPIO1, U3.18 | DIO2_B | U3.3, J1.2 |
| DIO3_T | U1.GPIO2, U3.17 | DIO3_B | U3.4, J1.3 |
| DIO4_T | U1.GPIO3, U3.16 | DIO4_B | U3.5, J1.4 |
| DIO5_T | U1.GPIO4, U3.15 | DIO5_B | U3.6, J1.13 |
| DIO6_T | U1.GPIO5, U3.14 | DIO6_B | U3.7, J1.14 |
| DIO7_T | U1.GPIO6, U3.13 | DIO7_B | U3.8, J1.15 |
| DIO8_T | U1.GPIO7, U3.12 | DIO8_B | U3.9, J1.16 |

### 5.4 GPIB management path (U4 SN75161B) — pin-precise, `R-HW-061`

EOI,DAV,NDAC,NRFD = GPIO12–15 (consecutive, one PIO SM). ATN,SRQ,REN,IFC =
GPIO8–11 (Go-driven). U4 pins: TE=1, bus 2–9 (REN2 IFC3 NDAC4 NRFD5 DAV6 EOI7 ATN8
SRQ9), GND=10, DC=11, terminal 12–19 (SRQ12 ATN13 EOI14 DAV15 NRFD16 NDAC17 IFC18
REN19), VCC=20.

| Terminal net | Members | Bus net | Members | Safety |
|---|---|---|---|---|
| ATN_T | U1.GPIO8, U4.13, R_ATN.b | ATN_B | U4.8, J1.11 | `R-HW-041` |
| SRQ_T | U1.GPIO9, U4.12, R_SRQ.b | SRQ_B | U4.9, J1.10 | `R-HW-040` |
| REN_T | U1.GPIO10, U4.19, R_REN.b | REN_B | U4.2, J1.17 | `R-HW-041` |
| IFC_T | U1.GPIO11, U4.18, R_IFC.b | IFC_B | U4.3, J1.9 | `R-HW-041` |
| EOI_T | U1.GPIO12, U4.14 | EOI_B | U4.7, J1.5 | |
| DAV_T | U1.GPIO13, U4.15 | DAV_B | U4.6, J1.6 | |
| NDAC_T | U1.GPIO14, U4.17 | NDAC_B | U4.4, J1.8 | |
| NRFD_T | U1.GPIO15, U4.16 | NRFD_B | U4.5, J1.7 | |

IEEE-488 J1 (Amphenol-57 numbering): 1-4 DIO1-4, 5 EOI, 6 DAV, 7 NRFD, 8 NDAC,
9 IFC, 10 SRQ, 11 ATN, 12 shield, 13-16 DIO5-8, 17 REN, 18-23 twisted-pair returns
(→GND), 24 signal ground (→GND).

### 5.5 MCU support block (U1 RP2354A) — build from RP2350-minimal

Enter per RP "Hardware design with RP2350" / RP2350A Minimal, **omitting the flash
and R10** (RP2354 internal flash). Key nets:

- **Core SMPS:** `U1.VREG_VIN`→+3V3; `U1.VREG_LX`→`L1` 3.3 µH→**`DVDD`** (the
  ≈1.1 V core rail, pins 6/23/39) with `C7` 4.7 µF on DVDD; `U1.VREG_FB`→`DVDD`
  as the sense node; `U1.VREG_PGND`→GND; `U1.VREG_AVDD`→+3V3. L1 orientation per
  RP guidance (the magnetic field couples into C7 if it is fitted the wrong way
  round — "Hardware design with RP2350" §2.5).

  **Corrected 2026-09-21.** Earlier revisions of this line read
  "`VREG_LX`→`L1`→`+3V3` (core node)", which is wrong and destructive: it would
  tie the 3.3 V rail through the inductor to the DVDD pins and feed the ≈1.1 V
  core from 3.3 V. Raspberry Pi's "Hardware design with RP2350" is explicit that
  the regulator produces "a 1.1 V output to supply the DVDD on the chip", with
  L1 and C7 as its output filter and VREG_FB monitoring that output. DVDD is its
  own net and must never be joined to +3V3.
- **Clock:** `X1` 12 MHz between `U1.XIN`/`U1.XOUT`, `C_X1a/b` 15 pF to GND.
- **Decoupling:** 100 nF (`C_MCU`) at every IOVDD/DVDD pin; bulk per minimal.
- **USB:** `U1.USB_DP`/`U1.USB_DM` → `J3` D+/D−.
- **Boot/reset:** `SW_BOOT` on `QSPI_CSn` (+`R_BOOT`); `U1.RUN`←`R_RUN` pull-up to
  +3V3 + `SW_RUN` to GND.
- **Status LED:** `LED1` + `R_LED` on a spare GPIO (e.g. GPIO25); GPIO recorded in
  the board package (`R-HW-064`).
- **SPI to W5500:** GPIO16 MISO, GPIO17 CSn, GPIO18 SCK, GPIO19 MOSI, GPIO20 W5500-RST,
  GPIO21 W5500-INT. Carried off-sheet as hierarchical labels `ETH_MISO`, `ETH_CSn`,
  `ETH_SCK`, `ETH_MOSI`, `ETH_RSTn`, `ETH_INTn` (per the globals-for-rails,
  hierarchical-for-signals rule).
- **SWD debug (added 2026-09-22, not in revision 2).** `U1.SWCLK` and `U1.SWDIO`
  are brought out to test pads `TP_SWCLK1`/`TP_SWDIO1`. The revision-2 net table
  was silent on debug access; a first prototype with no way to attach a probe is
  a false economy, and two pads cost nothing. The alternative — declaring both
  pins `!nc` — was rejected for that reason. Not an SWD header: this is a plug-in
  dongle and the height budget is set by the Centronics connector.
- **USB-C role:** `R_CC1`/`R_CC2` 5k1 from `J3.CC1`/`J3.CC2` to GND (upstream
  facing port / device role). Both `DP1`/`DP2` are tied to `USB_DP` and both
  `DN1`/`DN2` to `USB_DM` so the cable works either way up.

  **`VBUS` is a declared non-connection.** Both VBUS contacts carry `!nc`. The
  board is powered from PoE only; bridging VBUS to `+5V_POE` or `+3V3` would
  backfeed the PoE supply from a host, or the host from the PSE, in violation of
  design-spec §30.1. Bench-powering over USB is deliberately not supported.


**Analogue rail and magnetics, added 2026-09-22 from the WIZnet W5500-EVB-Pico2
reference (SCH-W5500-EVB-Pico2-V100, rev 1.0, 2024-08-01).** The W5500 datasheet
carries no application schematic - 27 figures, none showing the magnetics bias -
so the following is taken from WIZnet's own board, which pairs a W5500 with an
RP2350 and is therefore the same architecture as this design:

- **Separate filtered analogue rail.** `+3V3` feeds ferrite `L2` to make `3V3A`;
  every `AVDD` pin (4, 8, 11, 15, 17, 21) and the magnetics bias run from `3V3A`,
  while `VDD` (28) stays on the unfiltered rail. `PWR_FLAG` marks `3V3A` as driven.
- **Series resistors on the MDI pairs.** `R_TX1`/`R_TX2`/`R_RX1`/`R_RX2`, 3R3 each,
  between the W5500 and the connector (`R15`-`R18` in the reference).
- **Bias/termination.** `R_B1` 10R from `3V3A` to `ETH_BIAS`, decoupled by
  `C_BIAS1` 100n, with `R_T1`-`R_T4` 49R9 from that node to each MDI pin
  (`R28`, `C30`, `R19`-`R22`).
- **Pull-ups the reference does not leave to the internal ones:** `R_RSTP1` 4k7 on
  `ETH_RSTn` and `R_INTP1` 4k7 on `ETH_INTn` (`R12`, `R32`/`R33`).
- **Crystal.** Loads corrected 22p -> **18p** to match the datasheet's stated 18 pF
  load capacitance, plus `R_XF1` 1M feedback across the crystal (`R24`).
- **PHY-side centre taps.** `ETH_TCT` (J2.12, the RJ45 1&2 pair) and `ETH_RCT`
  (J2.6, the 3&6 pair) are AC-grounded through 22n, following `C28`. The taps of
  the two pairs unused at 10/100 (J2.1, J2.7) are declared open.

Deliberately **not** copied from the reference: its jack uses an *external* Bob
Smith network (`C31` 1nF/2kV on the jack's GND pin) whereas ours has it internal,
so no shield capacitor or 75 R network is fitted here.

Independently confirmed by the reference, previously only inferred: the SPI GPIO
map is identical (GPIO16 MISO, 17 CSn, 18 SCK, 19 MOSI, 20 RSTn, 21 INTn);
`EXRES1` 12k4 1% to ground; `TOCAP` 4u7; `VBG` left floating; PMODE[2:0] strapped
high; 25 MHz crystal; a status LED on GPIO25. The reference also brings SWD out on
a 3-pin header, supporting the test-pad decision recorded in 5.5.

### 5.6 Ethernet + PoE block (U2 W5500, J2, M1) — build from W5500 reference

Enter per the W5500 datasheet Figure 3 + a W5500 KiCad reference. Key nets:

- **Supplies:** all AVDD/VDD → +3V3, each with 100 nF (`C_ETH`); `U2.1V2O`→`C_1V2`
  4.7 µF; analog `C_ANA` 10 nF per datasheet; AGND → GND.
- **Bias:** `U2.EXRES1`→`RSET` 12.4 kΩ 1% → GND.
- **Clock:** `X2` 25 MHz between `U2.XI`/`U2.XO` + load caps.
- **PHY↔magnetics:** `U2.TXOP/TXON/RXIP/RXIN` → `J2` transceiver-side pairs; centre-tap
  bias/termination per W5500 reference; `ACTLED`/`DUPLED` → J2 LEDs.
- **SPI:** to U1 GPIO16–21 (§5.5).
- **PoE tap → M1:** `J2` RX centre tap (pins 1&2) and TX centre tap (pins 3&6) →
  `M1` VC1(±); `J2` spare pairs (4&5, 7&8) → `M1` VC2(±). `M1.OUT` → `+5V_POE`,
  `M1.GND` → GND. WIZPoE-P1 provides the bridge rectifiers and isolation.

### 5.7 USB-C (J3) — pin-precise where it matters

- `J3` D+/D− → U1 USB (§5.5); `R_CC1`/`R_CC2` 5.1 kΩ from CC1/CC2 → GND (device).
- **`J3.VBUS` MUST NOT connect to `+5V_POE` or any power rail** (design-spec.md §30.1);
  leave for USB-powered flashing on the bench only. Shield → chassis/GND per §31.

### 5.8 Test points (§32)

`TP_TE→TE, TP_DC→DC, TP_ATN→ATN_B, TP_DAV→DAV_B, TP_NRFD→NRFD_B, TP_NDAC→NDAC_B,
TP_SRQ→SRQ_B, TP_IFC→IFC_B, TP_5V→+5V_POE, TP_3V3→+3V3, TP_GND→GND`.

---

## 6. Explicit non-connections

| Pin / net | Status | Reason |
|---|---|---|
| U1 external QSPI flash | **omitted** | RP2354A internal flash; remove flash IC + R10 (RP HD guide §3) |
| U1 QSPI_SD0-3 / SCLK | not routed externally | internal to RP2354 flash |
| `J3.VBUS` | **NC to power rails** | no PoE↔USB backfeed (design-spec.md §30.1) |
| U1 GPIO22 | test pad only | `R-HW-065` spare |
| U1 GPIO28 (CFG) | `R_CFG` to +3V3 | defined state (`R-HW-065`) |
| U3/U4 | all 20 pins each used | 8 bus + 8 term + TE + (PE\|DC) + VCC + GND |
| J1 | all 24 pins used | 16 signal + shield(12) + 7 grounds |
| UART0 (GPIO0/1) | not a console | GPIO0/1 are DIO1/DIO2; diagnostics = USB CDC (`R-HW-063`) |

---

**Added 2026-09-22, MCU sheet.** Each of these carries a no-connect flag on the
schematic and a matching `!nc` line in `netlist-spec.nets`; the checker fails if a
pin is left floating that nobody declared open.

| Pin(s) | Reason |
|---|---|
| `U1.55`–`U1.60` (QSPI SD3/SCLK/SD0/SD2/SD1/SS) | RP2354A has 2 MB stacked internal flash; the QSPI bus is not brought out. This is the `R10`/external-flash omission of §5.5 made explicit. |
| `U1.35`, `U1.36` (GPIO23, GPIO24) | Unused. The GPIO budget in §5.2 is fully assigned without them. |
| `U1.43` (GPIO29/ADC3) | Unused; no analogue input in this design. |
| `J3.A4B9`, `J3.B4A9` (VBUS ×2) | No PoE↔USB backfeed — see §5.5. |
| `J3.B8`, `J3.A8` (SBU2, SBU1) | Sideband unused; USB 2.0 device only. |

## 7. Requirement traceability (`R-HW-010`..`R-HW-065`)

| Req | Implemented by | Verified by |
|---|---|---|
| `R-HW-010` | Silkscreen/enclosure "System Controller — no second controller" | §64 inspection |
| `R-HW-011` | Product documentation | §64 doc |
| `R-HW-012` | Firmware IFC/arbitration detect → status LED (LED1) + diagnostics | §64, firmware |
| `R-HW-020` | Q1 gate on `+5V_XCVR`, driven by U6→Q2 via `PG_3V3`; `R_G1`/`R_G2` fail-safe | §64 measurement |
| `R-HW-021` | (val) measure across PoE up/down/brown-out/USB changeover; §5.8 test points | §64 report |
| `R-HW-022` | U6 `VIT-` 3.0–3.08 V; ramp via `R_G1`·`C_G`; compatible with §41/`R-FW-110` | §64 doc |
| `R-HW-030` | External bias only (`R_TE/DC/ATN/REN/IFC/SRQ`); **E9 fixed by RP2354 A4 stepping** | §64, firmware |
| `R-HW-031` | Turnaround intervals covered by bias; E9 latch hazard removed on A4 | §64 analysis |
| `R-HW-032` | (val) record RP2354 stepping (A4) of validated + production units | §64 report |
| `R-HW-040` | `R_TE`→GND (TE=L), `R_DC`→+3V3 (DC=H), `R_SRQ`→+3V3 | §64 scope |
| `R-HW-041` | `R_ATN/R_REN/R_IFC`→+3V3 on terminal nets | §64 scope |
| `R-HW-042` | Passive bias + Q1 gating cover all five conditions | §64 scope |
| `R-HW-043` | (val) scope ATN/IFC/REN/DAV/DIO in each condition | §64 report |
| `R-HW-044` | Q1 VCC gating primary; §3.2 bias secondary | §64 |
| `R-HW-050` | Power budget §3.3 | §64 measurement |
| `R-HW-051` | `C_BULK` 47 µF + Q1 `R_G1`·`C_G` soft-start; §3.3 | §64 |
| `R-HW-052` | (val) measured figures replace estimates | §64 report |
| `R-HW-053` | (val) thermal check incl. U5 0.31 W hot spot in sealed enclosure | §64 report |
| `R-HW-060` | DIO1–8 = GPIO0–7 contiguous, DIO1 lowest (§5.3) | §64 inspection |
| `R-HW-061` | EOI,DAV,NDAC,NRFD = GPIO12–15 (§5.4) | §64 inspection |
| `R-HW-062` | (doc) GPIO0–15 map load-bearing for PIO | §64 doc |
| `R-HW-063` | GPIO0/1 = DIO1/2 → USB CDC diagnostics (§5.5, §6) | §64, §65 |
| `R-HW-064` | Status LED1 on a spare GPIO, recorded in board package (§5.5) | §64 inspection |
| `R-HW-065` | GPIO22 test pad; GPIO28 `R_CFG` defined state | §64 inspection |

All twenty-four `R-HW-0xx` are covered. Sourcing risk for U3/U4 (EOL) is
design-spec.md §67 **R24** (mitigated by a hundreds-quantity buy).

---

## 8. Netlist verification procedure

As Revision 1: KiCad 7.0.11 cannot run ERC, but exports a netlist.

1. Pull footprints/symbols by LCSC number: `easyeda2kicad --full --lcsc_id=Cxxxxx`.
2. Enter the schematic (decision subsystems from §5.1–§5.4/§5.7; MCU/ETH blocks
   from the RP2350-minimal and W5500 reference designs).
3. Export: `kicad-cli sch export netlist --format kicadsexpr -o build/adapter.net adapter.kicad_sch`.
4. Check: `scripts/netlist-check.py docs/netlist-spec.nets build/adapter.net`.

`docs/netlist-spec.nets` is the machine-readable form of the **decision/safety
subsystems** (power/gate/bias/GPIB/connector, §5.1–5.4/5.7) — the parts where a
mis-wire is a safety defect. `scripts/netlist-check.py` validates spec
self-consistency now (every listed device pin assigned once; GPIO contiguity for
`R-HW-060`/`061`; no bias net on a 5 V rail; every `R-HW-0xx` covered) and, once
`adapter.net` exists, diffs those nets against the export. The MCU and Ethernet
support blocks are validated against their reference designs rather than the
canonical net table, per §1.

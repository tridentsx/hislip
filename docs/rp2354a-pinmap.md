# RP2354A (U1) Pin Map — PoE-to-GPIB / HiSLIP Adapter

Generated from the project netlist (`build/adapter.net`) and `docs/netlist-spec.nets`.
MCU: **RP2354A**, QFN-60 (7×7 mm), 2 MB stacked internal flash.
Role: bridges Ethernet (W5500, SPI) and USB to the IEEE-488 (GPIB) bus via
SN75160B (data) + SN75161B (control) transceivers.

Net names below are shown with their sheet prefix stripped. Signals with a `_T`
suffix are the **logic/MCU side** of the GPIB transceivers (the `_B` side faces
the IEEE-488 connector).

## Firmware GPIO map

| GPIO | Pin | Net | Dir (MCU) | Function / notes | Suggested peripheral |
|-----:|----:|-----|-----------|------------------|----------------------|
| GPIO0  | 2  | DIO1_T | I/O | GPIB data bit 1 (LSB) | PIO (8-bit parallel) |
| GPIO1  | 3  | DIO2_T | I/O | GPIB data bit 2 | PIO |
| GPIO2  | 4  | DIO3_T | I/O | GPIB data bit 3 | PIO |
| GPIO3  | 5  | DIO4_T | I/O | GPIB data bit 4 | PIO |
| GPIO4  | 7  | DIO5_T | I/O | GPIB data bit 5 | PIO |
| GPIO5  | 8  | DIO6_T | I/O | GPIB data bit 6 | PIO |
| GPIO6  | 9  | DIO7_T | I/O | GPIB data bit 7 | PIO |
| GPIO7  | 10 | DIO8_T | I/O | GPIB data bit 8 (MSB) | PIO |
| GPIO8  | 12 | ATN_T  | I/O | GPIB Attention | GPIO |
| GPIO9  | 13 | SRQ_T  | I/O | GPIB Service Request | GPIO |
| GPIO10 | 14 | REN_T  | I/O | GPIB Remote Enable | GPIO |
| GPIO11 | 15 | IFC_T  | I/O | GPIB Interface Clear | GPIO |
| GPIO12 | 16 | EOI_T  | I/O | GPIB End-Or-Identify | GPIO/PIO |
| GPIO13 | 17 | DAV_T  | I/O | GPIB Data Valid (handshake) | GPIO/PIO |
| GPIO14 | 18 | NDAC_T | I/O | GPIB Not Data Accepted (handshake) | GPIO/PIO |
| GPIO15 | 19 | NRFD_T | I/O | GPIB Not Ready For Data (handshake) | GPIO/PIO |
| GPIO16 | 27 | ETH_MISO | in  | W5500 SPI MISO | **SPI0 RX** |
| GPIO17 | 28 | ETH_CSn  | out | W5500 SPI chip-select (active low) | SPI0 CSn / GPIO |
| GPIO18 | 29 | ETH_SCK  | out | W5500 SPI clock | **SPI0 SCK** |
| GPIO19 | 31 | ETH_MOSI | out | W5500 SPI MOSI | **SPI0 TX** |
| GPIO20 | 32 | ETH_RSTn | out | W5500 reset (active low) | GPIO |
| GPIO21 | 33 | ETH_INTn | in  | W5500 interrupt (active low) | GPIO (IRQ) |
| GPIO22 | 34 | TP_SPARE | —   | spare, to test point TP_GPIO22 | GPIO (free) |
| GPIO23 | 35 | — (NC) | —   | **unused / available** | — |
| GPIO24 | 36 | — (NC) | —   | **unused / available** | — |
| GPIO25 | 37 | LED_DRV | out | Status LED (LED1, green) | GPIO/PWM |
| GPIO26 / ADC0 | 40 | TE | out | GPIB transceiver **Talk Enable** | GPIO |
| GPIO27 / ADC1 | 41 | DC | out | GPIB transceiver **Direction Control** | GPIO |
| GPIO28 / ADC2 | 42 | CFG | in | Board config strap (read at boot) | GPIO/ADC |
| GPIO29 / ADC3 | 43 | — (NC) | — | **unused / available** (ADC-capable) | — |

### GPIB direction control (must be driven by firmware)
The SN75160B/SN75161B transceivers are set by **TE** (GPIO26) and **DC** (GPIO27):
- **TE (Talk Enable)** — sets talk/listen direction of the data lines (DIO1–8) and
  the source-handshake lines (DAV/EOI) vs. acceptor-handshake (NRFD/NDAC).
- **DC (Direction Control)** — sets controller-vs-device direction of the
  management lines (ATN, IFC, REN, SRQ).

Drive TE/DC per the standard SN75160/SN75161 truth table for the desired
controller / talker / listener state **before** driving or reading the bus.
On reset they are held in the safe (receive) state by bias resistors
(R_TE, R_DC) so the bus is not driven until firmware takes over.

## Dedicated-function pins

| Signal | Pin(s) | Net | Notes |
|--------|--------|-----|-------|
| SWCLK | 24 | SWDIO/SWCLK debug | SWD debug (to test points) |
| SWDIO | 25 | SWDIO | SWD debug |
| RUN   | 26 | RUN | Reset (active low), pulled up by R_RUN |
| XIN   | 21 | XIN | 12 MHz crystal X1 |
| XOUT  | 22 | XOUT | 12 MHz crystal X1 |
| USB_DM | 51 | USB_DM | Native USB D− → USB-C (J3) |
| USB_DP | 52 | USB_DP | Native USB D+ → USB-C (J3) |
| QSPI_SD0–3, SCLK, SS | 55–60 | NC | **Not used** — RP2354A boots from internal 2 MB flash |

## Power / ground

| Rail | Pins | Net |
|------|------|-----|
| +3V3 (IOVDD / analog / periph) | 1, 11, 20, 30, 38, 45 (IOVDD), 44 (ADC_AVDD), 46 (VREG_AVDD), 49 (VREG_VIN), 53 (USB_OTP_VDD), 54 (QSPI_IOVDD) | +3V3 |
| Core 1.1 V (internal SMPS) | 6, 23, 39 (DVDD), 50 (VREG_FB) | DVDD |
| Core buck switch node | 48 (VREG_LX) → L1 (3µ3) → DVDD | VREG_LX |
| Ground | 47 (VREG_PGND), 61 (EP / thermal pad) | GND |

Core supply: the RP2350 internal buck regulator drives **VREG_LX (48)** through
**L1 (3.3 µH)** to generate the 1.1 V core rail **DVDD**, sensed at VREG_FB (50).
VREG_VIN (49) is fed from +3V3.

## Firmware notes
- **GPIB data bus** DIO1..DIO8 = GPIO0..GPIO7, contiguous with DIO1 = GPIO0 — lay out
  a PIO state machine on the low 8 GPIOs for byte-wide transfers.
- **Handshake** EOI, DAV, NDAC, NRFD = GPIO12..GPIO15 (contiguous), convenient for a
  second PIO program or grouped GPIO.
- **Ethernet**: W5500 on SPI0 (GPIO16–19), CS = GPIO17, plus RSTn (GPIO20) and
  INTn (GPIO21, active-low IRQ). W5500 and MCU are both 3.3 V — no level shifting.
- **USB**: native device port (bootrom UF2 bootloader also available on this port).
- **Config strap** CFG (GPIO28) is read at boot; **3 spare GPIOs** are free:
  GPIO23, GPIO24, GPIO29 (GPIO29 is ADC-capable).
- Reset via RUN (26); SWD debug on GPIO-independent SWCLK/SWDIO (24/25).

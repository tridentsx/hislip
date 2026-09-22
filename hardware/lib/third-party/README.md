# Third-party KiCad libraries

Libraries in this directory were **not** authored here. They are vendored rather
than referenced so that the board is reproducible from a single checkout: a
schematic that points at a library nobody else has is not a design, it is a
promise. Each subdirectory keeps its upstream licence file and the exact commit
it came from.

Vendored copies are not to be edited. Fix bugs upstream and re-vendor, or the
next update silently reverts the fix.

## gpib-kicad-library

| | |
|---|---|
| Source | <https://github.com/Alba0404/gpib-kicad-library> |
| Author | Alexandre Barrat (`Alba0404`) |
| Commit | `f77d57f3e886f2f314c946e7a6f0ce3f1df2519f` (see `UPSTREAM_COMMIT`) |
| Vendored | 2026-09-21 |
| Licence | **CC BY-SA 4.0**, with the author's design waiver (below) |

Provides the IEEE-488 connector this project needs — L-com CIB24S / CIB24SPC /
CIB24SRA as symbol, footprint and 3D model — plus GPIB transceiver and controller
symbols (SN75160B/161B/162B, uPD7210, NAT9914, TMS9914A, Intel 8291A…).

Used here for **J1**, and as an independent cross-check of `docs/netlist-spec.md`
§5.4: the upstream pin table matches this project's connector pinout pin for pin,
including the six twisted-pair ground returns (18–23) and logic ground (24). That
agreement was arrived at separately from the spec, which is the only reason it is
worth anything.

### Licence interaction with this repository

The rest of `hislip` is MIT. This subdirectory is **CC BY-SA 4.0** and stays that
way; vendoring it does not relicense it, and redistributing it obliges us to keep
the attribution above and `LICENSE.txt` alongside it.

It does **not** make the board design CC BY-SA. The author grants an explicit
exception waiving article 3 of the licence for electronic designs that use the
material:

> To the extent that the creation of electronic designs that use 'Licensed
> Material' can be considered to be 'Adapted Material', then the copyright holder
> waives article 3 of the license with respect to these designs and any generated
> files which use data provided as part of the 'Licensed Material'.

So `adapter.kicad_sch`, the Gerbers and the BOM are unaffected by ShareAlike. The
library files in this directory are not.

3D models and drawings for the L-com connectors originate from
<https://www.l-com.com/> per the upstream README.

## Vendor-supplied single files

Not every third-party file arrives as a library. Recorded here so their
provenance is not lost:

| File | Part | Source | Notes |
|---|---|---|---|
| `../adapter.pretty/CONN_112-024-113R001_NRC.kicad_mod` | NorComp 112-024-113R001 (J1) | vendor/aggregator download, 2026-09-21 (`112_024_113R001.zip`) | Male Centronics-24 R/A. Measured before adoption: 24 pads, pins 1–12 / 13–24, 2.16 mm pitch, 4.29 mm rows, 2 × 3.1 mm holes on 46.8 mm span; mirror image of the L-com female above. Licence not stated by the supplier. The archive's symbol was **not** used — the vendored `Connector_GPIB` symbol has meaningful pin names and was already cross-checked against netlist-spec §5.4. |

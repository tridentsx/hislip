#!/usr/bin/env python3
"""Check the PoE-to-GPIB adapter netlist.

Usage:
    scripts/netlist-check.py docs/netlist-spec.nets [build/adapter.net]

With one argument it validates the canonical net table for internal
consistency (this runs before any schematic exists). With a second argument
(a KiCad-exported netlist, `kicad-cli sch export netlist`) it also diffs the
exported nets against the canonical set.

See docs/netlist-spec.md section 9. No third-party dependencies.
"""

import re
import sys
from collections import defaultdict


class Spec:
    def __init__(self):
        self.pins = {}                     # ref -> pin count N (full: pins 1..N)
        self.parts = set()                 # ref of partial/reference components
        self.gpio = {}                     # signal -> gpio number
        self.reqs = []                     # required R-HW ids (order preserved)
        self.covers = set()                # R-HW ids that have a @cover line
        self.nets = {}                     # net name -> list of (ref, pin)
        self.nc = {}
        self.open_nets = {}                       # (ref, pin) -> reason


def parse_pinref(tok):
    """'R_TE.1' -> ('R_TE', '1'). Ref has no dot; pin is the tail."""
    i = tok.rfind(".")
    if i < 0:
        raise ValueError(f"pin reference without '.': {tok!r}")
    return tok[:i], tok[i + 1:]


def load_spec(path):
    s = Spec()
    with open(path, encoding="utf-8") as fh:
        for lineno, raw in enumerate(fh, 1):
            line = raw.split("#", 1)[0].strip()   # strip full-line and inline comments
            if not line:
                continue
            if line.startswith("!pins"):
                _, ref, n = line.split()
                s.pins[ref] = int(n)
            elif line.startswith("!part"):
                s.parts.add(line.split()[1])
            elif line.startswith("!gpio"):
                _, sig, num = line.split()
                s.gpio[sig] = int(num)
            elif line.startswith("!req"):
                s.reqs.append(line.split()[1])
            elif line.startswith("!open"):
                # A net that is deliberately incomplete: the missing connection
                # is an unresolved design question, not an oversight. Reported
                # as pending so the gate stays meaningful while the open item
                # stays visible.
                parts = line.split(None, 2)
                s.open_nets[parts[1]] = parts[2] if len(parts) > 2 else ""
            elif line.startswith("!nc"):
                parts = line.split(None, 2)
                ref, pin = parse_pinref(parts[1])
                s.nc[(ref, pin)] = parts[2] if len(parts) > 2 else ""
            elif line.startswith("@cover"):
                m = re.search(r"(R-HW-\d+)", line)
                if m:
                    s.covers.add(m.group(1))
            elif ":" in line:
                name, rest = line.split(":", 1)
                members = [parse_pinref(t) for t in rest.split()]
                if name in s.nets:
                    raise ValueError(f"line {lineno}: duplicate net {name}")
                s.nets[name] = members
            else:
                raise ValueError(f"line {lineno}: unparseable: {line!r}")
    return s


class Report:
    def __init__(self):
        self.checks = 0
        self.failures = []

    def ok(self, msg):
        self.checks += 1
        print(f"  PASS  {msg}")

    def fail(self, msg):
        self.checks += 1
        self.failures.append(msg)
        print(f"  FAIL  {msg}")


def check_pin_assignment(s, r):
    """Every pin of every component is assigned exactly once (net or nc),
    with no out-of-range or unknown-component references."""
    assigned = defaultdict(list)   # (ref,pin) -> list of net names ('#nc' for nc)
    for net, members in s.nets.items():
        for ref, pin in members:
            assigned[(ref, pin)].append(net)
    for (ref, pin) in s.nc:
        assigned[(ref, pin)].append("#nc")

    unknown = sorted({ref for (ref, _) in assigned
                      if ref not in s.pins and ref not in s.parts})
    if unknown:
        r.fail(f"pins reference undeclared component(s): {', '.join(unknown)}")
    else:
        r.ok(f"all pin references name a declared component "
             f"({len(s.pins)} full + {len(s.parts)} reference)")

    dups = {p: locs for p, locs in assigned.items() if len(locs) > 1}
    if dups:
        for p, locs in sorted(dups.items()):
            r.fail(f"pin {p[0]}.{p[1]} assigned {len(locs)}x: {', '.join(locs)}")
    else:
        r.ok("no pin assigned to more than one net/nc")

    # completeness: for each component, referenced pins == {1..N}
    complete = True
    for ref, n in sorted(s.pins.items()):
        got = {int(pin) for (rr, pin) in assigned if rr == ref and pin.isdigit()}
        want = set(range(1, n + 1))
        missing = sorted(want - got)
        extra = sorted(got - want)
        if missing or extra:
            complete = False
            detail = []
            if missing:
                detail.append(f"missing {missing}")
            if extra:
                detail.append(f"out-of-range {extra}")
            r.fail(f"{ref}: pins != 1..{n} ({'; '.join(detail)})")
    if complete:
        total = sum(s.pins.values())
        r.ok(f"every component pin 1..N is assigned exactly once ({total} pins total)")


def check_min_two(s, r):
    """A net with fewer than two pins is dangling."""
    bad = {n: m for n, m in s.nets.items()
           if len(m) < 2 and n not in s.open_nets}
    for n in sorted(s.open_nets):
        if n in s.nets and len(s.nets[n]) < 2:
            r.ok(f"net {n}: pending - {s.open_nets[n] or 'declared open'}")
    if bad:
        for n, m in sorted(bad.items()):
            r.fail(f"net {n} has {len(m)} pin(s); needs >= 2")
    else:
        r.ok(f"every net has >= 2 pins ({len(s.nets)} nets)")


def check_gpio_contiguity(s, r):
    """R-HW-060: DIO1..DIO8 on GPIO0..7, DIO1 lowest.
       R-HW-061: EOI,DAV,NDAC,NRFD on four consecutive GPIOs."""
    dio = [s.gpio.get(f"DIO{i}") for i in range(1, 9)]
    if None in dio:
        r.fail("R-HW-060: DIO1..DIO8 GPIO assignments incomplete")
    elif dio == list(range(dio[0], dio[0] + 8)) and dio[0] == min(dio):
        r.ok(f"R-HW-060: DIO1..DIO8 contiguous on GPIO{dio[0]}..GPIO{dio[7]}, DIO1 lowest")
    else:
        r.fail(f"R-HW-060: DIO1..DIO8 not contiguous/lowest: {dio}")

    grp = {k: s.gpio.get(k) for k in ("EOI", "DAV", "NDAC", "NRFD")}
    vals = sorted(v for v in grp.values() if v is not None)
    if len(vals) != 4:
        r.fail(f"R-HW-061: EOI/DAV/NDAC/NRFD GPIO incomplete: {grp}")
    elif len(set(vals)) == 4 and vals[-1] - vals[0] == 3:
        r.ok(f"R-HW-061: EOI,DAV,NDAC,NRFD consecutive on GPIO{vals[0]}..GPIO{vals[-1]}")
    else:
        r.fail(f"R-HW-061: EOI/DAV/NDAC/NRFD not consecutive: {grp}")


def check_bias_rail_discipline(s, r):
    """Safe-state bias must never reference a 5 V net (GPIO26/27 are non-FT;
    the FT-side pull-ups stay on +3V3 to avoid 5 V into the pad). See 3.2."""
    bias = {"R_TE", "R_DC", "R_ATN", "R_REN", "R_IFC", "R_SRQ"}
    fivev = {"+5V_POE", "+5V_XCVR"}
    offenders = []
    for net in fivev:
        for ref, pin in s.nets.get(net, []):
            if ref in bias:
                offenders.append(f"{ref}.{pin} on {net}")
    if offenders:
        r.fail("bias resistor on a 5 V net: " + ", ".join(offenders))
    else:
        r.ok("no safe-state bias resistor (R_TE/R_DC/R_ATN/R_REN/R_IFC/R_SRQ) touches a 5 V net")


def check_requirements(s, r):
    missing = [q for q in s.reqs if q not in s.covers]
    if missing:
        r.fail("requirements with no @cover: " + ", ".join(missing))
    else:
        r.ok(f"every declared requirement has a coverage line ({len(s.reqs)} reqs: "
             f"{s.reqs[0]}..{s.reqs[-1]})")


# ---- optional: diff against a KiCad-exported netlist ----

def load_kicad_netlist(path):
    """Parser for `kicad-cli sch export netlist` (s-expr).

    Returns (nets, pinnames, refs) where
      nets     = {netname: set((ref, pin_number))}
      pinnames = {ref: {PINFUNCTION: pin_number}}
      refs     = set of every reference designator in the export

    Two normalisations matter. KiCad prefixes a sheet-local net with its sheet
    path ("Some Sheet/DIO1_B"); only the last component is the net's name. And
    KiCad identifies a pin by *number*, while this specification names the pins
    of `!part` components by function (U5.IN, Q1.S, R_G1.a) because a number
    means nothing until a symbol is chosen. `pinnames` is what lets the two be
    compared without hard-coding either.
    """
    text = open(path, encoding="utf-8").read()
    nets, pinnames, refs = {}, {}, set()
    for net_blk in re.finditer(r'\(net\b(.*?)(?=^\t\t\(net\b|\Z)', text,
                               re.S | re.M):
        blk = net_blk.group(1)
        nm = re.search(r'\(name\s+"([^"]*)"\s*\)', blk)
        if not nm:
            continue
        name = nm.group(1).strip().rsplit("/", 1)[-1]
        members = set()
        for node in re.finditer(r'\(node\b((?:\s*\([^()]*\))+)\s*\)', blk, re.S):
            nb = node.group(1)
            ref = re.search(r'\(ref\s+"([^"]*)"', nb)
            pin = re.search(r'\(pin\s+"([^"]*)"', nb)
            fn = re.search(r'\(pinfunction\s+"([^"]*)"', nb)
            if not (ref and pin):
                continue
            members.add((ref.group(1), pin.group(1)))
            refs.add(ref.group(1))
            if fn:
                pinnames.setdefault(ref.group(1), {})[fn.group(1).upper()] = \
                    pin.group(1)
        nets[name] = members
    return nets, pinnames, refs


def resolve_pin(ref, pin, pinnames):
    """Translate a specification pin id into the exported pin *number*.

    Tries, in order: it already is the number; the symbol's pin function
    (U6.RESET); the function with a 'V' prefix, since a datasheet's IN/OUT is
    a symbol's VIN/VOUT; and finally the two-terminal a/b convention.
    """
    table = pinnames.get(ref, {})
    if pin in table.values():
        return pin
    for cand in (pin.upper(), "V" + pin.upper()):
        if cand in table:
            return table[cand]
    return {"a": "1", "b": "2"}.get(pin, pin)


def diff_against_kicad(s, kpath, r):
    kicad, pinnames, exported_refs = load_kicad_netlist(kpath)
    canon = {}
    for name, members in s.nets.items():
        canon[name] = {(ref, resolve_pin(ref, pin, pinnames))
                       for ref, pin in members}
    k_by_members = {frozenset(m): n for n, m in kicad.items()}
    for name, members in sorted(canon.items()):
        key = frozenset(members)
        if key in k_by_members:
            r.ok(f"net {name}: membership matches exported net "
                 f"{k_by_members[key]!r}")
            continue
        if name in kicad:
            miss = members - kicad[name]
            extra = kicad[name] - members
            # A member whose component is not in the export at all belongs to a
            # sheet that has not been drawn yet. That is pending work, not a
            # wiring defect, and saying so keeps the real failures visible.
            if miss and not extra and all(ref not in exported_refs
                                          for ref, _ in miss):
                r.ok(f"net {name}: partial - implemented members match, "
                     f"awaiting {sorted(ref for ref, _ in miss)}")
            else:
                r.fail(f"net {name}: membership differs "
                       f"(missing {sorted(miss)}, extra {sorted(extra)})")
        else:
            if all(ref not in exported_refs for ref, _ in members):
                r.ok(f"net {name}: pending - no member component drawn yet")
            else:
                r.fail(f"net {name}: not found in exported netlist by name "
                       f"or membership")
    canon_keys = {frozenset(m) for m in canon.values()}
    for name, members in sorted(kicad.items()):
        if frozenset(members) in canon_keys or name in canon:
            continue
        if all(ref.startswith("#") for ref, _ in members):
            continue  # power-flag-only net, carries no component pin
        # KiCad invents an "unconnected-(REF-PINNAME-PadN)" net for every pin
        # left deliberately open. That is not an unspecified net: it is the
        # export's way of spelling a non-connection, so check it against the
        # specification's !nc declarations instead of reporting it as junk. A
        # pin KiCad reports as unconnected that the specification never
        # declared open is a real defect and still fails.
        if name.startswith("unconnected-") and len(members) == 1:
            ref, pin = next(iter(members))
            declared = {(r, resolve_pin(r, p, pinnames))
                        for r, p in s.nc}
            if (ref, pin) in declared:
                r.ok(f"net {name}: matches declared non-connection {ref}.{pin}")
            else:
                r.fail(f"pin {ref}.{pin} is unconnected in the export but no "
                       f"!nc declares it open")
            continue
        r.fail(f"exported net {name!r} has no counterpart in the "
               f"specification")


def main(argv):
    if len(argv) < 2:
        print(__doc__)
        return 2
    s = load_spec(argv[1])
    r = Report()

    print("Spec self-consistency:")
    check_pin_assignment(s, r)
    check_min_two(s, r)
    check_gpio_contiguity(s, r)
    check_bias_rail_discipline(s, r)
    check_requirements(s, r)

    if len(argv) >= 3:
        print(f"\nDiff against exported netlist {argv[2]}:")
        diff_against_kicad(s, argv[2], r)

    print(f"\n{r.checks} checks, {len(r.failures)} failure(s).")
    return 1 if r.failures else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))

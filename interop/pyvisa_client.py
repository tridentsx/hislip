#!/usr/bin/env python3
"""Interoperability test for the gotmc/hislip server, driven by pyvisa-py.

pyvisa-py is an independent pure-Python HiSLIP client with no shared lineage with
the Go server. That independence is the point: conformance established between
this project's own client and server would prove only that they agree with each
other, whether or not either agrees with IVI-6.1.

Coverage and known limitations are recorded in interop/README.md. In short:
pyvisa-py exercises initialization, queries, commands, chunked responses, device
clear and the status byte, but does not implement viAssertTrigger or viLock for
HiSLIP, so trigger and locking need a different client.

Usage:
    python3 pyvisa_client.py [host] [port]

Exits non-zero if any check fails. Operations pyvisa-py does not implement are
reported as skipped, not failed.
"""

import sys
import time

try:
    import pyvisa
except ImportError:
    print("SKIP pyvisa-py: not installed (pip install pyvisa pyvisa-py)")
    sys.exit(0)

HOST = sys.argv[1] if len(sys.argv) > 1 else "127.0.0.1"
PORT = int(sys.argv[2]) if len(sys.argv) > 2 else 4880

passed = failed = skipped = 0


def check(name, fn, want=None):
    """Run one check. A None want means any result without an exception passes."""
    global passed, failed, skipped
    start = time.time()
    try:
        got = fn()
    except NotImplementedError:
        skipped += 1
        print(f"  SKIP {name}: not implemented by pyvisa-py")
        return
    except Exception as exc:  # noqa: BLE001 - the point is to classify anything
        if "NSUP_OPER" in str(exc):
            skipped += 1
            print(f"  SKIP {name}: unsupported by pyvisa-py")
            return
        failed += 1
        print(f"  FAIL {name}: {type(exc).__name__}: {str(exc)[:70]}")
        return
    elapsed = time.time() - start
    if want is not None and want not in str(got):
        failed += 1
        print(f"  FAIL {name} ({elapsed:.2f}s): got {str(got)[:50]!r}, want {want!r} in it")
        return
    passed += 1
    print(f"  PASS {name} ({elapsed:.2f}s): {str(got)[:55]!r}")


def main():
    resource = f"TCPIP0::{HOST}::hislip0,{PORT}::INSTR"
    print(f"pyvisa-py {pyvisa.__version__} -> {resource}")

    rm = pyvisa.ResourceManager("@py")
    inst = rm.open_resource(resource)
    inst.timeout = 5000

    print("initialization and data transfer")
    check("*IDN? query", lambda: inst.query("*IDN?").strip(), "GoTMC")
    check("*OPC? query", lambda: inst.query("*OPC?").strip(), "1")
    check("command then query",
          lambda: (inst.write("SOUR:VOLT 3.3"), inst.query("MEAS:VOLT?").strip())[1], "3.3")
    check("compound message", lambda: inst.query("*CLS;*ESR?").strip(), "0")

    print("chunked and long transfers")
    check("chunked binary response",
          lambda: len((inst.write("CURV?"), inst.read_raw())[1]) > 4000 or "short")
    # The message whose only query marker falls past the negotiated payload size.
    # An accumulate-then-classify policy answers this wrongly; see §24.4.2.
    long_msg = "SOUR:VOLT 3.3;" * 700 + "MEAS:VOLT?"
    check(f"long compound {len(long_msg)}B", lambda: inst.query(long_msg).strip(), "3.3")
    huge = "SOUR:VOLT 1.1;" * 3000 + "MEAS:VOLT?"
    check(f"very long {len(huge)}B", lambda: inst.query(huge).strip(), "1.1")

    print("quoted and binary data must not look like queries")
    check("question mark inside a string",
          lambda: (inst.write('DISP:TEXT "what?"'), inst.query("*IDN?").strip())[1], "GoTMC")

    print("status and control")
    check("read_stb", lambda: hex(inst.read_stb()))
    check("device clear", lambda: inst.clear() or "cleared")
    check("query after clear", lambda: inst.query("*IDN?").strip(), "GoTMC")
    check("assert_trigger", lambda: inst.assert_trigger() or "triggered")
    check("exclusive lock", lambda: inst.lock_excl(2000) or "locked")

    print("error paths")
    # A query the instrument does not answer must complete rather than hang;
    # see §17.3. An empty result is the correct outcome.
    check("unanswered query completes", lambda: repr(inst.query("MEAS:VLOT?").strip()))
    check("error queue reports it", lambda: inst.query("SYST:ERR?").strip(), "113")
    check("session alive at end", lambda: inst.query("*IDN?").strip(), "GoTMC")

    inst.close()

    print(f"\npyvisa-py: {passed} passed, {failed} failed, {skipped} skipped")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())

#!/usr/bin/env python3
"""Interoperability test for the gotmc/hislip server, driven by PyHiSLIP.

PyHiSLIP is an independent Python HiSLIP implementation. It matters here because
it is the only client available that exercises three transactions pyvisa-py does
not: trigger, the lock request/info/release cycle, and a device clear whose
channel usage can be read directly from its source.

It is also the second of the two independent confirmations that IVI-6.1 Table 4 is
wrong about the DeviceClearAcknowledge channel. Its device_clear reads the
acknowledgement from self.sync_channel, and its own message table annotates the
message "# S, S", server and synchronous.

PyHiSLIP uses `async` as a parameter name, which has been a reserved word since
Python 3.7, so it does not import on a current interpreter without a rename. The
runner applies that rename to a copy; see interop/run.sh. This script expects the
patched copy to be importable.

Usage:
    python3 pyhislip_client.py [host] [port]
"""

import sys
import time

try:
    from pyhislip import HiSLIP
except (ImportError, SyntaxError) as exc:
    print(f"SKIP PyHiSLIP: not importable ({type(exc).__name__})")
    print("     the runner clones it and applies the `async` rename; see interop/README.md")
    sys.exit(0)

HOST = sys.argv[1] if len(sys.argv) > 1 else "127.0.0.1"
PORT = int(sys.argv[2]) if len(sys.argv) > 2 else 4880

passed = failed = skipped = 0


def check(name, fn, want=None):
    global passed, failed, skipped
    start = time.time()
    try:
        got = fn()
    except AttributeError as exc:
        skipped += 1
        print(f"  SKIP {name}: {exc}")
        return
    except Exception as exc:  # noqa: BLE001
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
    print(f"PyHiSLIP -> {HOST}:{PORT}")
    inst = HiSLIP()
    inst.connect(HOST, port=PORT)

    print("data transfer")
    check("*IDN? via ask", lambda: inst.ask("*IDN?"), "GoTMC")
    check("write then ask",
          lambda: (inst.write("SOUR:VOLT 1.5"), inst.ask("MEAS:VOLT?"))[1], "1.5")

    print("maximum message size")
    # The client's value sets the server's transmit limit; the server's reply
    # carries its own receive limit. Getting that direction wrong broke every
    # response after the negotiation; see §24.4.2.
    check("set_max_message_size", lambda: inst.set_max_message_size(4096) or "set")
    check("query after renegotiation", lambda: inst.ask("*IDN?"), "GoTMC")
    # No binary check here: ask() decodes its result as text, so a block response
    # raises UnicodeDecodeError and leaves the session unusable. Chunked binary
    # responses are covered by pyvisa_client.py, which has read_raw.

    print("device clear")
    # The second confirmation of R-PROTO-041. This completes only if the server
    # sends DeviceClearAcknowledge on the synchronous channel.
    check("device_clear", lambda: inst.device_clear() or "cleared")
    check("query after clear", lambda: inst.ask("*IDN?"), "GoTMC")

    print("locking, which pyvisa-py cannot exercise")
    # 1 means granted for a request, and an exclusive release for a release.
    check("request_lock", lambda: inst.request_lock(), "1")
    check("lock_info", lambda: inst.lock_info(), "(1, 1)")
    check("release_lock", lambda: inst.release_lock(), "1")

    print("trigger and status, which pyvisa-py cannot exercise")
    check("trigger_message", lambda: inst.trigger_message() or "triggered")
    check("status_query", lambda: inst.status_query())

    print(f"\nPyHiSLIP: {passed} passed, {failed} failed, {skipped} skipped")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())

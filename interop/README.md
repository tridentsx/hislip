# Interoperability tests

Conformance for this project is established against independent implementations,
not against its own client and server. A client and a server written by one author
from one reading of IVI-6.1 will agree with each other whether or not either
agrees with the standard; that is risk R21 in the design specification, and
running the server first made it easier to fall into.

Everything here drives the example server in `../examples/server` with a client
this project did not write.

## Running

```sh
./run.sh                    # every available client
./run.sh pyvisa             # one of pyvisa | pyhislip | libhislip
KEEP=1 ./run.sh             # leave the server up and print its log path
PORT=4880 ./run.sh          # fixed port instead of an arbitrary free one
```

Clients that are not installed are skipped rather than failed, so the script is
useful on a machine with none of them. The exit status is non-zero only if a
client that actually ran reported a failure.

### Installing the clients

```sh
# pyvisa-py
python3 -m venv .venv && .venv/bin/pip install pyvisa pyvisa-py

# libhislip
git clone https://github.com/lxi-tools/libhislip && cd libhislip
meson setup build && ninja -C build
export LIBHISLIP=$PWD

# PyHiSLIP is cloned by run.sh, or point at a local copy
export PYHISLIP_SRC=/path/to/pyhislip.py
```

`run.sh` looks for a Python with pyvisa in `$VISA_PYTHON`, `/tmp/visa-env/bin/python`
and `./.venv/bin/python`, in that order, before falling back to `python3`.

## Results as of 2026-09-21

```text
client       version    passed  failed  skipped
pyvisa-py    1.16.2         14       0        2
PyHiSLIP     git HEAD       11       0        0
libhislip    initial import  7       0        0
```

Server counters for a full run: 3 sessions, 0 fatal errors, 0 non-fatal errors,
2 device clears, 1 trigger, 2 serial polls, 1 query without an answer (deliberate),
6 silent interrupted errors (see below).

## Client capability matrix

No single client exercises the whole protocol, which is why more than one is
required.

```text
transaction              pyvisa-py  PyHiSLIP  libhislip
initialization, both ch.     yes       yes       yes
maximum message size          no       yes       yes
Data / DataEnd query         yes       yes       yes
command, no response         yes       yes       yes
chunked response             yes        no        no
device clear                 yes       yes        no
status query                 yes       yes        no
trigger                       no       yes        no
lock request/info/release     no       yes        no
```

Trigger and locking rest on one client each. R-DOC-065 records that a second
should be found before release; NI-VISA is the obvious candidate if a Windows
machine is available.

## What these tests found

Each of these was invisible to a full unit suite, which is the argument for the
practice.

**Query misclassification on long messages** — found by pyvisa-py on the first
compound write longer than the negotiated payload. The server accumulated the
program message into a bounded buffer and silently truncated it, so a query marker
in the tail was lost, the response was never read, and the client timed out. The
response policy is now a streaming state machine that accumulates nothing. See
R-DEV-015.

**Maximum message size direction** — found by libhislip on its first query.
IVI-6.1 Table 28 makes the two halves independent: the client's value tells the
server the largest message it may send, the server's reply tells the client the
largest the server can receive. The server was returning the smaller of the two
and applying it to its transmit limit, which gets both halves wrong.

**Response buffer sized once** — found by the same run, and the more damaging of
the two. The buffer was allocated when the channel loop started and sized from the
transmit limit at that moment, so a client that lowered the limit afterwards broke
every subsequent response.

## The silent interrupted errors are not ours

A full run reports six. They are real, and they are pyvisa-py's.

IVI-6.1 says of the RMT-delivered flag that it "is 1 if this is the **first**
RMT-delivered flag-carrying message since the client delivered RMT to the
application layer" and that it "is only reported once". pyvisa-py sets its `_rmt`
flag to 1 on receiving a `DataEnd`:

```python
if self._payload_remaining == 0 and self._msg_type == "DataEnd":
    self._rmt = 1
```

but `_send_data_packet` and `_send_data_end_packet` send `self._rmt` without
clearing it afterwards. Only `AsyncStatusQuery` clears it. So every synchronous
message after the first response carries the flag, not just the first, and the
server sees RMT-delivered set while nothing is outstanding. That is the mismatch
of IVI-6.1 section 3.1.1 rule 2.

The server's behaviour is correct and worth spelling out, because it looks like a
failure and is not:

- the error is reported only through the internal error mechanism and counted;
- no wire traffic is produced, since notifying the client here would itself be a
  protocol violation;
- the session continues and every subsequent operation succeeds.

That the counter moves at all is useful: it shows the mechanism is live rather
than dead code, which no unit test can demonstrate as convincingly. It also
exercises the interpretation recorded on R-SYNC-013, that RMT-expected is cleared
after a mismatch rather than left set, so one client defect produces one error per
occurrence instead of a cascade.

## Known limitations of the clients

Recorded so that a future failure is not mistaken for a regression in this
project.

**libhislip** is at its initial import and is incomplete in two ways that matter.
`hs_sync_send` computes a chunk count but then passes the full length and an
unadvanced data pointer to `msg_create`, so it emits one message however large and
violates the maximum message size it has just negotiated; messages in
`libhislip_client.c` are kept inside that maximum for this reason.
`hs_sync_receive` loops until `DataEnd` but its payload reassembly is two TODO
comments with no code, so it cannot receive a chunked response — a correctly
chunking server appears to hang to it. Its API has no device clear, trigger or
lock.

**pyvisa-py** does not implement `viAssertTrigger` or `viLock` for HiSLIP, and
reports them as unsupported operations. It also has the RMT-delivered defect
described above.

**PyHiSLIP** uses `async` as a parameter name, reserved since Python 3.7, so it
does not import on a current interpreter without a rename; `run.sh` applies it to a
copy. Its `ask` decodes responses as text, so it cannot receive binary block data.

**scpify**, the Rust crate, exposes only connect, send and query. With no device
clear, trigger or lock it would add a third check of the data path and nothing
else, so it is not used here.

## What is still untested

- The Interrupted transaction, the input-queued case that sends both
  `Interrupted` and `AsyncInterrupted`. A conforming client does not trigger it,
  so it has unit coverage only.
- Overlap Mode, which this server profile refuses by design.
- NI-VISA and Keysight IO Libraries, neither installable on this machine.
- A physical HiSLIP instrument. R-DOC-064 records this as deferred rather than
  cancelled, and what it would add: vendor-specific behaviour, real timing, and
  the discovery path as a shipping product implements it.

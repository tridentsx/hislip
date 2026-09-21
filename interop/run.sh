#!/usr/bin/env bash
#
# Run the interoperability suite against the example server.
#
# Conformance for this project is established against independent implementations
# rather than against its own client and server, which would agree with each other
# whether or not either agrees with IVI-6.1. This script is that evidence made
# repeatable: before it existed the results lived in a shell history and could not
# be reproduced.
#
# Clients that are not installed are skipped, not failed, so the script is useful
# on a machine with none of them. Exit status is non-zero only if a client that did
# run reported a failure.
#
# Usage:
#   ./run.sh                 # run every available client
#   ./run.sh pyvisa          # run one
#   KEEP=1 ./run.sh          # leave the server running and print its log path
#
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "${here}/.." && pwd)"
work="$(mktemp -d)"
port="${PORT:-0}"
keep="${KEEP:-0}"
want="${1:-all}"

server_pid=""
server_log="${work}/server.log"
failures=0
ran=0
skipped=0

cleanup() {
    if [[ -n "${server_pid}" ]] && [[ "${keep}" != "1" ]]; then
        kill -INT "${server_pid}" 2>/dev/null || true
        wait "${server_pid}" 2>/dev/null || true
    fi
    if [[ "${keep}" == "1" ]]; then
        echo "server left running, pid ${server_pid}, log ${server_log}"
    else
        rm -rf "${work}"
    fi
}
trap cleanup EXIT

section() { printf '\n== %s ==\n' "$1"; }

# Pick a free port when none was given, so concurrent runs do not collide.
if [[ "${port}" == "0" ]]; then
    port="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
fi

section "building the example server"
if ! (cd "${repo}" && go build -o "${work}/hislip-server" ./examples/server); then
    echo "FAIL: could not build the example server"
    exit 1
fi

# setsid detaches the server so that a pattern matching this script's own command
# line can never kill it. Two earlier attempts at this did exactly that.
setsid "${work}/hislip-server" -addr "127.0.0.1:${port}" -v > "${server_log}" 2>&1 &
server_pid=$!
sleep 1

if ! kill -0 "${server_pid}" 2>/dev/null; then
    echo "FAIL: the server exited immediately"
    cat "${server_log}"
    exit 1
fi
echo "server pid ${server_pid} on 127.0.0.1:${port}"

# Prefer a virtual environment if one was made for this, else the system python.
python_bin="python3"
for candidate in "${VISA_PYTHON:-}" /tmp/visa-env/bin/python "${here}/.venv/bin/python"; do
    if [[ -n "${candidate}" ]] && [[ -x "${candidate}" ]]; then
        python_bin="${candidate}"
        break
    fi
done

run_client() {
    local name="$1"
    shift
    ran=$((ran + 1))
    if "$@"; then
        return 0
    fi
    echo "  -> ${name} reported failures"
    failures=$((failures + 1))
}

if [[ "${want}" == "all" || "${want}" == "pyvisa" ]]; then
    section "pyvisa-py"
    if "${python_bin}" -c "import pyvisa" 2>/dev/null; then
        run_client pyvisa-py "${python_bin}" "${here}/pyvisa_client.py" 127.0.0.1 "${port}"
    else
        skipped=$((skipped + 1))
        echo "SKIP: pyvisa-py not installed"
        echo "      python3 -m venv .venv && .venv/bin/pip install pyvisa pyvisa-py"
    fi
fi

if [[ "${want}" == "all" || "${want}" == "pyhislip" ]]; then
    section "PyHiSLIP"
    ph="${work}/pyhislip"
    mkdir -p "${ph}"
    if [[ -n "${PYHISLIP_SRC:-}" ]] && [[ -f "${PYHISLIP_SRC}" ]]; then
        cp "${PYHISLIP_SRC}" "${ph}/pyhislip.py"
    elif command -v git >/dev/null && git clone -q --depth 1 \
            https://github.com/noboruatkek/PyHiSLIP.git "${work}/PyHiSLIP" 2>/dev/null; then
        cp "$(find "${work}/PyHiSLIP" -name pyhislip.py | head -1)" "${ph}/pyhislip.py"
    fi
    if [[ -f "${ph}/pyhislip.py" ]]; then
        # PyHiSLIP uses `async` as a parameter name, reserved since Python 3.7.
        # The rename is mechanical and does not change behaviour.
        sed -i 's/\basync=/async_=/g; s/\basync\b\([,)]\)/async_\1/g; s/if async:/if async_:/g' \
            "${ph}/pyhislip.py"
        run_client PyHiSLIP env PYTHONPATH="${ph}" \
            "${python_bin}" "${here}/pyhislip_client.py" 127.0.0.1 "${port}"
    else
        skipped=$((skipped + 1))
        echo "SKIP: PyHiSLIP source not available"
        echo "      set PYHISLIP_SRC=/path/to/pyhislip.py, or allow the clone"
    fi
fi

if [[ "${want}" == "all" || "${want}" == "libhislip" ]]; then
    section "libhislip"
    lib="${LIBHISLIP:-}"
    if [[ -z "${lib}" ]] && [[ -d /tmp/libhislip/build/src ]]; then
        lib=/tmp/libhislip
    fi
    if [[ -n "${lib}" ]] && [[ -d "${lib}/build/src" ]] && command -v gcc >/dev/null; then
        if gcc -o "${work}/libhislip_client" "${here}/libhislip_client.c" \
                -I"${lib}/src" -L"${lib}/build/src" -lhislip 2>/dev/null; then
            run_client libhislip env LD_LIBRARY_PATH="${lib}/build/src" \
                "${work}/libhislip_client" 127.0.0.1 "${port}"
        else
            skipped=$((skipped + 1))
            echo "SKIP: could not compile against libhislip"
        fi
    else
        skipped=$((skipped + 1))
        echo "SKIP: libhislip not built"
        echo "      git clone https://github.com/lxi-tools/libhislip && cd libhislip &&"
        echo "      meson setup build && ninja -C build, then set LIBHISLIP=\$PWD"
    fi
fi

section "server counters"
# The counters are printed on clean shutdown, so ask for one and read them.
if [[ "${keep}" != "1" ]]; then
    kill -INT "${server_pid}" 2>/dev/null || true
    for _ in $(seq 1 30); do
        kill -0 "${server_pid}" 2>/dev/null || break
        sleep 0.1
    done
    server_pid=""
    tail -1 "${server_log}"
    if grep -q "fatal=[1-9]" "${server_log}"; then
        echo "  note: the server reported fatal errors; a refused second client is expected"
    fi
    if grep -q "silent-interrupted=[1-9]" "${server_log}"; then
        echo "  note: an RMT mismatch was detected and correctly reported without wire traffic"
    fi
fi

section "summary"
echo "${ran} client(s) ran, ${failures} reported failures, ${skipped} skipped"
exit $((failures > 0 ? 1 : 0))

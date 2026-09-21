# -*- Justfile -*-

coverage_file := "coverage.out"

# List the available justfile recipes.
[group('general')]
@default:
  just --list --unsorted

# View documentation in web browser using pkgsite.
[group('general')]
docs:
  pkgsite -open .

# Format and vet Go code. Runs before tests.
[group('test')]
check:
	go fix ./...
	go fmt ./...
	go vet ./...

# Lint using golangci-lint
[group('test')]
lint:
  golangci-lint run --config .golangci.yaml

# Run the unit tests.
[group('test')]
unit *FLAGS: check
  go test ./... -cover -vet=off -race {{FLAGS}} -short

# Fuzz one target for the given duration. The decoder is the target that matters
# most: it is the only code reachable from an unauthenticated TCP port before any
# session state exists.
[group('test')]
fuzz target='FuzzDecodeHeader' time='60s':
  go test ./protocol -run '^$' -fuzz='^{{target}}$' -fuzztime={{time}}

# Fuzz every target in sequence.
[group('test')]
fuzzall time='60s':
  #!/usr/bin/env bash
  set -euo pipefail
  for t in FuzzDecodeHeader FuzzReadPayload FuzzWriteMessage; do
    echo "== $t =="
    go test ./protocol -run '^$' -fuzz="^${t}\$" -fuzztime={{time}}
  done

# Assert that the device-side data path does not allocate. Excluded from race
# builds, because the race detector adds allocations of its own.
[group('device')]
allocs:
  go test ./protocol -run TestCodecDoesNotAllocate -v

# HTML report for unit (default), int, e2e, or all tests.
[group('test')]
cover test='unit': check
  go test ./... -vet=off -coverprofile={{coverage_file}} \
  {{ if test == 'all' { '' } \
    else if test == 'int' { '-run Integration' } \
    else if test == 'e2e' { '-run E2E' } \
    else { '-short' } }}
  go tool cover -html={{coverage_file}}

# Verify the device-side import boundary that keeps protocol and server
# compilable under TinyGo. See the design specification, §5.1.
[group('device')]
deps:
  go test -run TestDeviceImportGraph -v .

# Compile the device-side packages for the RP2350 target. Requires tinygo.
[group('device')]
tinygo:
  tinygo build -target=pico2 -o /dev/null ./protocol/... ./server/...

# List the outdated direct dependencies (slow to run).
[group('dependencies')]
outdated:
  # (requires https://github.com/psampaz/go-mod-outdated).
  go list -u -m -json all | go-mod-outdated -update -direct

# Run go mod tidy and verify.
[group('dependencies')]
tidy:
  go mod tidy
  go mod verify

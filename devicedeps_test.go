// Copyright (c) 2026 The hislip developers. All rights reserved.
// Project site: https://github.com/tridentsx/hislip
// Use of this source code is governed by a MIT-style license that
// can be found in the LICENSE.txt file for the project.

package hislip

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// modulePath is the module path of this repository.
const modulePath = "github.com/tridentsx/hislip"

// devicePackages are the packages that TinyGo firmware imports. Their
// transitive import sets must stay inside deviceAllowlist.
//
// See the design specification, §5.1 and R-DOC-031.
var devicePackages = []string{
	"protocol",
	"server",
}

// deviceAllowlist is the set of standard library packages the device-side code
// may import.
//
// Adding an entry is a deliberate decision and the reason belongs here, not in
// a commit message (R-DOC-033). Each entry must be verified to work under
// TinyGo on the target before it is added.
var deviceAllowlist = map[string]string{
	"context":         "cancellation and deadlines; required by the Device interface",
	"encoding/binary": "big-endian header fields; no reflection on fixed-width types",
	"errors":          "sentinel errors and errors.Is",
	"io":              "Reader, Writer and ReadFull for the Stream abstraction",
	"strconv":         "Version.String; diagnostic paths only, never the data path",
	"sync":            "only where TinyGo behaviour has been verified on the target",
	"time":            "deadlines and timeout categories",
}

// deviceDenylist names packages whose presence indicates a specific mistake.
// They would be caught by the allowlist anyway; naming them produces a failure
// message that says what went wrong rather than merely what is not permitted.
var deviceDenylist = map[string]string{
	"fmt":                  "formatting allocates and pulls in reflection; build errors with errors.New and strconv instead",
	"net":                  "the server core must not depend on a transport; use the Stream interface of §7",
	"os":                   "no operating system on the target",
	"reflect":              "excluded by §5.1; defeats TinyGo dead code elimination",
	"encoding/gob":         "excluded by §5.1; reflection-based serialisation",
	"encoding/json":        "reflection-based serialisation; not needed on the device",
	"unsafe":               "excluded by §5.1",
	"log":                  "writes to stderr, which does not exist on the target",
	"testing":              "test-only package reached from non-test code",
	"regexp":               "allocates heavily; resource string parsing belongs in the root package",
	modulePath + "/client": "the desktop client must never be reachable from device code",
}

// TestDeviceImportGraph walks the transitive import graph of the device-side
// packages and fails when it reaches a package outside the allowlist.
//
// This is the enforcement half of the boundary described in §5.1. The rule
// alone is not enough: the realistic failure is not a deliberate bad import but
// an fmt.Errorf added to an error path long after the boundary was agreed.
//
// The test parses imports with go/parser rather than shelling out to the go
// tool, so it needs no network, no module cache and no build of the package
// under inspection.
func TestDeviceImportGraph(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	for _, pkg := range devicePackages {
		t.Run(pkg, func(t *testing.T) {
			dir := filepath.Join(root, filepath.FromSlash(pkg))
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				t.Skipf("package %s not implemented yet", pkg)
			}
			seen := map[string]bool{}
			visit(t, root, pkg, seen, []string{pkg})
		})
	}
}

// visit parses every non-test Go file of the named intra-module package and
// checks its imports, recursing into packages of this module.
func visit(t *testing.T, root, pkg string, seen map[string]bool, path []string) {
	t.Helper()
	if seen[pkg] {
		return
	}
	seen[pkg] = true

	dir := filepath.Join(root, filepath.FromSlash(pkg))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			// Test files may import anything; they never reach the firmware.
			continue
		}
		file := filepath.Join(dir, name)
		f, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, spec := range f.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("%s: bad import path %s", file, spec.Path.Value)
			}
			check(t, file, imported, append(path, imported))
			if rel, ok := intraModule(imported); ok {
				visit(t, root, rel, seen, append(path, imported))
			}
		}
	}
}

// check reports an import that the device-side packages may not use.
func check(t *testing.T, file, imported string, path []string) {
	t.Helper()
	if reason, denied := deviceDenylist[imported]; denied {
		t.Errorf(
			"%s imports %q, which device-side code must not use:\n  reason: %s\n  path:   %s",
			rel(file), imported, reason, strings.Join(path, " -> "),
		)
		return
	}
	if _, ok := intraModule(imported); ok {
		return
	}
	if _, allowed := deviceAllowlist[imported]; !allowed {
		t.Errorf(
			"%s imports %q, which is not in the device allowlist.\n"+
				"  path: %s\n"+
				"  If this package is TinyGo-safe on the target, add it to\n"+
				"  deviceAllowlist with a reason. Do not remove this test.\n"+
				"  Allowed: %s",
			rel(file), imported, strings.Join(path, " -> "), strings.Join(allowlistNames(), ", "),
		)
	}
}

// intraModule reports whether an import path belongs to this module, returning
// the path relative to the module root.
func intraModule(imported string) (string, bool) {
	if imported == modulePath {
		return ".", true
	}
	prefix := modulePath + "/"
	if strings.HasPrefix(imported, prefix) {
		return strings.TrimPrefix(imported, prefix), true
	}
	return "", false
}

func allowlistNames() []string {
	names := make([]string, 0, len(deviceAllowlist))
	for name := range deviceAllowlist {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// rel shortens an absolute file path for readable failure messages.
func rel(file string) string {
	if wd, err := os.Getwd(); err == nil {
		if r, err := filepath.Rel(wd, file); err == nil {
			return r
		}
	}
	return file
}

// TestDenylistIsSubsetOfDisallowed guards against a denylist entry that also
// appears on the allowlist, which would make the denylist silently ineffective.
func TestDenylistIsSubsetOfDisallowed(t *testing.T) {
	for name := range deviceDenylist {
		if _, allowed := deviceAllowlist[name]; allowed {
			t.Errorf("%q appears on both the allowlist and the denylist", name)
		}
	}
}

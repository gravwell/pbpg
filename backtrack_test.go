/*************************************************************************
* Copyright 2026 Gravwell, Inc. All rights reserved.
* Contact: <legal@gravwell.io>
*
* This software may be modified and distributed under the terms of the
* BSD 2-clause license. See the LICENSE file for details.
**************************************************************************/
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBacktracking generates a parser from testdata/backtrack.b and runs it
// on inputs whose parses show what the parser undoes when an attempt fails:
// Data's state, through Data's savepoints, the position, and the variables
// the attempt set.
func TestBacktracking(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a generated parser")
	}
	dir := t.TempDir()
	run := func(dir string, name string, args ...string) string {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=", "GOWORK=off")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v %v: %v\n%s", name, args, err, out)
		}
		return string(out)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	pbpg := filepath.Join(dir, "pbpg")
	run(wd, "go", "build", "-o", pbpg, ".")
	grammar, err := os.ReadFile(filepath.Join("testdata", "backtrack.b"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "backtrack.b"), grammar, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module probe\n\ngo 1.18\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run(dir, pbpg, "-prefix", "Probe", "backtrack.b")

	// each line is the result, the error, the declarations, and the
	// savepoints left open
	tests := []struct {
		in, want string
	}{
		// Decl's declaration is undone with the attempt it was made in
		{"1 foo!", `decl foo | <nil> | ["foo"] | 0`},
		{"1 foo", `word foo | <nil> | [] | 0`},
		// the second alternative starts where the first did
		{"2 a y", `alternative 2 | <nil> | [] | 0`},
		{"2 a z", "\x20| expected y\nexpected x | [] | 0"},
		// a failed option leaves its variables unset
		{"3 bar", `label="" name="bar" | <nil> | [] | 0`},
		{"3 foo: bar", `label="foo" name="bar" | <nil> | [] | 0`},
		// an iteration's variables don't carry over to the next
		{"4 a ! b", `words=["a" "" "b"] bangs=["" "!" ""] | <nil> | [] | 0`},
		{"5 x: ! !", `labels=["x" ""] colons=[":" ""] | <nil> | [] | 0`},
		// a failed option's repetition leaves its slice empty
		{"6 a, b", `label="" labels=[] name="a" names=["b"] | <nil> | [] | 0`},
		{"6 a, b: c", `label="a" labels=["b"] name="c" names=[] | <nil> | [] | 0`},
	}
	args := []string{"run", "."}
	var want []string
	for _, tt := range tests {
		args = append(args, tt.in)
		want = append(want, tt.want)
	}
	if got, want := run(dir, "go", args...), strings.Join(want, "\n")+"\n"; got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

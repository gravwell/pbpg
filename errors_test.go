/*************************************************************************
* Copyright 2026 Gravwell, Inc. All rights reserved.
* Contact: <legal@gravwell.io>
*
* This software may be modified and distributed under the terms of the
* BSD 2-clause license. See the LICENSE file for details.
**************************************************************************/
package main

import (
	"strings"
	"testing"
)

// The main and lexer functions for testdata/errors.b in each mode. A name is
// lower case letters, and if is reserved, which is a real error rather than
// an expectation. A number is digits. In token mode, the input is split into
// tokens at spaces.
const (
	errorsStringMode = `package main

import (
	"errors"
	"fmt"
	"os"
)

type ProbeData struct{}

func main() {
	for _, in := range os.Args[1:] {
		fmt.Println(ParseProbe(in, &ProbeData{}))
	}
}

func lex(input string, ok func(byte) bool) (int, string) {
	n := 0
	for n < len(input) && input[n] == ' ' {
		n++
	}
	start := n
	for n < len(input) && ok(input[n]) {
		n++
	}
	return n, input[start:n]
}

func (p *ProbeData) lexname(input string) (int, string, error) {
	n, name := lex(input, func(c byte) bool { return c >= 'a' && c <= 'z' })
	switch name {
	case "":
		return 0, "", ProbeExpected("a name")
	case "if":
		return 0, "", errors.New("if is reserved")
	}
	return n, name, nil
}

func (p *ProbeData) lexnumber(input string) (int, string, error) {
	n, number := lex(input, func(c byte) bool { return c >= '0' && c <= '9' })
	if number == "" {
		return 0, "", ProbeExpected("a number")
	}
	return n, number, nil
}
`
	errorsTokenMode = `package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type ProbeData struct{}

func main() {
	for _, in := range os.Args[1:] {
		fmt.Println(ParseProbe(strings.Fields(in), &ProbeData{}))
	}
}

func lex(input []string, ok func(rune) bool) string {
	if len(input) == 0 || strings.IndexFunc(input[0], func(r rune) bool { return !ok(r) }) >= 0 {
		return ""
	}
	return input[0]
}

func (p *ProbeData) lexname(input []string) (int, string, error) {
	switch name := lex(input, func(r rune) bool { return r >= 'a' && r <= 'z' }); name {
	case "":
		return 0, "", ProbeExpected("a name")
	case "if":
		return 0, "", errors.New("if is reserved")
	default:
		return 1, name, nil
	}
}

func (p *ProbeData) lexnumber(input []string) (int, string, error) {
	if number := lex(input, func(r rune) bool { return r >= '0' && r <= '9' }); number != "" {
		return 1, number, nil
	}
	return 0, "", ProbeExpected("a number")
}
`
)

// TestErrors generates parsers from testdata/errors.b in string and token
// mode and checks the errors they report for the same inputs.
func TestErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a generated parser")
	}
	tests := []struct {
		in, want string
	}{
		{"a = 1 + 2 ; f ( a ) ;", "<nil>"},
		// a production that fails where it starts expected its label
		{"", "expected a statement"},
		{"= 1 ;", "expected a statement"},
		{"a = ;", "expected an expression"},
		// one that fails further on expected what it expected there
		{"a = 1 + ;", "expected '(', a name or a number"},
		// labels and literals are listed together
		{"a = 1 1 ;", "expected ';' or an operator"},
		{"a ( 1 ;", "expected ')' or an operator"},
		{"a = ( 1 ;", "expected ')' or an operator"},
		// a real error is reported instead of what was expected with it
		{"a = if ;", "if is reserved"},
	}

	var args, want []string
	for _, tt := range tests {
		args = append(args, tt.in)
		want = append(want, tt.want)
	}
	for _, mode := range []struct {
		name  string
		main  string
		flags []string
	}{
		{"string", errorsStringMode, nil},
		{"token", errorsTokenMode, []string{"-token"}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			dir := generate(t, "errors.b", map[string]string{"main.go": mode.main}, mode.flags...)
			got := run(t, dir, "go", append([]string{"run", "."}, args...)...)
			if want := strings.Join(want, "\n") + "\n"; got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// TestGrammarLabels parses a grammar with labels, and a grammar with an error
// that pbpg's own grammar reports through its label for Term.
func TestGrammarLabels(t *testing.T) {
	parse := func(grammar string) (*pbpgData, error) {
		d := &pbpgData{
			typeMap:    make(map[string]string),
			stateMap:   make(map[string]*Expression),
			statesUsed: make(map[string]bool),
			labels:     make(map[string]string),
		}
		return d, Parsepbpg(grammar, d)
	}

	d, err := parse(`A "an a" = "a" B . B = "b" .`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := d.PrintGrammar(), "A \"an a\" = \"a\" B\nB        = \"b\"\n"; got != want {
		t.Errorf("printed %q, want %q", got, want)
	}
	if out := d.out.String(); !strings.Contains(out, `p.label("an a", entryPos)`) || strings.Count(out, "p.label(") != 1 {
		t.Errorf("want one label, for A, in:\n%s", out)
	}

	if _, err := parse(`A = "a" | .`); err == nil || err.Error() != "expected a term" {
		t.Errorf("got %v, want expected a term", err)
	}

	// a lexer function that fails names what it didn't find. These are real
	// errors rather than expectations, since each lexer runs only once its
	// opening token has matched, so the message is all the parser reports.
	if _, err := parse("A = \"a\" .\tAction {"); err == nil || err.Error() != "unterminated code block" {
		t.Errorf("got %v, want unterminated code block", err)
	}
	if _, err := parse(`A = "a .`); err == nil || err.Error() != "unterminated string" {
		t.Errorf("got %v, want unterminated string", err)
	}
	// an unterminated label runs to the next quote in the file, if there is
	// one, and what it reports is that the string it read has whitespace
	// around it
	if _, err := parse(`A "an a = "a" .`); err == nil || err.Error() != "string cannot contain leading or trailing whitespace" {
		t.Errorf("got %v, want a whitespace error", err)
	}
}

// The main function for a grammar whose parser needs no lexer functions, in
// each mode.
const (
	plainStringMode = `package main

import (
	"fmt"
	"os"
)

type ProbeData struct{}

func main() {
	for _, in := range os.Args[1:] {
		fmt.Println(ParseProbe(in, &ProbeData{}))
	}
}
`
	plainTokenMode = `package main

import (
	"fmt"
	"os"
	"strings"
)

type ProbeData struct{}

func main() {
	for _, in := range os.Args[1:] {
		fmt.Println(ParseProbe(strings.Fields(in), &ProbeData{}))
	}
}
`
)

// probe generates a parser from the grammar in each mode and checks what it
// prints for each input. The inputs are given per mode, since a token mode
// parser takes its input split at spaces, which moves the positions errors
// report.
func probe(t *testing.T, grammar string, modes ...[][2]string) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds and runs a generated parser")
	}
	for i, mode := range []struct {
		name  string
		main  string
		flags []string
	}{
		{"string", plainStringMode, nil},
		{"token", plainTokenMode, []string{"-token"}},
	} {
		tests := modes[i]
		t.Run(mode.name, func(t *testing.T) {
			var args, want []string
			for _, tt := range tests {
				args = append(args, tt[0])
				want = append(want, tt[1])
			}
			dir := generate(t, grammar, map[string]string{"main.go": mode.main}, mode.flags...)
			got := run(t, dir, "go", append([]string{"run", "."}, args...)...)
			if want := strings.Join(want, "\n") + "\n"; got != want {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// TestLabelError checks a production that has both a label and an Error
// fragment: the fragment sees the error with the label applied, and the
// error it returns survives a sibling attempt that fails further on.
func TestLabelError(t *testing.T) {
	probe(t, "labelerror.b",
		[][2]string{
			{"<x>", "<nil>"},
			{"<x,x>", "<nil>"},
			// the label is applied before the Error fragment runs
			{"", "at 0: expected a pair"},
			// the fragment's error is reported at the position Pair's own
			// errors were at, so Tail's expectations don't outrank it
			{"<x,>", "at 3: expected 'x'"},
		},
		[][2]string{
			{"< x >", "<nil>"},
			{"< x , x >", "<nil>"},
			{"", "at 0: expected a pair"},
			{"< x , >", "at 3: expected 'x'"},
		})
}

// TestTrailingInput checks that input left over after the top production
// matches is an error, whether or not the parser has anything else to
// report.
func TestTrailingInput(t *testing.T) {
	probe(t, "trailing.b",
		[][2]string{
			{"x", "<nil>"},
			{"x y", "<nil>"},
			// what the parser expected, when it expected something
			{"x z", "expected 'y'"},
			// and the trailing input itself, when the error stack is empty
			{"x y z", "unexpected input at offset 4"},
		},
		[][2]string{
			{"x", "<nil>"},
			{"x y", "<nil>"},
			{"x z", "expected 'y'"},
			{"x y z", "unexpected input at token 2"},
		})
}

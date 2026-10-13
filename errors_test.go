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
		return 0, "", parseExpected("a name")
	case "if":
		return 0, "", errors.New("if is reserved")
	}
	return n, name, nil
}

func (p *ProbeData) lexnumber(input string) (int, string, error) {
	n, number := lex(input, func(c byte) bool { return c >= '0' && c <= '9' })
	if number == "" {
		return 0, "", parseExpected("a number")
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
		return 0, "", parseExpected("a name")
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
	return 0, "", parseExpected("a number")
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
}

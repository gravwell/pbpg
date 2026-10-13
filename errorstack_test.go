/*************************************************************************
* Copyright 2026 Gravwell, Inc. All rights reserved.
* Contact: <legal@gravwell.io>
*
* This software may be modified and distributed under the terms of the
* BSD 2-clause license. See the LICENSE file for details.
**************************************************************************/
package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// These test the generated parsers' error stack through the copy in pbpg.go,
// which pbpg generates from the errorRecovery template, so a change to the
// template is tested once pbpg.go is regenerated.

// scopes renders the stack's open scopes, outermost first, as
// "[a@5 'b'@5 <c>@5]": a real error, an expected literal, and another
// expectation, such as a label. It fails the test if a scope's errors don't
// all rank alike, which pop relies on.
func scopes(t *testing.T, e *pbpgErrorStack) string {
	t.Helper()
	bounds := append([]int{0}, e.scopes...)
	bounds = append(bounds, len(e.stack))
	var s []string
	for i := 0; i+1 < len(bounds); i++ {
		var errs []string
		for _, v := range e.stack[bounds[i]:bounds[i+1]] {
			if first := e.stack[bounds[i]]; v.rank() != first.rank() {
				t.Errorf("scope %d has errors ranked %d and %d", i, first.rank(), v.rank())
			}
			switch {
			case v.err != nil:
				errs = append(errs, fmt.Sprintf("%v@%d", v.err, v.pos))
			case v.literal:
				errs = append(errs, fmt.Sprintf("'%v'@%d", v.expected, v.pos))
			default:
				errs = append(errs, fmt.Sprintf("<%v>@%d", v.expected, v.pos))
			}
		}
		s = append(s, "["+strings.Join(errs, " ")+"]")
	}
	return strings.Join(s, " ")
}

func TestErrorStack(t *testing.T) {
	var e pbpgErrorStack
	check := func(want string) {
		t.Helper()
		if got := scopes(t, &e); got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
	errAt := func(msg string, pos int) {
		e.error(errors.New(msg), pos)
	}

	// an error behind the scope's is dropped, a tie is kept, and an error
	// past them replaces them
	errAt("a", 5)
	errAt("b", 3)
	check("[a@5]")
	errAt("c", 5)
	check("[a@5 c@5]")
	errAt("d", 7)
	check("[d@7]")

	// a closing scope merges into its parent the same way
	e.push()
	errAt("e", 6)
	check("[d@7] [e@6]")
	e.pop()
	check("[d@7]")
	e.push()
	errAt("f", 7)
	e.pop()
	check("[d@7 f@7]")
	e.push()
	errAt("g", 9)
	e.pop()
	check("[g@9]")

	// an empty scope leaves its parent as it was
	e.push()
	e.pop()
	check("[g@9]")

	// a scope merges into an empty parent, and through it into the next
	e.push()
	e.push()
	errAt("h", 4)
	check("[g@9] [] [h@4]")
	e.pop()
	check("[g@9] [h@4]")
	e.pop()
	check("[g@9]")
	e.push()
	e.push()
	errAt("i", 12)
	e.pop()
	e.pop()
	check("[i@12]")

	// clear, depth and coalesce see only the innermost scope
	e.push()
	if d := e.depth(); d != 0 {
		t.Errorf("empty scope's depth = %d, want 0", d)
	}
	if err := e.coalesce(); err != nil {
		t.Errorf("empty scope coalesces to %v, want nil", err)
	}
	errAt("j", 3)
	if d := e.depth(); d != 3 {
		t.Errorf("depth = %d, want 3", d)
	}
	e.clear()
	check("[i@12] []")
	e.pop()
	check("[i@12]")
	if d := e.depth(); d != 12 {
		t.Errorf("depth = %d, want 12", d)
	}
}

func TestErrorStackRank(t *testing.T) {
	var e pbpgErrorStack
	check := func(want string) {
		t.Helper()
		if got := scopes(t, &e); got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}

	// literals and pbpgExpected errors are both expectations
	e.expect("x", 5)
	e.error(pbpgExpected("num"), 5)
	check("['x'@5 <num>@5]")

	// at one position, a real error replaces expectations, and an
	// expectation isn't recorded after one
	e.error(errors.New("a"), 5)
	check("[a@5]")
	e.expect("y", 5)
	e.error(pbpgExpected("num"), 5)
	check("[a@5]")
	e.error(errors.New("b"), 5)
	check("[a@5 b@5]")

	// but an expectation further on replaces it
	e.expect("y", 6)
	check("['y'@6]")

	// a closing scope merges into its parent by rank too
	e.push()
	e.error(errors.New("c"), 6)
	e.pop()
	check("[c@6]")
	e.push()
	e.expect("z", 6)
	e.pop()
	check("[c@6]")
	e.push()
	e.expect("z", 7)
	e.pop()
	check("['z'@7]")
}

func TestErrorStackLabel(t *testing.T) {
	p := newpbpgParser("ab  cd", nil)
	e := p.errorStack
	label := func(start int, want string) {
		t.Helper()
		p.label("L", start)
		if got := scopes(t, e); got != want {
			t.Errorf("got %s, want %s", got, want)
		}
		e.clear()
	}

	// the expectations of a production that failed where it started are
	// replaced by its label
	e.expect("x", 2)
	e.error(pbpgExpected("num"), 2)
	label(2, "[<L>@2]")

	// so are those past only whitespace, at their own position
	e.expect("x", 4)
	label(2, "[<L>@4]")

	// but not those past input the production matched
	e.expect("x", 4)
	label(0, "['x'@4]")

	// nor real errors
	e.error(errors.New("a"), 2)
	label(2, "[a@2]")

	// a production that failed without recording anything expected what
	// its label names
	label(3, "[<L>@3]")

	// only the innermost scope, the production's own, is labeled
	e.expect("x", 1)
	e.push()
	e.expect("y", 1)
	label(1, "['x'@1] [<L>@1]")
	e.pop()
	if got, want := scopes(t, e), "['x'@1]"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// listErr is an error of an uncomparable type: comparing two of them as
// interface values panics.
type listErr []string

func (l listErr) Error() string { return strings.Join(l, " ") }

func TestErrorStackCoalesce(t *testing.T) {
	var e pbpgErrorStack
	one := errors.New("expected x")
	e.error(one, 4)
	if err := e.coalesce(); err != one {
		t.Errorf("one error coalesces to %v, want the error itself", err)
	}

	// errors are deduplicated by message, and listed newest first
	e.error(errors.New("expected x"), 4)
	e.error(errors.New("expected y"), 4)
	e.error(listErr{"expected", "z"}, 4)
	e.error(listErr{"expected", "z"}, 4)
	if got, want := e.coalesce().Error(), "expected z\nexpected y\nexpected x"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// expectations make one message, which lists each thing expected once,
	// the literals first, quoted, and each group sorted
	tests := []struct {
		add  func()
		want string
	}{
		{func() { e.error(pbpgExpected("a number"), 4) }, "expected a number"},
		{func() { e.expect(";", 4) }, "expected ';'"},
		{func() {
			e.error(pbpgExpected("a number"), 4)
			e.expect(";", 4)
		}, "expected ';' or a number"},
		{func() {
			e.expect("x", 4)
			e.error(pbpgExpected("a number"), 4)
			e.expect("'", 4)
			e.error(pbpgExpected("a name"), 4)
			e.expect("x", 4)
			e.error(pbpgExpected("a number"), 4)
			e.expect("(", 4)
		}, `expected "'", '(', 'x', a name or a number`},
	}
	for _, tt := range tests {
		e.clear()
		tt.add()
		if got := e.coalesce().Error(); got != tt.want {
			t.Errorf("got %q, want %q", got, tt.want)
		}
	}
}

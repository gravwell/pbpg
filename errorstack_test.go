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

// scopes renders the stack's open scopes, outermost first, as "[a@5 b@5]",
// failing the test if a scope's errors aren't all at one position, which pop
// relies on.
func scopes(t *testing.T, e *parserErrorStack) string {
	t.Helper()
	bounds := append([]int{0}, e.scopes...)
	bounds = append(bounds, len(e.stack))
	var s []string
	for i := 0; i+1 < len(bounds); i++ {
		var errs []string
		for _, v := range e.stack[bounds[i]:bounds[i+1]] {
			if v.pos != e.stack[bounds[i]].pos {
				t.Errorf("scope %d has errors at %d and %d", i, e.stack[bounds[i]].pos, v.pos)
			}
			errs = append(errs, fmt.Sprintf("%v@%d", v.err, v.pos))
		}
		s = append(s, "["+strings.Join(errs, " ")+"]")
	}
	return strings.Join(s, " ")
}

func TestErrorStack(t *testing.T) {
	var e parserErrorStack
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

// listErr is an error of an uncomparable type: comparing two of them as
// interface values panics.
type listErr []string

func (l listErr) Error() string { return strings.Join(l, " ") }

func TestErrorStackCoalesce(t *testing.T) {
	var e parserErrorStack
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
}

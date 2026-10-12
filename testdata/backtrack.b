# A grammar whose parses show what the parser undoes when an attempt fails:
# Data's state, through Data's savepoints, the position, and the variables
# the attempt set. See TestBacktracking.
{
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ProbeData records the declarations that Decl's action makes, and undoes
// those of failed attempts through its savepoints.
type ProbeData struct {
	decls []string
	marks []int // len(decls) at each open savepoint
}

func (p *ProbeData) savepoint() {
	p.marks = append(p.marks, len(p.decls))
}

func (p *ProbeData) rollback() {
	p.decls = p.decls[:p.marks[len(p.marks)-1]]
	p.marks = p.marks[:len(p.marks)-1]
}

func (p *ProbeData) release() {
	p.marks = p.marks[:len(p.marks)-1]
}

var _ ProbeSavepointer = (*ProbeData)(nil)

// main parses each argument, printing the result, the error, the
// declarations, and the savepoints left open, which should be none.
func main() {
	for _, in := range os.Args[1:] {
		d := &ProbeData{}
		r, err := ParseProbe(in, d)
		fmt.Printf("%v | %v | %q | %d\n", r, err, d.decls, len(d.marks))
	}
}

// lexword lexes a word of lower case letters after any spaces.
func (p *ProbeData) lexword(input string) (int, string, error) {
	n := 0
	for n < len(input) && input[n] == ' ' {
		n++
	}
	start := n
	for n < len(input) && input[n] >= 'a' && input[n] <= 'z' {
		n++
	}
	if n == start {
		return 0, "", errors.New("expected word")
	}
	return n, input[start:n], nil
}
}

type Start string
type Decls string
type Decl string
type Alts string
type Opt string
type Rep string
type OptRep string
type List string
type Word string

Start	= ( "1" Decls ) | ( "2" Alts ) | ( "3" Opt ) | ( "4" Rep ) |
	  ( "5" OptRep ) | ( "6" List ) .				Action {
									switch a1Pos {
									case 1:
										return v2
									case 2:
										return v4
									case 3:
										return v6
									case 4:
										return v8
									case 5:
										return v10
									case 6:
										return v12
									}
									return ""
								}
# Decl's action runs before the ! fails to match.
Decls	= ( Decl "!" ) | Word .					Action {
									if a1Pos == 1 {
										return "decl " + v1
									}
									return "word " + v3
								}
Decl	= Word .						Action {
									p.decls = append(p.decls, v1)
									return v1
								}
# The second alternative starts where the first did.
Alts	= "a" "x" | "a" "y" .					Action { return fmt.Sprint("alternative ", a1Pos) }
# A failed option leaves no label.
Opt	= [ Word ":" ] Word .					Action { return fmt.Sprintf("label=%q name=%q", v1, v3) }
# An iteration sees nothing of the one before.
Rep	= { Word | "!" } .					Action { return fmt.Sprintf("words=%q bangs=%q", v1, v2) }
OptRep	= { [ Word ":" ] "!" } .				Action { return fmt.Sprintf("labels=%q colons=%q", v1, v2) }
# A failed option's repetition leaves an empty slice.
List	= [ Word { "," Word } ":" ] Word { "," Word } .		Action {
									return fmt.Sprintf("label=%q labels=%q name=%q names=%q", v1, v3, v5, v7)
								}
Word	= lex(word) .						Action { return v1 }

/*************************************************************************
* Copyright 2022 Gravwell, Inc. All rights reserved.
* Contact: <legal@gravwell.io>
*
* This software may be modified and distributed under the terms of the
* BSD 2-clause license. See the LICENSE file for details.
**************************************************************************/
package main

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

const (
	TERM_NAME = iota
	TERM_LITERAL
	TERM_GOR
	TERM_LEX

	GOR_GROUP = iota
	GOR_OPTION
	GOR_REPETITION
)

type pbpgData struct {
	typeMap       map[string]string
	stateMap      map[string]*Expression // list of productions
	orderedStates []string               // list of productions in source order
	out           bytes.Buffer           // output buffer, which is combined with the boiler plate code and formatted on success

	statesUsed map[string]bool // list of states referenced in terms. Used for grammar validation.

	labels map[string]string // the labels of the productions that have one

	entryPoint string // The name of the first encountered production.

	// savepoints and undo make pbpgData a pbpgSavepointer, so an attempt
	// that fails takes back the types, productions, and output its actions
	// added. undo holds a function that reverts each map entry set while an
	// attempt is open, oldest first.
	savepoints []pbpgSavepoint
	undo       []func()

	// saveCount numbers the snapshots attemptSaves emits within the state
	// function being emitted; emitState resets it so a production's output
	// doesn't depend on how the productions before it parsed.
	saveCount int
}

// pbpgSavepoint is the state to restore if the attempt that saved it fails.
type pbpgSavepoint struct {
	out, orderedStates, undo int
	entryPoint               string
}

var _ pbpgSavepointer = (*pbpgData)(nil)

func (p *pbpgData) savepoint() {
	p.savepoints = append(p.savepoints, pbpgSavepoint{
		out:           p.out.Len(),
		orderedStates: len(p.orderedStates),
		undo:          len(p.undo),
		entryPoint:    p.entryPoint,
	})
}

func (p *pbpgData) rollback() {
	s := p.savepoints[len(p.savepoints)-1]
	p.savepoints = p.savepoints[:len(p.savepoints)-1]
	for i := len(p.undo) - 1; i >= s.undo; i-- {
		p.undo[i]()
	}
	p.undo = p.undo[:s.undo]
	p.out.Truncate(s.out)
	p.orderedStates = p.orderedStates[:s.orderedStates]
	p.entryPoint = s.entryPoint
}

func (p *pbpgData) release() {
	p.savepoints = p.savepoints[:len(p.savepoints)-1]
	if len(p.savepoints) == 0 {
		p.undo = p.undo[:0]
	}
}

// setEntry sets m[k] to v, and while an attempt is open, records how to take
// it back. A write that changes nothing is skipped rather than logged: nearly
// every statesUsed write re-sets an entry that's already true, and logging
// those would grow the undo log with every term parsed.
func setEntry[V comparable](p *pbpgData, m map[string]V, k string, v V) {
	if old, ok := m[k]; ok {
		if old == v {
			return
		}
		if len(p.savepoints) > 0 {
			p.undo = append(p.undo, func() { m[k] = old })
		}
	} else if len(p.savepoints) > 0 {
		p.undo = append(p.undo, func() { delete(m, k) })
	}
	m[k] = v
}

type Variable struct {
	Value      string
	T          int
	Repetition bool
}

// verify does the following:
//  1. Ensures all productions used are defined.
//  2. All productions defined are used when starting from the entrypoint.
func (p *pbpgData) verify() error {
	// 1
	for k := range p.statesUsed {
		if p.stateMap[k] == nil {
			return fmt.Errorf("state %v not defined", k)
		}
	}

	// 2
	// First, build a list of used states when starting from the entrypoint.
	used := []string{p.entryPoint} // the entrypoint is used by definition

	names := p.stateMap[p.entryPoint].enumerateNames()
	for len(names) > 0 {
		var nextNames []string
		for _, v := range names {
			used = append(used, v)
			e := p.stateMap[v].enumerateNames()
		NEXT_LOOP:
			for _, w := range e {
				for _, x := range used {
					if w == x {
						continue NEXT_LOOP
					}
				}
				nextNames = append(nextNames, w)
			}
		}
		names = nextNames
	}

USED_LOOP:
	for k := range p.stateMap {
		for _, v := range used {
			if k == v {
				continue USED_LOOP
			}
		}
		return fmt.Errorf("state %v defined but not used", k)
	}

	return nil
}

// lexFunctions returns a map of all lexer function names referenced by the
// given expression. This is used to build the stub function definitions when
// using -stub.
func lexFunctions(e *Expression) map[string]bool {
	m := make(map[string]bool)

	for _, a := range e.alternatives {
		for _, t := range a.terms {
			switch t.option {
			case TERM_LEX:
				m[t.lex] = true
			case TERM_GOR:
				x := lexFunctions(t.gor.expression)
				for k := range x {
					m[k] = true
				}
			}
		}
	}

	return m
}

// An expression as defined by the ebnf in pbpg. Expressions contain one or
// more Alternatives.
type Expression struct {
	alternatives []*Alternative
}

func (e *Expression) String() string {
	var s []string
	for _, v := range e.alternatives {
		s = append(s, v.String())
	}
	return strings.Join(s, " | ")
}

// enumerateNames recursively builds a list of all production names referenced
// by the expression.
func (e *Expression) enumerateNames() []string {
	var s []string
	for _, a := range e.alternatives {
		for _, t := range a.terms {
			if t.option == TERM_NAME {
				s = append(s, t.name)
			} else if t.option == TERM_GOR {
				s = append(s, t.gor.expression.enumerateNames()...)
			}
		}
	}
	return s
}

func (e *Expression) variables() []*Variable {
	var r []*Variable

	// we have to walk the expression tree, generating an in order list of
	// variables we find.

	for _, a := range e.alternatives {
		for _, t := range a.terms {
			switch t.option {
			case TERM_NAME:
				r = append(r, &Variable{
					T:     TERM_NAME,
					Value: t.name,
				})
			case TERM_LITERAL:
				r = append(r, &Variable{
					T: TERM_LITERAL,
				})
			case TERM_LEX:
				r = append(r, &Variable{
					T: TERM_LEX,
				})
			case TERM_GOR:
				rg := t.gor.expression.variables()
				if t.gor.option == GOR_REPETITION {
					for _, v := range rg {
						v.Repetition = true
					}
				}
				r = append(r, rg...)
			}
		}
	}

	return r
}

func (e *Expression) numAlternativeGroups() int {
	var r int
	if len(e.alternatives) > 1 {
		r++
	}
	for _, a := range e.alternatives {
		for _, t := range a.terms {
			if t.option == TERM_GOR {
				r += t.gor.expression.numAlternativeGroups()
			}
		}
	}
	return r
}

// typedVariables calls f for each variable of exp that the generated code
// binds a value to — every term except a production without a type — with its
// number, counted from first, and its Go type. Everything that refers to
// variables by number (declarators, positionalArgs, functionSignature,
// zeroVars, attemptSaves, and the repetition appends) numbers them through
// this one walk, so they can't drift apart.
func (p *pbpgData) typedVariables(exp *Expression, first int, f func(c int, v *Variable, ftype string)) {
	c := first
	for _, v := range exp.variables() {
		ftype := "string"
		if v.T == TERM_NAME {
			var ok bool
			if ftype, ok = p.typeMap[v.Value]; !ok {
				continue
			}
		}
		f(c, v, ftype)
		c++
	}
}

func (p *pbpgData) declarators(exp *Expression) string {
	var r string

	ag := exp.numAlternativeGroups()
	for i := 1; i <= ag; i++ {
		r += fmt.Sprintf("var a%vPos int\n", i)
	}

	p.typedVariables(exp, 1, func(c int, v *Variable, ftype string) {
		if v.Repetition {
			r += fmt.Sprintf("var v%vtemp %v\n", c, ftype)
			r += fmt.Sprintf("var v%v []%v\n", c, ftype)
		} else {
			r += fmt.Sprintf("var v%v %v\n", c, ftype)
		}
	})
	return r
}

func (p *pbpgData) positionalArgs(exp *Expression) string {
	var r string

	ag := exp.numAlternativeGroups()
	for i := 1; i <= ag; i++ {
		r += fmt.Sprintf("a%vPos,", i)
	}

	p.typedVariables(exp, 1, func(c int, v *Variable, ftype string) {
		r += fmt.Sprintf("v%v,", c)
	})
	return strings.TrimSuffix(r, ",")
}

func (p *pbpgData) functionSignature(exp *Expression) string {
	var r string

	ag := exp.numAlternativeGroups()
	for i := 1; i <= ag; i++ {
		r += fmt.Sprintf("a%vPos int,", i)
	}

	p.typedVariables(exp, 1, func(c int, v *Variable, ftype string) {
		if v.Repetition {
			r += fmt.Sprintf("v%v []%v,", c, ftype)
		} else {
			r += fmt.Sprintf("v%v %v,", c, ftype)
		}
	})
	return strings.TrimSuffix(r, ",")
}

// zeroVars returns the statements that reset the variables of exp, numbered
// from first, to their zero values, so that an action sees the terms of a
// failed attempt as unmatched rather than as what they held when it failed.
// In a repetition they're the temporaries of the current iteration, and
// otherwise a repetition in exp has a slice, which is reset to nil. What a
// failed attempt added to state that predates it — a nested repetition's
// slice inside an enclosing repetition, and the alternative positions it set
// — is restored by attemptSaves rather than zeroed here. Without an action or
// error block there are no variables to reset.
func (p *pbpgData) zeroVars(exp *Expression, first int, rep bool, hasAction bool) string {
	if !hasAction {
		return ""
	}
	var r strings.Builder
	p.typedVariables(exp, first, func(c int, v *Variable, ftype string) {
		switch {
		case rep:
			r.WriteString(fmt.Sprintf("v%vtemp = %v\n", c, zeroValue(ftype)))
		case v.Repetition:
			r.WriteString(fmt.Sprintf("v%v = nil\n", c))
		default:
			r.WriteString(fmt.Sprintf("v%v = %v\n", c, zeroValue(ftype)))
		}
	})
	return r.String()
}

// attemptSaves returns paired statements for the speculative attempt over
// exp, whose typed variables are numbered from vFirst and whose alternative
// groups follow aCount: saves runs as the attempt begins, and resets runs
// when it backtracks. An attempt can leave more than its own scalars behind —
// a nested repetition appends to its slice as it iterates, and an alternative
// group records its position as soon as it's tried. Inside a repetition
// (rep), what earlier, accepted iterations added must survive the reset, so
// saves snapshots the slice lengths and positions for resets to restore;
// elsewhere nothing came before, so resets clears the positions directly and
// zeroVars' nil reset covers the slices. The attempt's own scalars are
// zeroVars' job.
func (p *pbpgData) attemptSaves(exp *Expression, vFirst, aCount int, rep bool, hasAction bool) (string, string) {
	if !hasAction {
		return "", ""
	}
	var saves, resets strings.Builder
	p.saveCount++
	id := p.saveCount
	if rep {
		p.typedVariables(exp, vFirst, func(c int, v *Variable, ftype string) {
			if !v.Repetition {
				return
			}
			saves.WriteString(fmt.Sprintf("s%vv%v := len(v%v)\n", id, c, c))
			resets.WriteString(fmt.Sprintf("v%v = v%v[:s%vv%v]\n", c, c, id, c))
		})
	}
	for i := aCount + 1; i <= aCount+exp.numAlternativeGroups(); i++ {
		if rep {
			saves.WriteString(fmt.Sprintf("s%va%v := a%vPos\n", id, i, i))
			resets.WriteString(fmt.Sprintf("a%vPos = s%va%v\n", i, id, i))
		} else {
			resets.WriteString(fmt.Sprintf("a%vPos = 0\n", i))
		}
	}
	return saves.String(), resets.String()
}

// zeroValue returns an expression for the zero value of the Go type ftype,
// which is the rest of its type declaration's line. *new(T) is the zero value
// of any type; strings, bools, and numbers get their literal spelling only to
// keep the generated code readable.
func zeroValue(ftype string) string {
	ftype = strings.TrimSpace(ftype)
	switch {
	case ftype == "string":
		return `""`
	case ftype == "bool":
		return "false"
	case basicNumeric[ftype]:
		return "0"
	}
	return fmt.Sprintf("*new(%v)", ftype)
}

var basicNumeric = map[string]bool{
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"uintptr": true, "byte": true, "rune": true, "float32": true, "float64": true,
}

// An alternative contains one or more terms.
type Alternative struct {
	terms []*Term
}

func (a *Alternative) String() string {
	var s []string
	for _, v := range a.terms {
		s = append(s, v.String())
	}
	return strings.Join(s, " ")
}

// mayFailPartway reports whether the alternative can move the parser forward
// and then fail. A literal that fails doesn't move it, and a group, option, or
// repetition undoes its own failed attempts. Anything else can: a sequence can
// fail after its first term matches, a production likewise, and a lexer
// function can consume input along with its error.
func (a *Alternative) mayFailPartway() bool {
	if len(a.terms) != 1 {
		return true
	}
	switch a.terms[0].option {
	case TERM_LITERAL, TERM_GOR:
		return false
	}
	return true
}

// A term is either a production name, string literal, lexer function, or a
// group/option/repetition expression.
type Term struct {
	option int

	name    string
	literal string
	gor     *GOR
	lex     string
}

func (t *Term) String() string {
	switch t.option {
	case TERM_NAME:
		return t.name
	case TERM_LITERAL:
		return fmt.Sprintf("\"%v\"", t.literal)
	case TERM_LEX:
		return t.lex
	case TERM_GOR:
		return t.gor.String()
	}
	return "invalid term type"
}

// A GOR is a group/option/repetition expression, identified by the option
// value.
type GOR struct {
	option int

	expression *Expression
}

func (g *GOR) String() string {
	switch g.option {
	case GOR_GROUP:
		return fmt.Sprintf("( %v )", g.expression.String())
	case GOR_OPTION:
		return fmt.Sprintf("[ %v ]", g.expression.String())
	case GOR_REPETITION:
		return fmt.Sprintf("{ %v }", g.expression.String())
	}
	return "invalid GOR type"
}

// productionName returns how a production's name is written in the grammar:
// the name, followed by its label, if it has one, quoted.
func productionName(name, label string) string {
	if label == "" {
		return name
	}
	return name + " " + strconv.Quote(label)
}

// emitState is called in the action of the Production production. It writes a
// state function and walks the given expression (via the visit* functions) to
// write the logic for the production.
func (p *pbpgData) emitState(name, label string, exp *Expression, a, e string) {
	hasActionError := a != "" || e != ""
	// make the comment of the current production
	p.out.WriteString(fmt.Sprintf("// %v = %v\n", productionName(name, label), exp.String()))

	ftype, hasType := p.typeMap[name]
	var retType string
	if hasType {
		retType = ftype + ", "
	}
	p.out.WriteString(fmt.Sprintf("func (p *%vParser) state%v() (%v error) {\nvar err error\n", *fPrefix, name, retType))

	if e != "" || label != "" {
		p.out.WriteString("entryPos := p.pos\n")
	}

	if hasType {
		p.out.WriteString(fmt.Sprintf("var ret %v\n", ftype))
	}
	if p.declarators(exp) != "" && hasActionError {
		p.out.WriteString(p.declarators(exp))
	}

	if *fDebug {
		p.out.WriteString(fmt.Sprintf("log.Println(\"state%v\")\n", name))
	}

	p.saveCount = 0
	p.visitExpression(1, 0, exp, false, hasActionError)

	// the label is applied first, so the Error function sees the error with
	// it applied
	if label != "" {
		p.out.WriteString(fmt.Sprintf("if err != nil { p.label(%v, entryPos) }\n\n", strconv.Quote(label)))
	}

	pa := p.positionalArgs(exp)
	if a != "" {
		if hasType {
			p.out.WriteString(fmt.Sprintf("if err == nil { ret = p.Data.action%v(p.pos, %v) }\n\n", name, pa))
		} else {
			p.out.WriteString(fmt.Sprintf("if err == nil { p.Data.action%v(p.pos, %v) }\n\n", name, pa))
		}
	}
	if e != "" {
		var args string
		if pa != "" {
			args = ", " + pa
		}
		// the replacement error is recorded where the production's own
		// errors were, not at p.pos, which a failure partway through
		// leaves behind them: recorded at p.pos it would rank below a
		// sibling attempt's expectations and be dropped
		p.out.WriteString(fmt.Sprintf("if err != nil { terr := p.errorStack.coalesce(); errPos := p.errorStack.depth(); rerr := p.Data.error%v(entryPos, errPos, terr %v); if rerr != terr { p.errorStack.clear(); p.errorStack.error(rerr, errPos); } }\n\n", name, args))
	}

	if hasType {
		p.out.WriteString("return ret, err\n}\n\n")
	} else {
		p.out.WriteString("return err\n}\n\n")
	}

	fs := p.functionSignature(exp)
	if a != "" {
		if hasType {
			p.out.WriteString(fmt.Sprintf("func (p *%vData) action%v(pos int, %v) %v { %v\n}\n\n", *fPrefix, name, fs, ftype, a))
		} else {
			p.out.WriteString(fmt.Sprintf("func (p *%vData) action%v(pos int, %v) { %v\n}\n\n", *fPrefix, name, fs, a))
		}
	}
	if e != "" {
		var args string
		if fs != "" {
			args = ", " + fs
		}
		p.out.WriteString(fmt.Sprintf("func (p *%vData) error%v(pos int, errPos int, err error %v) error {\n%v\n}\n\n", *fPrefix, name, args, e))
	}
}

func (p *pbpgData) visitExpression(vCount int, aCount int, exp *Expression, rep bool, hasAction bool) (int, int) {
	// pos is this expression's own alternative group number; aCount keeps
	// counting through the groups nested in the alternatives, so sibling
	// groups get distinct numbers, matching numAlternativeGroups.
	var needPos bool
	var pos int
	if len(exp.alternatives) > 1 && hasAction {
		aCount++
		pos = aCount
		needPos = true
	}

	for i, v := range exp.alternatives {
		if needPos {
			p.out.WriteString(fmt.Sprintf("a%vPos = %v\n", pos, i+1))
		}
		// An alternative that fails after matching part of the input is
		// undone before the next one is tried, so that every alternative
		// starts where the first did. The last needs no undoing here: when
		// it fails, the expression does, and whatever tried the expression
		// undoes it.
		undo := i < len(exp.alternatives)-1 && v.mayFailPartway()
		alt := &Expression{alternatives: []*Alternative{v}}
		var resets string
		if undo {
			var saves string
			saves, resets = p.attemptSaves(alt, vCount, aCount, rep, hasAction)
			p.out.WriteString(saves)
			p.out.WriteString("p = p.predict()\n")
		}
		vStart := vCount
		vCount, aCount = p.visitAlternative(vCount, aCount, v, rep, hasAction)
		if undo {
			p.out.WriteString(fmt.Sprintf("if err != nil {\np = p.backtrack()\n%v%v} else {\np = p.accept()\n}\n", resets, p.zeroVars(alt, vStart, rep, hasAction)))
		}
		if i < len(exp.alternatives)-1 {
			p.out.WriteString("if err != nil { \n")
		} else if needPos {
			p.out.WriteString("if err != nil { \n")
			p.out.WriteString(fmt.Sprintf("a%vPos = -1\n", pos))
		}
	}
	for i := range exp.alternatives {
		if i < len(exp.alternatives)-1 || needPos {
			p.out.WriteString("}\n")
		}
	}
	return vCount, aCount
}

func (p *pbpgData) visitAlternative(vCount int, aCount int, alt *Alternative, rep bool, hasAction bool) (int, int) {
	for i, v := range alt.terms {
		vCount, aCount = p.visitTerm(vCount, aCount, v, rep, hasAction)
		if i < len(alt.terms)-1 {
			p.out.WriteString("if err == nil {\n")
		}
	}
	for i := range alt.terms {
		if i < len(alt.terms)-1 {
			p.out.WriteString("}\n")
		}
	}
	return vCount, aCount
}

func (p *pbpgData) visitTerm(vCount int, aCount int, term *Term, rep bool, hasAction bool) (int, int) {
	switch term.option {
	case TERM_NAME:
		// states get their own error scope, which is merged into the
		// parent's when it pops (see _PREFIX_ErrorStack), while
		// clear/coalesce/depth operate on the innermost open scope only
		p.out.WriteString("p.errorStack.push()\n")

		if _, ok := p.typeMap[term.name]; ok {
			if hasAction {
				if rep {
					p.out.WriteString(fmt.Sprintf("v%vtemp, err = p.state%v()\n", vCount, term.name))
				} else {
					p.out.WriteString(fmt.Sprintf("v%v, err = p.state%v()\n", vCount, term.name))
				}
			} else {
				p.out.WriteString(fmt.Sprintf("_, err = p.state%v()\n", term.name))
			}
			vCount++
		} else {
			p.out.WriteString(fmt.Sprintf("err = p.state%v()\n", term.name))
		}
		if p.statesUsed == nil {
			p.statesUsed = make(map[string]bool)
		}
		setEntry(p, p.statesUsed, term.name, true)

		p.out.WriteString("p.errorStack.pop()\n")
	case TERM_LITERAL:
		// literal records what it expected in the error stack itself
		if hasAction {
			if rep {
				p.out.WriteString(fmt.Sprintf("v%vtemp, err = p.literal(%v)\n", vCount, strconv.Quote(term.literal)))
			} else {
				p.out.WriteString(fmt.Sprintf("v%v, err = p.literal(%v)\n", vCount, strconv.Quote(term.literal)))
			}
		} else {
			p.out.WriteString(fmt.Sprintf("_, err = p.literal(%v)\n", strconv.Quote(term.literal)))
		}
		vCount++
	case TERM_GOR:
		vCount, aCount = p.visitGOR(vCount, aCount, term.gor, rep, hasAction)
	case TERM_LEX:
		if hasAction {
			if rep {
				p.out.WriteString(fmt.Sprintf("{ n, lexeme, lerr := p.Data.lex%v(p.input[p.pos:]); p.pos += n; if lerr != nil { err = lerr } else { err = nil; v%vtemp = lexeme }; };", term.lex, vCount))
			} else {
				p.out.WriteString(fmt.Sprintf("{ n, lexeme, lerr := p.Data.lex%v(p.input[p.pos:]); p.pos += n; if lerr != nil { err = lerr } else { err = nil; v%v = lexeme }; };", term.lex, vCount))
			}
		} else {
			p.out.WriteString(fmt.Sprintf("{ n, _, lerr := p.Data.lex%v(p.input[p.pos:]); p.pos += n; if lerr != nil { err = lerr } else { err = nil; }; };", term.lex))
		}
		vCount++
		p.out.WriteString("if err != nil { p.errorStack.error(err, p.pos) }\n")
	}
	return vCount, aCount
}

// Subexpressions, which are visited in groups, are speculative attempts: the
// parser saves its state (predict), tries the expression, and then either
// restores the state if the expression fails (backtrack) or keeps the result
// (accept). A failed attempt's variables are reset too (zeroVars and
// attemptSaves), and each repetition starts with its temporaries reset, so
// that an action sees only what matched.
func (p *pbpgData) visitGOR(vCount int, aCount int, gor *GOR, rep bool, hasAction bool) (int, int) {
	vStart := vCount
	switch gor.option {
	case GOR_GROUP:
		p.out.WriteString("// group\n")
		saves, resets := p.attemptSaves(gor.expression, vStart, aCount, rep, hasAction)
		p.out.WriteString(saves)
		p.out.WriteString("p = p.predict()\n")
		vCount, aCount = p.visitExpression(vCount, aCount, gor.expression, rep, hasAction)
		p.out.WriteString(fmt.Sprintf("if err != nil {\np = p.backtrack()\n%v%v} else {\np = p.accept()\n}\n", resets, p.zeroVars(gor.expression, vStart, rep, hasAction)))
	case GOR_OPTION:
		p.out.WriteString("// option\n")
		saves, resets := p.attemptSaves(gor.expression, vStart, aCount, rep, hasAction)
		p.out.WriteString(saves)
		p.out.WriteString("p = p.predict()\n")
		vCount, aCount = p.visitExpression(vCount, aCount, gor.expression, rep, hasAction)
		p.out.WriteString(fmt.Sprintf("if err != nil {\np = p.backtrack()\nerr = nil\n%v%v} else {\np = p.accept()\n}\n", resets, p.zeroVars(gor.expression, vStart, rep, hasAction)))
	case GOR_REPETITION:
		// Each iteration is an attempt of its own: it sees the slices and
		// positions earlier iterations accumulated, so a failed one restores
		// them from the snapshots taken as it began.
		p.out.WriteString("// repetition\n")
		p.out.WriteString("for {\n")
		saves, resets := p.attemptSaves(gor.expression, vStart, aCount, true, hasAction)
		p.out.WriteString(saves)
		p.out.WriteString("p = p.predict()\n")
		p.out.WriteString(p.zeroVars(gor.expression, vStart, true, hasAction))
		vCount, aCount = p.visitExpression(vCount, aCount, gor.expression, true, hasAction)
		var acceptAppends string
		if hasAction {
			p.typedVariables(gor.expression, vStart, func(c int, v *Variable, ftype string) {
				// a nested repetition's own loop appends to its slice,
				// one entry per iteration of its own, not per iteration
				// of this one
				if v.Repetition {
					return
				}
				acceptAppends += fmt.Sprintf("v%v = append(v%v, v%vtemp)\n", c, c, c)
			})
		}
		p.out.WriteString(fmt.Sprintf("if err != nil { p = p.backtrack(); err = nil; %vbreak } else { %v p = p.accept() }\n", resets, acceptAppends))
		p.out.WriteString("}\n")
	}
	return vCount, aCount
}

var errorRecovery = `

// _PREFIX_ErrorStack accumulates parse errors in a single reusable backing
// array.  Speculative parse attempts open a scope (push) around each state
// call and close it afterwards (pop); recorded errors are kept when a scope
// pops, while clear, coalesce, and depth operate on the innermost open scope
// only.  This avoids allocating a fresh error stack for every speculative
// attempt, which both burns CPU and creates a new runtime memory-profile
// bucket for every unique recursion stack in long-running processes.
//
// An error is either an expectation, something the parser looked for and
// didn't find (a literal, a production's label, or what a lexer function
// returned as a _PREFIX_Expected), or a real error: any other error from a
// lexer function or an Error function. coalesce reports only the errors
// that rank highest in a scope: those furthest into the input, and at one
// position, real errors over expectations. So a scope keeps only those: an
// error that ranks lower isn't recorded, one that ranks higher replaces
// them, and a scope that pops is merged into its parent the same way. Every
// scope's errors therefore rank alike, and the stack stays small however
// many speculative attempts fail.
type _PREFIX_ErrorStack struct {
	stack  []_PREFIX_ParseError
	scopes []int
	es     []error // scratch buffer reused by coalesce
}

// _PREFIX_ParseError is a real error, or when err is nil, an expectation of
// expected.
type _PREFIX_ParseError struct {
	err      error
	expected string
	literal  bool // expected is a literal, which messages quote
	pos      int
}

// rank orders errors for reporting: errors further into the input rank
// higher, and at one position, real errors rank higher than expectations.
func (pe _PREFIX_ParseError) rank() int {
	if pe.err != nil {
		return 2*pe.pos + 1
	}
	return 2 * pe.pos
}

// _PREFIX_Expected is the error a lexer function returns when the input doesn't
// start with its token, naming the token, such as "a number". The parser
// lists it with the other things it expected at the same position, as in
// "expected ';' or a number", rather than reporting it as an error of its
// own.
type _PREFIX_Expected string

func (e _PREFIX_Expected) Error() string {
	return "expected " + string(e)
}

// _PREFIX_NoMatch is the error literal returns when the input doesn't start
// with the literal. It only signals the failure: literal records what it
// expected in the error stack.
var _PREFIX_NoMatch = errors.New("literal not matched")

// push opens an error scope around a speculative parse attempt.
func (e *_PREFIX_ErrorStack) push() {
	e.scopes = append(e.scopes, len(e.stack))
}

// pop closes the innermost scope, merging it into the parent: only the
// errors that rank highest survive. The closing scope's errors are dropped
// when they rank below the parent's own, they replace the parent's errors
// when they rank above them, and a tie keeps both.
func (e *_PREFIX_ErrorStack) pop() {
	inner := e.scopes[len(e.scopes)-1]
	e.scopes = e.scopes[:len(e.scopes)-1]
	outer, n := e.scopeBase(), len(e.stack)
	if inner == n || inner == outer {
		return
	}
	switch or, ir := e.stack[inner-1].rank(), e.stack[n-1].rank(); {
	case ir < or:
		e.stack = e.stack[:inner]
	case ir > or:
		e.stack = append(e.stack[:outer], e.stack[inner:]...)
	}
}

func (e *_PREFIX_ErrorStack) scopeBase() int {
	if n := len(e.scopes); n > 0 {
		return e.scopes[n-1]
	}
	return 0
}

func (e *_PREFIX_ErrorStack) clear() {
	e.stack = e.stack[:e.scopeBase()]
}

// error records err at pos, as an expectation if it's a _PREFIX_Expected.
func (e *_PREFIX_ErrorStack) error(err error, pos int) {
	if want, ok := err.(_PREFIX_Expected); ok {
		e.add(_PREFIX_ParseError{expected: string(want), pos: pos})
	} else {
		e.add(_PREFIX_ParseError{err: err, pos: pos})
	}
}

// expect records that the parser expected the literal lit at pos.
func (e *_PREFIX_ErrorStack) expect(lit string, pos int) {
	e.add(_PREFIX_ParseError{expected: lit, literal: true, pos: pos})
}

func (e *_PREFIX_ErrorStack) add(pe _PREFIX_ParseError) {
	if base, n := e.scopeBase(), len(e.stack); n > base {
		if top := e.stack[n-1].rank(); pe.rank() < top {
			return
		} else if pe.rank() > top {
			e.stack = e.stack[:base]
		}
	}
	e.stack = append(e.stack, pe)
}

// label replaces the innermost scope's errors with an expectation of want,
// a production's label, at the same position, if they're expectations. A
// scope with no errors gets the expectation at start.
func (e *_PREFIX_ErrorStack) label(want string, start int) {
	base, n := e.scopeBase(), len(e.stack)
	pos := start
	if n > base {
		if e.stack[n-1].err != nil {
			return
		}
		pos = e.stack[n-1].pos
	}
	e.stack = append(e.stack[:base], _PREFIX_ParseError{expected: want, pos: pos})
}

// coalesce returns the innermost scope's errors as one error: nil if there
// are none, an _PREFIX_ExpectedError listing them if they're expectations, and
// otherwise the real errors, each message once, one per line.
func (e *_PREFIX_ErrorStack) coalesce() error {
	scope := e.stack[e.scopeBase():]
	if len(scope) == 0 {
		return nil
	}
	if scope[0].err == nil {
		// a scope's errors rank alike, so they're all expectations. The
		// error escapes into user code, so it can't be pooled, but it
		// carries its own small buffer, which keeps the usual short list
		// to one allocation.
		ee := &_PREFIX_ExpectedError{}
		ee.want = append(ee.buf[:0], scope...)
		return ee
	}

	es := e.es[:0]
COALESCE_OUTER:
	for _, v := range scope {
		// deduplicate errors by message. Comparing the error values first
		// measured slower, and it panics on two errors of the same
		// uncomparable type.
		for _, w := range es {
			if w.Error() == v.err.Error() {
				continue COALESCE_OUTER
			}
		}
		es = append(es, v.err)
	}
	e.es = es[:0]

	if len(es) == 1 {
		return es[0]
	}
	// defer building the joined message until something reads it; failure
	// handlers coalesce on every failed speculative attempt and almost
	// always discard the result, so rendering eagerly wastes both CPU and
	// allocations on strings that keep growing as failures cascade
	return &_PREFIX_JoinedError{es: append([]error(nil), es...)}
}

// _PREFIX_ExpectedError is what the parser expected at a position, as one message:
// "expected ';', '=' or an operator". Each thing expected is listed once,
// the literals first, quoted, and then the rest, each group sorted. Like
// _PREFIX_JoinedError's, the message is rendered when it's first read.
type _PREFIX_ExpectedError struct {
	want []_PREFIX_ParseError
	// buf is where want is built, so that the usual short list of things
	// expected costs no allocation beyond the error itself
	buf [4]_PREFIX_ParseError
	msg string
}

func (e *_PREFIX_ExpectedError) Error() string {
	if e.msg == "" {
		// an insertion sort, since the lists are short; ws[:lits] are the
		// literals
		var ws []string
		lits := 0
	WANT:
		for _, v := range e.want {
			w, lo, hi := v.expected, lits, len(ws)
			if v.literal {
				w, lo, hi = _PREFIX_QuoteLiteral(w), 0, lits
			}
			i := lo
			for ; i < hi && ws[i] <= w; i++ {
				if ws[i] == w {
					continue WANT
				}
			}
			ws = append(ws, "")
			copy(ws[i+1:], ws[i:])
			ws[i] = w
			if v.literal {
				lits++
			}
		}

		var sb strings.Builder
		sb.WriteString("expected ")
		for i, w := range ws {
			if i > 0 && i == len(ws)-1 {
				sb.WriteString(" or ")
			} else if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(w)
		}
		e.msg = sb.String()
	}
	return e.msg
}

// _PREFIX_QuoteLiteral quotes a literal for an error message, in single quotes
// unless it has one.
func _PREFIX_QuoteLiteral(s string) string {
	if strings.Contains(s, "'") {
		return fmt.Sprintf("%q", s)
	}
	return "'" + s + "'"
}

// _PREFIX_JoinedError is a multi-error whose message is rendered lazily and
// memoized.  It prints the error stack in reverse order.
type _PREFIX_JoinedError struct {
	es  []error
	msg string
}

func (j *_PREFIX_JoinedError) Error() string {
	if j.msg == "" {
		var sb strings.Builder
		for i := len(j.es) - 1; i >= 0; i-- {
			sb.WriteString(j.es[i].Error())
			sb.WriteByte('\n')
		}
		j.msg = strings.TrimSpace(sb.String())
	}
	return j.msg
}

func (e *_PREFIX_ErrorStack) depth() int {
	var ret int
	for _, v := range e.stack[e.scopeBase():] {
		if v.pos > ret {
			ret = v.pos
		}
	}
	return ret
}

// label is called when a production with a label fails. If the production
// failed before matching anything, that is, if its errors are at start,
// where it started, or past only whitespace in string mode, then what it
// expected is what its label names, and the label replaces the
// expectations it recorded. Errors further on are left as they are, since
// they say what was expected there, and so are real errors.
func (p *_PREFIX_Parser) label(want string, start int) {
	if d := p.errorStack.depth(); d > start && !p.blank(start, d) {
		return
	}
	p.errorStack.label(want, start)
}
`

var header = `

func Parse_PREFIX_(input string, data *_PREFIX_Data) %v {
	p := new_PREFIX_Parser(input, data)

	%v := p.state_ENTRYPOINT_()
	if err == nil {
		if rest := strings.TrimLeftFunc(p.input[p.pos:], unicode.IsSpace); rest != "" {
			// the top production matched but input is left over. The
			// parser may have recorded nothing it expected, in which case
			// coalesce is nil and the trailing input needs an error of its
			// own, or the parse would be reported as a success.
			err = p.errorStack.coalesce()
			if err == nil {
				err = fmt.Errorf("unexpected input at offset %%v", len(p.input)-len(rest))
			}
			%v
		}
	} else {
		err = p.errorStack.coalesce()
	}
	
	%v 
}

type _PREFIX_Parser struct {
	input       string
	pos         int
	lineOffsets []int
	Data        *_PREFIX_Data
	errorStack  *_PREFIX_ErrorStack

	posStack []int

	// Data, when it implements _PREFIX_Savepointer
	savepoints _PREFIX_Savepointer
}

func new_PREFIX_Parser(input string, data *_PREFIX_Data) *_PREFIX_Parser {
	p := &_PREFIX_Parser{
		input:       input,
		lineOffsets: _PREFIX_GenerateLineOffsets(input),
		Data: data,
		errorStack: &_PREFIX_ErrorStack{
			stack:  make([]_PREFIX_ParseError, 0, 64),
			scopes: make([]int, 0, 64),
		},
		posStack: make([]int, 0, 64),
	}
	if sp, ok := interface{}(data).(_PREFIX_Savepointer); ok && data != nil {
		p.savepoints = sp
	}
	return p
}

func _PREFIX_GenerateLineOffsets(input string) []int {
	var ret []int

	lines := strings.Split(input, "\n")

	offset := 0
	for _, v := range lines {
		ret = append(ret, len(v)+1+offset)
		offset += len(v) + 1
	}
	return ret
}

// literal runs on every token match attempt during speculative parsing, so
// it must not allocate; it records what it expected as data, and returns the
// same error every time.
func (p *_PREFIX_Parser) literal(want string) (string, error) {
	count := 0
	for r, s := utf8.DecodeRuneInString(p.input[p.pos+count:]); s > 0 && unicode.IsSpace(r); r, s = utf8.DecodeRuneInString(p.input[p.pos+count:]) {
		count += s
	}

	if strings.HasPrefix(p.input[p.pos+count:], want) {
		p.pos += count + len(want)
		return want, nil
	}

	p.errorStack.expect(want, p.pos)
	return "", _PREFIX_NoMatch
}

// blank reports whether the input from start to end is only whitespace,
// which literals skip.
func (p *_PREFIX_Parser) blank(start, end int) bool {
	return strings.TrimSpace(p.input[start:end]) == ""
}
`

var headerTokenMode = `
func Parse_PREFIX_(input []string, data *_PREFIX_Data) %v {
	p := new_PREFIX_Parser(input, data)

	%v := p.state_ENTRYPOINT_()
	if err == nil {
		if p.pos < len(p.input) {
			// the top production matched but tokens are left over. The
			// parser may have recorded nothing it expected, in which case
			// coalesce is nil and the trailing tokens need an error of
			// their own, or the parse would be reported as a success.
			err = p.errorStack.coalesce()
			if err == nil {
				err = fmt.Errorf("unexpected input at token %%v", p.pos)
			}
			%v
		}
	} else {
		err = p.errorStack.coalesce()
	}
	
	%v 
}

type _PREFIX_Parser struct {
	input       []string
	pos         int
	Data        *_PREFIX_Data
	errorStack  *_PREFIX_ErrorStack

	posStack []int

	// Data, when it implements _PREFIX_Savepointer
	savepoints _PREFIX_Savepointer
}

func new_PREFIX_Parser(input []string, data *_PREFIX_Data) *_PREFIX_Parser {
	p := &_PREFIX_Parser{
		input:       input,
		Data: data,
		errorStack: &_PREFIX_ErrorStack{
			stack:  make([]_PREFIX_ParseError, 0, 64),
			scopes: make([]int, 0, 64),
		},
		posStack: make([]int, 0, 64),
	}
	if sp, ok := interface{}(data).(_PREFIX_Savepointer); ok && data != nil {
		p.savepoints = sp
	}
	return p
}

// literal runs on every token match attempt during speculative parsing, so
// it must not allocate; it records what it expected as data, and returns the
// same error every time.
func (p *_PREFIX_Parser) literal(want string) (string, error) {
	if p.pos < len(p.input) && p.input[p.pos] == want {
		p.pos++
		return want, nil
	}

	p.errorStack.expect(want, p.pos)
	return "", _PREFIX_NoMatch
}

// blank reports whether the tokens from start to end are only whitespace:
// in token mode, whether there are none.
func (p *_PREFIX_Parser) blank(start, end int) bool {
	return end <= start
}
`

// backtracking is the code that undoes a failed speculative attempt, the same
// in string and token mode.
var backtracking = `

// _PREFIX_Savepointer is implemented by a _PREFIX_Data whose actions, lexer
// functions, or error functions change state that backtracking must undo.
// Backtracking restores the parser's position, but an action runs as soon as
// its production matches, even in an attempt that fails afterwards, and a
// lexer function runs on every attempt. When _PREFIX_Data implements it, the
// parser calls savepoint as each speculative attempt begins, then rollback if
// the attempt fails or release if it succeeds. The calls nest as the attempts
// do: rollback restores the state saved by the latest savepoint not yet rolled
// back or released, and release forgets that savepoint, keeping the changes
// made since.
type _PREFIX_Savepointer interface {
	savepoint()
	rollback()
	release()
}

// predict/backtrack/accept save and restore the parser's state around a
// speculative parse attempt: its position, and Data's state when Data is a
// _PREFIX_Savepointer. input, lineOffsets, and errorStack are shared, so a
// position stack on the receiver replaces the previous scheme of allocating a
// full parser copy per attempt.
func (p *_PREFIX_Parser) predict() *_PREFIX_Parser {
	p.posStack = append(p.posStack, p.pos)
	if p.savepoints != nil {
		p.savepoints.savepoint()
	}
	return p
}

func (p *_PREFIX_Parser) backtrack() *_PREFIX_Parser {
	p.pos = p.posStack[len(p.posStack)-1]
	p.posStack = p.posStack[:len(p.posStack)-1]
	if p.savepoints != nil {
		p.savepoints.rollback()
	}
	return p
}

func (p *_PREFIX_Parser) accept() *_PREFIX_Parser {
	p.posStack = p.posStack[:len(p.posStack)-1]
	if p.savepoints != nil {
		p.savepoints.release()
	}
	return p
}
`

var doNotModify = `// generated by pbpg, do not modify
`

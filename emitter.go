/*************************************************************************
 * Copyright 2022 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/
package main

import (
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
	typeMap  map[string]string
	stateMap map[string]*Expression // list of productions
	out      strings.Builder        // output buffer, which is combined with the boiler plate code and formatted on success

	statesUsed map[string]bool // list of states referenced in terms. Used for grammar validation.

	entryPoint string // The name of the first encountered production.
}

type Variable struct {
	Value      string
	T          int
	Repetition bool
}

// verify does the following:
// 	1. Ensures all productions used are defined.
// 	2. All productions defined are used when starting from the entrypoint.
func (p *pbpgData) verify() error {
	// 1
	for k, _ := range p.statesUsed {
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
	for k, _ := range p.stateMap {
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
				for k, _ := range x {
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

func (p *pbpgData) declarators(exp *Expression) string {
	vars := exp.variables()
	c := 1
	var r string

	ag := exp.numAlternativeGroups()
	for i := 1; i <= ag; i++ {
		r += fmt.Sprintf("var a%vPos int\n", i)
	}

	for _, v := range vars {
		switch v.T {
		case TERM_NAME:
			if ftype, ok := p.typeMap[v.Value]; ok {
				if v.Repetition {
					r += fmt.Sprintf("var v%vtemp %v\n", c, ftype)
					r += fmt.Sprintf("var v%v []%v\n", c, ftype)
				} else {
					r += fmt.Sprintf("var v%v %v\n", c, ftype)
				}
				c++
			}
		case TERM_LITERAL, TERM_LEX:
			if v.Repetition {
				r += fmt.Sprintf("var v%vtemp string\n", c)
				r += fmt.Sprintf("var v%v []string\n", c)
			} else {
				r += fmt.Sprintf("var v%v string\n", c)
			}
			c++
		}
	}
	return r
}

func (p *pbpgData) positionalArgs(exp *Expression) string {
	vars := exp.variables()
	c := 1
	var r string

	ag := exp.numAlternativeGroups()
	for i := 1; i <= ag; i++ {
		r += fmt.Sprintf("a%vPos,", i)
	}

	for _, v := range vars {
		switch v.T {
		case TERM_NAME:
			if _, ok := p.typeMap[v.Value]; ok {
				r += fmt.Sprintf("v%v,", c)
				c++
			}
		case TERM_LITERAL, TERM_LEX:
			r += fmt.Sprintf("v%v,", c)
			c++
		}
	}
	return strings.TrimSuffix(r, ",")
}

func (p *pbpgData) functionSignature(exp *Expression) string {
	vars := exp.variables()
	c := 1
	var r string

	ag := exp.numAlternativeGroups()
	for i := 1; i <= ag; i++ {
		r += fmt.Sprintf("a%vPos int,", i)
	}

	for _, v := range vars {
		switch v.T {
		case TERM_NAME:
			if ftype, ok := p.typeMap[v.Value]; ok {
				if v.Repetition {
					r += fmt.Sprintf("v%v []%v,", c, ftype)
				} else {
					r += fmt.Sprintf("v%v %v,", c, ftype)
				}
				c++
			}
		case TERM_LITERAL, TERM_LEX:
			if v.Repetition {
				r += fmt.Sprintf("v%v []string,", c)
			} else {
				r += fmt.Sprintf("v%v string,", c)
			}
			c++
		}
	}
	return strings.TrimSuffix(r, ",")
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

// emitState is called in the action of the Production production. It writes a
// state function and walks the given expression (via the visit* functions) to
// write the logic for the production.
func (p *pbpgData) emitState(name string, exp *Expression, a, e string) {
	hasActionError := a != "" || e != ""
	// make the comment of the current production
	p.out.WriteString(fmt.Sprintf("// %v = %v\n", name, exp.String()))

	ftype, hasType := p.typeMap[name]
	var retType string
	if hasType {
		retType = ftype + ", "
	}
	p.out.WriteString(fmt.Sprintf("func (p *%vParser) state%v() (%v error) {\nvar err error\n", *fPrefix, name, retType))

	if hasType {
		p.out.WriteString(fmt.Sprintf("var ret %v\n", ftype))
	}
	if p.declarators(exp) != "" && hasActionError {
		p.out.WriteString(p.declarators(exp))
	}

	if *fDebug {
		p.out.WriteString(fmt.Sprintf("log.Println(\"state%v\")\n", name))
	}

	p.visitExpression(1, 0, exp, false, hasActionError)

	pa := p.positionalArgs(exp)
	if a != "" {
		if hasType {
			p.out.WriteString(fmt.Sprintf("if err == nil { ret = p.Data.action%v(%v) }\n\n", name, pa))
		} else {
			p.out.WriteString(fmt.Sprintf("if err == nil { p.Data.action%v(%v) }\n\n", name, pa))
		}
	}
	if e != "" {
		var args string
		if pa != "" {
			args = ", " + pa
		}
		p.out.WriteString(fmt.Sprintf("if err != nil { err = p.Data.error%v(err $v) }\n\n", name, args))
	}

	if hasType {
		p.out.WriteString("return ret, err\n}\n\n")
	} else {
		p.out.WriteString("return err\n}\n\n")
	}

	fs := p.functionSignature(exp)
	if a != "" {
		if hasType {
			p.out.WriteString(fmt.Sprintf("func (p *%vData) action%v(%v) %v { %v\n}\n\n", *fPrefix, name, fs, ftype, a))
		} else {
			p.out.WriteString(fmt.Sprintf("func (p *%vData) action%v(%v) { %v\n}\n\n", *fPrefix, name, fs, a))
		}
	}
	if e != "" {
		var args string
		if fs != "" {
			args = ", " + fs
		}
		p.out.WriteString(fmt.Sprintf("func (p *%vData) error%v(err error %v) error {\n%v\n}\n\n", *fPrefix, name, args, e))
	}
}

func (p *pbpgData) visitExpression(vCount int, aCount int, exp *Expression, rep bool, hasAction bool) int {
	var needPos bool
	if len(exp.alternatives) > 1 && hasAction {
		aCount++
		needPos = true
	}

	for i, v := range exp.alternatives {
		if needPos {
			p.out.WriteString(fmt.Sprintf("a%vPos = %v\n", aCount, i))
		}
		vCount = p.visitAlternative(vCount, aCount, v, rep, hasAction)
		if i < len(exp.alternatives)-1 {
			p.out.WriteString(fmt.Sprintf("if err != nil { \n"))
		}
	}
	for i, _ := range exp.alternatives {
		if i < len(exp.alternatives)-1 {
			p.out.WriteString("}\n")
		}
	}
	return vCount
}

func (p *pbpgData) visitAlternative(vCount int, aCount int, alt *Alternative, rep bool, hasAction bool) int {
	for i, v := range alt.terms {
		vCount = p.visitTerm(vCount, aCount, v, rep, hasAction)
		if i < len(alt.terms)-1 {
			p.out.WriteString("if err == nil {\n")
		}
	}
	for i, _ := range alt.terms {
		if i < len(alt.terms)-1 {
			p.out.WriteString("}\n")
		}
	}
	return vCount
}

func (p *pbpgData) visitTerm(vCount int, aCount int, term *Term, rep bool, hasAction bool) int {
	switch term.option {
	case TERM_NAME:
		if _, ok := p.typeMap[term.name]; ok {
			if hasAction {
				if rep {
					p.out.WriteString(fmt.Sprintf("v%vtemp, err = p.state%v()\n", vCount, term.name))
				} else {
					p.out.WriteString(fmt.Sprintf("v%v, err = p.state%v()\n", vCount, term.name))
				}
			} else {
				p.out.WriteString(fmt.Sprintf("_, err = p.state%v()\n", vCount, term.name))
			}
			vCount++
		} else {
			p.out.WriteString(fmt.Sprintf("err = p.state%v()\n", term.name))
		}
		if p.statesUsed == nil {
			p.statesUsed = make(map[string]bool)
		}
		p.statesUsed[term.name] = true
	case TERM_LITERAL:
		if hasAction {
			if rep {
				p.out.WriteString(fmt.Sprintf("v%vtemp, err = p.literal(%v)\n", vCount, strconv.Quote(term.literal)))
			} else {
				p.out.WriteString(fmt.Sprintf("v%v, err = p.literal(%v)\n", vCount, strconv.Quote(term.literal)))
			}
		} else {
			p.out.WriteString(fmt.Sprintf("_, err = p.literal(%v)\n", vCount, strconv.Quote(term.literal)))
		}
		vCount++
	case TERM_GOR:
		vCount = p.visitGOR(vCount, aCount, term.gor, rep, hasAction)
	case TERM_LEX:
		if hasAction {
			if rep {
				p.out.WriteString(fmt.Sprintf("{ n, lexeme, lerr := p.Data.lex%v(p.input[p.pos:]); p.pos += n; if lerr != nil { err = fmt.Errorf(\"%%v: %%w\", p.position(), lerr) } else { err = nil; v%vtemp = lexeme }; };", term.lex, vCount))
			} else {
				p.out.WriteString(fmt.Sprintf("{ n, lexeme, lerr := p.Data.lex%v(p.input[p.pos:]); p.pos += n; if lerr != nil { err = fmt.Errorf(\"%%v: %%w\", p.position(), lerr) } else { err = nil; v%v = lexeme }; };", term.lex, vCount))
			}
		} else {
			p.out.WriteString(fmt.Sprintf("{ n, _, lerr := p.Data.lex%v(p.input[p.pos:]); p.pos += n; if lerr != nil { err = fmt.Errorf(\"%%v: %%w\", p.position(), lerr) } else { err = nil; }; };", term.lex, vCount))
		}
		vCount++
	}
	return vCount
}

// Subexpressions, which are visited in groups, must create a new parser, which
// then attempts to evaluate the expression. If it fails, the parent parser can
// discard the result (backtracking), or accept it by merging the parser states
// together.
func (p *pbpgData) visitGOR(vCount int, aCount int, gor *GOR, rep bool, hasAction bool) int {
	switch gor.option {
	case GOR_GROUP:
		p.out.WriteString("// group\n")
		p.out.WriteString("p = p.predict()\n")
		vCount = p.visitExpression(vCount, aCount, gor.expression, rep, hasAction)
		p.out.WriteString(fmt.Sprintf("if err != nil { p = p.backtrack() } else { p = p.accept() }\n"))
	case GOR_OPTION:
		p.out.WriteString("// option\n")
		p.out.WriteString("p = p.predict()\n")
		vCount = p.visitExpression(vCount, aCount, gor.expression, rep, hasAction)
		p.out.WriteString(fmt.Sprintf("if err != nil { p = p.backtrack(); p.lastErr = err; err = nil } else { p = p.accept() }\n"))
	case GOR_REPETITION:
		p.out.WriteString("// repetition\n")
		p.out.WriteString("for {\n")
		p.out.WriteString("p = p.predict()\n")
		vStart := vCount
		vCount = p.visitExpression(vCount, aCount, gor.expression, true, hasAction)
		var acceptAppends string
		if hasAction {
			for i := vStart; i < vCount; i++ {
				acceptAppends += fmt.Sprintf("v%v = append(v%v, v%vtemp)\n", i, i, i)
			}
		}
		p.out.WriteString(fmt.Sprintf("if err != nil { p = p.backtrack(); p.lastErr = err; err = nil; break } else { %v p = p.accept() }\n", acceptAppends))
		p.out.WriteString("}\n")
	}
	return vCount
}

var header = `
func Parse_PREFIX_(input string, data *_PREFIX_Data) error {
	p := new_PREFIX_Parser(input, data)

	err := p.state_ENTRYPOINT_()
	if err == nil {
		if strings.TrimSpace(p.input[p.pos:]) != "" {
			return p.lastErr
		}
	}
	return err
}

type _PREFIX_Parser struct {
	input       string
	pos         int
	lineOffsets []int
	Data        *_PREFIX_Data
	lastErr error

	predictStack []*_PREFIX_Parser
}

func new_PREFIX_Parser(input string, data *_PREFIX_Data) *_PREFIX_Parser {
	return &_PREFIX_Parser{
		input:       input,
		lineOffsets: _PREFIX_GenerateLineOffsets(input),
		Data: data,
	}
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

func (p *_PREFIX_Parser) position() string {
	for i, v := range p.lineOffsets {
		if p.pos < v {
			return fmt.Sprintf("line %v", i)
		}
	}
	return fmt.Sprintln("impossible line reached", p.pos)
}

func (p *_PREFIX_Parser) literal(want string) (string, error) {
	count := 0
	for r, s := utf8.DecodeRuneInString(p.input[p.pos+count:]); s > 0 && unicode.IsSpace(r); r, s = utf8.DecodeRuneInString(p.input[p.pos+count:]) {
		count += s
	}

	if strings.HasPrefix(p.input[p.pos+count:], want) {
		p.pos += count + len(want)
		return want, nil
	}

	return "", fmt.Errorf("%v: expected \"%v\"", p.position(), want)
}

func (p *_PREFIX_Parser) predict() *_PREFIX_Parser {
	p.predictStack = append(p.predictStack, p)
	return &_PREFIX_Parser{
		input: p.input,
		pos: p.pos,
		lineOffsets: p.lineOffsets,
		predictStack: p.predictStack,
		lastErr: p.lastErr,
		Data: p.Data,
	}
}

func (p *_PREFIX_Parser) backtrack() *_PREFIX_Parser {
	pp := p.predictStack[len(p.predictStack)-1]
	pp.predictStack = pp.predictStack[:len(pp.predictStack)-1]
	pp.lastErr = p.lastErr
	return pp
}

func (p *_PREFIX_Parser) accept() *_PREFIX_Parser {
	pp := p.backtrack()
	pp.pos = p.pos
	return pp
}
`

var doNotModify = `// generated by pbpg, do not modify
`

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
	"log"
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
	stateMap map[string]*Expression // list of productions
	out      strings.Builder        // output buffer, which is combined with the boiler plate code and formatted on success

	statesUsed map[string]bool // list of states referenced in terms. Used for grammar validation.

	entryPoint string // The name of the first encountered production.

	// Temporaries. Most of these are arranged as a stack and popped off in actions.
	sCode    []string
	sName    []string
	sAction  string
	sError   string
	sLiteral []string
	sString  []string
	lastTerm []int
	gors     []*GOR

	// The current grammar being produced.
	expression   *Expression
	alternatives []*Alternative
	terms        []*Term
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

func (p *pbpgData) fork() *pbpgData {
	return &pbpgData{}
}

// Merge b->a. Essentially all paramters that are stacks get appended into a,
// and all non-nil (or non empty) values are copied over.
func (a *pbpgData) merge(b *pbpgData) {
	a.out.WriteString(b.out.String())

	if a.stateMap == nil {
		a.stateMap = make(map[string]*Expression)
	}
	for k, v := range b.stateMap {
		if a.stateMap[k] != nil {
			log.Fatalf("%v redeclared", k)
		}
		a.stateMap[k] = v
	}

	if a.statesUsed == nil {
		a.statesUsed = make(map[string]bool)
	}
	for k, _ := range b.statesUsed {
		a.statesUsed[k] = true
	}

	if a.entryPoint == "" {
		a.entryPoint = b.entryPoint
	}

	a.sCode = append(a.sCode, b.sCode...)
	a.sName = append(a.sName, b.sName...)
	if b.sAction != "" {
		a.sAction = b.sAction
	}
	if b.sError != "" {
		a.sError = b.sError
	}
	a.sLiteral = append(a.sLiteral, b.sLiteral...)
	a.sString = append(a.sString, b.sString...)
	a.lastTerm = append(a.lastTerm, b.lastTerm...)
	a.gors = append(a.gors, b.gors...)

	if b.expression != nil {
		a.expression = b.expression
	}

	if b.alternatives != nil {
		a.alternatives = append(a.alternatives, b.alternatives...)
	}

	if b.terms != nil {
		a.terms = append(a.terms, b.terms...)
	}

}

func (p *pbpgData) pushGOR(g *GOR) {
	p.gors = append(p.gors, g)
}

func (p *pbpgData) popGOR() *GOR {
	r := p.gors[len(p.gors)-1]
	p.gors = p.gors[:len(p.gors)-1]
	return r
}

func (p *pbpgData) pushName(n string) {
	p.sName = append(p.sName, n)
}

func (p *pbpgData) popName() string {
	r := p.sName[len(p.sName)-1]
	p.sName = p.sName[:len(p.sName)-1]
	return r
}

func (p *pbpgData) pushCode(n string) {
	p.sCode = append(p.sCode, n)
}

func (p *pbpgData) popCode() string {
	r := p.sCode[len(p.sCode)-1]
	p.sCode = p.sCode[:len(p.sCode)-1]
	return r
}

func (p *pbpgData) pushLiteral(n string) {
	p.sLiteral = append(p.sLiteral, n)
}

func (p *pbpgData) popLiteral() string {
	r := p.sLiteral[len(p.sLiteral)-1]
	p.sLiteral = p.sLiteral[:len(p.sLiteral)-1]
	return r
}

func (p *pbpgData) pushString(n string) {
	p.sString = append(p.sString, n)
}

func (p *pbpgData) popString() string {
	r := p.sString[len(p.sString)-1]
	p.sString = p.sString[:len(p.sString)-1]
	return r
}

func (p *pbpgData) pushLastTerm(n int) {
	p.lastTerm = append(p.lastTerm, n)
}

func (p *pbpgData) popLastTerm() int {
	r := p.lastTerm[len(p.lastTerm)-1]
	p.lastTerm = p.lastTerm[:len(p.lastTerm)-1]
	return r
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
	// make the comment of the current production
	p.out.WriteString(fmt.Sprintf("// %v = %v\n", name, exp.String()))

	p.out.WriteString(fmt.Sprintf("func (p *%vParser) state%v() (err error) {\n", *fPrefix, name))

	p.visitExpression(exp)

	if a != "" {
		p.out.WriteString(fmt.Sprintf("if err == nil { p.Data.action%v(p.lastLiteral, p.lexeme) }\n\n", name))
	}
	if e != "" {
		p.out.WriteString(fmt.Sprintf("if err != nil { err = p.Data.error%v(err, p.lastLiteral, p.lexeme, p.pos, p.position()) }\n\n", name))
	}

	p.out.WriteString("return err\n}\n\n")

	if a != "" {
		p.out.WriteString(fmt.Sprintf("func (p *%vData) action%v(lit, lex string) { %v\n}\n\n", *fPrefix, name, a))
	}
	if e != "" {
		p.out.WriteString(fmt.Sprintf("func (p *%vData) error%v(err error, lit, lex string, pos int, position string) error {\n%v\n}\n\n", *fPrefix, name, e))
	}
}

func (p *pbpgData) visitExpression(exp *Expression) {
	for i, v := range exp.alternatives {
		p.visitAlternative(v)
		if i < len(exp.alternatives)-1 {
			p.out.WriteString(fmt.Sprintf("if err != nil { \n"))
		}
	}
	for i, _ := range exp.alternatives {
		if i < len(exp.alternatives)-1 {
			p.out.WriteString("}\n")
		}
	}
}

func (p *pbpgData) visitAlternative(alt *Alternative) {
	for i, v := range alt.terms {
		p.visitTerm(v)
		if i < len(alt.terms)-1 {
			p.out.WriteString("if err == nil {\n")
		}
	}
	for i, _ := range alt.terms {
		if i < len(alt.terms)-1 {
			p.out.WriteString("}\n")
		}
	}
}

func (p *pbpgData) visitTerm(term *Term) {
	switch term.option {
	case TERM_NAME:
		p.out.WriteString(fmt.Sprintf("err = p.state%v()\n", term.name))
		if p.statesUsed == nil {
			p.statesUsed = make(map[string]bool)
		}
		p.statesUsed[term.name] = true
	case TERM_LITERAL:
		p.out.WriteString(fmt.Sprintf("err = p.literal(%v)\n", strconv.Quote(term.literal)))
	case TERM_GOR:
		p.visitGOR(term.gor)
	case TERM_LEX:
		p.out.WriteString(fmt.Sprintf("{ n, lexeme, lerr := p.Data.lex%v(p.input[p.pos:]); p.pos += n; if lerr != nil { err = fmt.Errorf(\"%%v: %%w\", p.position(), lerr) } else { err = nil; p.lexeme = lexeme }; };", term.lex))
	}
}

// Subexpressions, which are visited in groups, must create a new parser, which
// then attempts to evaluate the expression. If it fails, the parent parser can
// discard the result (backtracking), or accept it by merging the parser states
// together.
func (p *pbpgData) visitGOR(gor *GOR) {
	switch gor.option {
	case GOR_GROUP:
		p.out.WriteString("// group\n")
		p.out.WriteString("p = p.predict()\n")
		p.visitExpression(gor.expression)
		p.out.WriteString(fmt.Sprintf("if err != nil { p = p.backtrack() } else { p = p.accept() }\n"))
	case GOR_OPTION:
		p.out.WriteString("// option\n")
		p.out.WriteString("p = p.predict()\n")
		p.visitExpression(gor.expression)
		p.out.WriteString(fmt.Sprintf("if err != nil { p = p.backtrack(); p.lastErr = err; err = nil } else { p = p.accept() }\n"))
	case GOR_REPETITION:
		p.out.WriteString("// repetition\n")
		p.out.WriteString("for {\n")
		p.out.WriteString("p = p.predict()\n")
		p.visitExpression(gor.expression)
		p.out.WriteString(fmt.Sprintf("if err != nil { p = p.backtrack(); p.lastErr = err; err = nil; break } else { p = p.accept() }\n"))
		p.out.WriteString("}\n")
	}
}

var header = `
func Parse_PREFIX_(input string) (*_PREFIX_Parser, error) {
	p := new_PREFIX_Parser(input)

	err := p.state_ENTRYPOINT_()
	if err == nil {
		if strings.TrimSpace(p.input[p.pos:]) != "" {
			return p, p.lastErr
		}
	}
	return p, err
}

type _PREFIX_Parser struct {
	input       string
	pos         int
	lineOffsets []int
	lexeme      string
	Data        *_PREFIX_Data
	lastErr error
	lastLiteral string

	predictStack []*_PREFIX_Parser
}

func new_PREFIX_Parser(input string) *_PREFIX_Parser {
	return &_PREFIX_Parser{
		input:       input,
		lineOffsets: _PREFIX_GenerateLineOffsets(input),
		Data: &_PREFIX_Data{},
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

func (p *_PREFIX_Parser) literal(want string) error {
	count := 0
	for r, s := utf8.DecodeRuneInString(p.input[p.pos+count:]); s > 0 && unicode.IsSpace(r); r, s = utf8.DecodeRuneInString(p.input[p.pos+count:]) {
		count += s
	}

	if strings.HasPrefix(p.input[p.pos+count:], want) {
		p.pos += count + len(want)
		p.lastLiteral = want
		return nil
	}

	return fmt.Errorf("%v: expected \"%v\"", p.position(), want)
}

func (p *_PREFIX_Parser) predict() *_PREFIX_Parser {
	p.predictStack = append(p.predictStack, p)
	return &_PREFIX_Parser{
		input: p.input,
		pos: p.pos,
		lineOffsets: p.lineOffsets,
		lexeme: p.lexeme, 
		Data: p.Data.fork(),
		predictStack: p.predictStack,
		lastErr: p.lastErr,
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
	pp.lexeme = p.lexeme
	pp.Data.merge(p.Data)
	return pp
}
`

var stubHeader = `
type _PREFIX_Data struct {}

func (p *_PREFIX_Data) fork() *_PREFIX_Data {
	return &_PREFIX_Data{}
}

func (a *_PREFIX_Data) merge(b *_PREFIX_Data) {}

`

var doNotModify = `// generated by pbpg, do not modify
`

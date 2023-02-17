{
package main

import (
	"fmt"
	"log"
	"strings"
	"unicode"
	"unicode/utf8"
)

}

type Code string
type Literal string
type Lex string
type Repetition *GOR
type Option *GOR
type Group *GOR
type Term *Term
type Alternative *Alternative
type Expression *Expression
type CodeBlock string
type Error string
type Action string
type Name string
type QuotedString string

# The top level production is the initial state to attempt to reduce.

Program     = { Comment } [ Header ] { Types } Line { Line } .
Header      = CodeBlock .							Action { p.out.WriteString(doNotModify); p.out.WriteString(v1) }
Types       = "type" Name lex(type) .						Action {
											if _, ok := p.typeMap[v2]; ok {
												log.Fatalf("type %v redeclared", v2)
											}
											p.typeMap[v2] = v3
										}
Line        = Comment | Production .
Production  = Name "=" [ Expression ] "." [ Action ] [ Error ] .		Action { 
											if p.stateMap[v1] != nil {
												log.Fatalf("%v redeclared", v1)
											}
											p.stateMap[v1] = v3
											p.orderedStates = append(p.orderedStates, v1)
											
											p.emitState(v1, v3, v5, v6)

											if p.entryPoint == "" {
												p.entryPoint = v1
											}
										}
Action      = "Action" CodeBlock .						Action { return v2; }
Error       = "Error" CodeBlock .						Action { return v2; }
CodeBlock   = "{" Code "}" .							Action { return v2; }
Expression  = Alternative { "|" Alternative } .					Action { return &Expression{ alternatives: append([]*Alternative{v1}, v3...)}; }
Alternative = Term { Term } .							Action { return &Alternative{terms: append([]*Term{v1}, v2...)}; }
Term        = Lex | Name | Literal | Group | Option | Repetition .		Action { 
											t := &Term{}
											switch a1Pos {
												case 1:
													t.lex = v1	
													t.option = TERM_LEX
												case 2:
													t.name = v2
													t.option = TERM_NAME
												case 3:
													t.literal = v3
													t.option = TERM_LITERAL
												case 4:
													t.gor = v4
													t.option = TERM_GOR
												case 5:
													t.gor = v5
													t.option = TERM_GOR
												case 6: 
													t.gor = v6
													t.option = TERM_GOR
											}
											return t
										}
Group       = "(" Expression ")" .						Action { return &GOR{ option: GOR_GROUP, expression: v2}; }
Option      = "[" Expression "]" .						Action { return &GOR{ option: GOR_OPTION, expression: v2}; }
Repetition  = "{" Expression "}" .						Action { return &GOR{ option: GOR_REPETITION, expression: v2}; }
Lex         = "lex" "(" lex(functionname) ")" .					Action { return v3; }
Literal     = "\"" QuotedString "\"" .						Action { return v2; }
Name	    = lex(name) .							Action { return v1; }

# Lexer directives. 

Code        	= lex(code) .							Action { return v1; }
QuotedString    = lex(quotedstring) .						Action { return v1; }
Comment     	= "#" lex(comment) .						Action { p.out.WriteString("// " + v2 + "\n") }

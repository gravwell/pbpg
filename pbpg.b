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

# The top level production is the initial state to attempt to reduce.

Program     = [ Comment { Comment } ] [ Header ] Line { Line } .
Header      = "{" Code "}" .						Action { p.out.WriteString(doNotModify); p.out.WriteString(p.popCode()) }
Line        = Comment | Production .
Production  = Name "=" [ Expression ] "." [ Action ] [ Error ] .	Action { 
										if p.stateMap == nil {
											p.stateMap = make(map[string]*Expression)
										}
										name := p.popName()
										if p.stateMap[name] != nil {
											log.Fatalf("%v redeclared", name)
										}
										p.stateMap[name] = p.expression

										p.emitState(name, p.expression, p.sAction, p.sError)
										p.expression = nil
										p.sAction = ""
										p.sError = ""

										if p.entryPoint == "" {
											p.entryPoint = name
										}
									}
Action      = "Action" CodeBlock .					Action { p.sAction = p.popCode() }
Error       = "Error" CodeBlock .					Action { p.sError = p.popCode() }
CodeBlock   = "{" Code "}" .						
Expression  = Alternative { "|" Alternative } .				Action { 
										p.expression = &Expression{
												alternatives: p.alternatives,
											}
										p.alternatives = nil
									}
Alternative = Term { Term } .						Action { 
										p.alternatives = append(p.alternatives, &Alternative{terms: p.terms})
										p.terms = nil
									}
Term        = Lex | Name | Literal | Group | Option | Repetition .	Action {
										t := &Term{option: p.popLastTerm()}
										switch t.option {
										case TERM_NAME:
											t.name = p.popName()
										case TERM_LEX:
											t.lex = p.popName()
										case TERM_GOR:
											t.gor = p.popGOR()
										case TERM_LITERAL:
											t.literal = p.popLiteral()
										}
										p.terms = append(p.terms, t)
									}
Group       = "(" Expression ")" .					Action {
										p.pushLastTerm(TERM_GOR)
										p.pushGOR(&GOR{option: GOR_GROUP, expression: p.expression})
										p.expression = nil
									}
Option      = "[" Expression "]" .					Action {
										p.pushLastTerm(TERM_GOR)
										p.pushGOR(&GOR{option: GOR_OPTION, expression: p.expression})
										p.expression = nil
									}
Repetition  = "{" Expression "}" .					Action {
										p.pushLastTerm(TERM_GOR)
										p.pushGOR(&GOR{option: GOR_REPETITION, expression: p.expression})
										p.expression = nil
									}
Lex         = "lex" "(" LexFunction ")" .				Action {
										p.pushLastTerm(TERM_LEX)
									}
Literal     = "\"" String "\"" .					Action { p.pushLiteral(p.popString()) }

# Lexer directives. 

Code        = lex(code) .						Action { p.pushCode(lex) }
Comment     = "#" lex(comment) .					Action { p.out.WriteString("// " + lex + "\n") }
Name        = lex(name) .						Action { p.pushLastTerm(TERM_NAME); p.pushName(lex) }
LexFunction = lex(functionname) .					Action { p.pushLastTerm(TERM_NAME); p.pushName(lex) }				
String      = lex(string) .						Action { p.pushLastTerm(TERM_LITERAL); p.pushString(lex) }

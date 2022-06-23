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

#type String string
#type Code string
#type Literal string
#type Lex string
#type Repetition *GOR
#type Option *GOR
#type Group *GOR
#type Term *Term
#type Alternative *Alternative
#type Expression *Expression
#type CodeBlock string
#type Error string
#type Action string

# The top level production is the initial state to attempt to reduce.

Program     = [ Comment { Comment } ] [ Header ] { Types } Line { Line } .
Header      = CodeBlock .							Action { p.out.WriteString(doNotModify); p.out.WriteString(v1) }
Types       = "type" String String .						Action {
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
											
											a, err := p.patchTypes(v3, v5)
											if err != nil {
												log.Fatal(err)
											}
											e, err := p.patchTypes(v3, v6)
											if err != nil {
												log.Fatal(err)
											}

											p.emitState(v1, v3, a, e)

											if p.entryPoint == "" {
												p.entryPoint = v1
											}
										}
Action      = "Action" CodeBlock .						Action { return v2; }
Error       = "Error" CodeBlock .						Action { return v2; }
CodeBlock   = "{" Code "}" .							Action { return v2; }
Expression  = Alternative { "|" Alternative } .					Action { return &Expression{ alternatives: append([]*Alternative, v1, v3...)}; }
Alternative = Term { Term } .							Action { return &Alternative{terms: append([]*Term, v1, v2...)}; }
Term        = Lex | Name | Literal | Group | Option | Repetition .		Action { 
											t := &Term{
												option: vP1,
											}
											switch t.option {
												case 1:
													t.Lex = v1	
												case 2:
													t.Name = v1
												case 3:
													t.Literal = v1
												case 4, 5, 6:
													t.GOR = v1
											}
											return t
										}
Group       = "(" Expression ")" .						Action { return &GOR{ option: TYPE_GROUP, expression: v2}; }
Option      = "[" Expression "]" .						Action { return &GOR{ option: TYPE_OPTION, expression: v2}; }
Repetition  = "{" Expression "}" .						Action { return &GOR{ option: TYPE_REPETITION, expression: v2}; }
Lex         = "lex" "(" String ")" .						Action { return v3; }
Literal     = "\"" String "\"" .						Action { return v2; }
Name	    = String .								Action { return v1; }

# Lexer directives. 

Code        = lex(code) .							Action { return v1; }
String      = lex(string) .							Action { return v1; }
Comment     = "#" lex(comment) .						Action { p.out.WriteString("// " + v2) }

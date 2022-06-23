# Introduction

Practical BNF Parser Generator (pbpg) is a parser generator that aims to keep many of the best parts of other parser generators and syntaxes such as yacc, EBNF, and PEG, while both relaxing certain strict requirements and promoting concepts such as errors to part of the specification.

# Syntax and Constraints

pbpg input is specified in an EBNF-like syntax. pbpg is also itself specified using pbpg. The syntax (pbnf) for pbpg is as follows:

```
# This is a comment

Program     = [ Comment { Comment } ] [ Header ] Line { Line } .
Header      = "{" Code "}" .
Line        = Comment | Production .
Production  = Name "=" [ Expression ] "." [ Action ] [ Error ] .
Action      = "Action" CodeBlock .
Error       = "Error" CodeBlock .
CodeBlock   = "{" Code "}" .						
Expression  = Alternative { "|" Alternative } .	
Alternative = Term { Term } .		
Term        = Lex | Name | Literal | Group | Option | Repetition .
Group       = "(" Expression ")" .		
Option      = "[" Expression "]" .	
Repetition  = "{" Expression "}" .
Lex         = "lex" "(" LexFunction ")" .
Literal     = "\"" String "\"" .	

# lexer rules

Code        = lex(code) .			
Comment     = "#" lex(comment) .
Name        = lex(name) .		
LexFunction = lex(functionname) .
String      = lex(string) .			
```

pbpg creates unambiguous input by using left-to-right precedence, similar to Parsing Expression Grammar (PEG). If multiple paths in the parse tree could be satisfied, the left-most rule is used. For example:

```
Pet = "Caterpillar" | "Cat" .
```

Given an input "Caterpillar's make terrible pets.", pbpg will match on the first substring in the list of given alternatives. If this were specified as `"Cat" | "Caterpillar"`, the parser would use "Cat", and the user would likely not get the intended result. This is also the fundamental shortcoming of PEGs. 

To avoid both the complexity of maintaining a stateful lexer, and the difficulty in expressing Unicode-supported lexemes, pbpg provides a `lex()` rule. This rule calls a user-supplied lexer function that expects a lexeme and number of characters read, or an error. pbpg can generate stub lexer functions for the user by using the `-stub` flag. By using lexer functions in the specification, pbpg itself maintains the state of what is expected in the token stream, leaving _just_ the actual lexing to the user. 

Actions are code fragments that are executed at the successful reduction of a production, and are specified after a production as `Action { ... }`. All action fragments are executed as functions of a user-supplied data object, and this is where the user can build parse trees, maintain other state, and return data to the code calling the generated parser. pbpg can generate a stub data struct by using the `-stub` flag.

Along with actions, the user can supply an `Error { ... }` code fragment, that will be called in place of a production's default error, if one is encountered. The Error fragment is given the default error, position, pretty-printed position, and the most recently generated lexeme and literal value. This enables the user to directly create more useful errors.

Whitespace *is trimmed* when parsing string literals. "foo" will match on both the input "foo bar" and "   foobar".

Any grammatical element that requires backtracking (repetitions, groups, optional groups), are implemented by creating a new parser, rooted at the current input, and executing it. If the parse fails, backtracking is accomplished by simply discarding the parser. If the parse is successful, the parser state is merged, and a user supplied `merge` function is called to merge any user state with the parent parser. In addition to merging, pbpg calls a user supplied fork function when a new object is created. This allows preloading state in the predictive parser. pbpg can generate both the `merge` and `fork` stubs by using the `-stub` flag.

pbpg generates a backtracking recursive descent parser. This means that there are no guarantees to the runtime of the parser, even if the supplied grammar is LL(k). pbpg parsers can take exponential time in the worst case, so care should be taken when expressing a grammar. 

# Why not just use (yacc, PEG, ANTLR)?

Tools like yacc should still be preferred when the grammar being expressed fits within the scope of an LALR(1) parser. Yacc provides guarantees about linear time processing and unambiguous parsing (alternatives in yacc are commutative). pbpg makes neither guarantee, and depends on the author to understand what precedence paths will take and generally how expensive a parse will be. That said, pbpg also allows for simpler error generation, more readable output, non-global scope, and infinite lookahead. 

Other tools, such as ANTLR, have external dependencies (The Java Runtime Environment in the case of ANTLR), and while powerful and general purpose, are far too heavyweight for the target use cases of pbpg. 

Additionally, all of these tools are cumbersome with regards to UTF-8 input, both in specification and in how terminals are defined. Specifying what a string is using EBNF, that in turn needs to accommodate portions of the printable Unicode categories is cumbersome enough that it's often simpler to just give the author a function stub and let them implement it. Often that can even be more compact and more readable code.

# Example

Below is an example of a simple integer arithmetic calculator. It takes as the first argument an input string consisting of numbers, +, -, *, /, and (). It can be built and executed with:

```
pbpg -prefix Calc calc.b
go run Calc.go "5+(10*2*(30/5))"
```


```
{
	package main

	import (
		"os"
		"log"
		"fmt"
		"strings"
		"strconv"
		"unicode/utf8"
		"unicode"
	)

	type CalcData struct {}

	func main() {
		result, err := ParseCalc(os.Args[1], nil)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(result)
	} 
}

type Expression int
type Term int
type Factor int
type AddOp string
type MultOp string
type Number int
type Neg string
type Digit string

Expression 	= Term { AddOp Term } .			Action {
								r := v1
								for i, v := range v2 {
									switch v {
									case "+":
										r = r + v3[i]
									case "-":
										r = r - v3[i]
									}
								}

								return r
							}
Term 		= Factor { MultOp Factor } .		Action {
								r := v1
								for i, v := range v2 {
									switch v {
									case "*":
										r = r * v3[i]
									case "/":
										r = r / v3[i]
									}
								}
								return r
							}
Factor		= ( "(" Expression ")" ) | Number .	Action {
								var r int
								switch a1Pos {
								case 1:
									r = v2
								case 2:
									r = v4
								}
								return r
							}
AddOp 		= "+" | "-" .				Action {
								var r string
								switch a1Pos {
								case 1:
									r = v1
								case 2:
									r = v2
								}
								return r
							}
MultOp 		= "*" | "/" .				Action { 
								var r string
								switch a1Pos {
								case 1:
									r = v1
								case 2:
									r = v2
								}
								return r
							}
Number 		= [ Neg ] Digit { Digit } .		Action {
								stringNumber := v1 + v2 + strings.Join(v3, "")
								num, _ := strconv.Atoi(stringNumber)
								return num
							}
Neg		= "-" .					Action { return v1 }
Digit 		= "0" | "1" | "2" | "3" | "4" | 
	  	  "5" | "6" | "7" | "8" | "9" . 	Action { return fmt.Sprintf("%v", a1Pos-1) }
```

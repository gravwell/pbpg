# Introduction

pbpg is a Go parser generator. It was created because, at the time, there were no practical BNF-life parser generators for Go. The gold standard parser generator, [goyacc](https://pkg.go.dev/golang.org/x/tools/cmd/goyacc), is great, but it doesn't support anything but LL(1) grammars (and also doesn't generate very idiomatic Go). 

pbpg attempts to provide a better approach to attaching the parser to a stateful lexer, as well as including error handling as a first class concept.

Additionally, pbpg provides two input mechanisms -- single string "character streams", and pre-tokenized slices of strings.

# Syntax and Constraints

pbpg input is specified in [Wirth](https://en.wikipedia.org/wiki/Wirth_syntax_notation). Additionally, a `lex()` function and a `yacc`-like code header is included. Action/Error clauses are also used to interact with data at each rule. 

pbpg is itself specified using pbpg.

The syntax (pbnf) for pbpg is as follows:

```
# This is a comment

Program     	= { Comment } [ Header ] { Types } Line { Line } .
Header      	= "{" Code "}" .
Types	    	= "type" Name lex(type) .
Line        	= Comment | Production .
Production  	= Name "=" [ Expression ] "." [ Action ] [ Error ] .
Action      	= "Action" CodeBlock .
Error       	= "Error" CodeBlock .
CodeBlock   	= "{" Code "}" .						
Expression  	= Alternative { "|" Alternative } .	
Alternative 	= Term { Term } .		
Term        	= Lex | Name | Literal | Group | Option | Repetition .
Group       	= "(" Expression ")" .		
Option      	= "[" Expression "]" .	
Repetition  	= "{" Expression "}" .
Lex         	= "lex" "(" LexFunction ")" .
Literal     	= "\"" QuotedString "\"" .	

# lexer rules

Code        	= lex(code) .			
Comment     	= "#" lex(comment) .
Name        	= lex(name) .		
LexFunction 	= lex(functionname) .
QuotedString	= lex(quotedstring) .			
```

pbpg creates unambiguous input by using left-to-right precedence, similar to Parsing Expression Grammar (PEG). If multiple paths in the parse tree could be satisfied, the left-most rule is used. For example:

```
Pet = "Caterpillar" | "Cat" .
```

Given an input "Caterpillar's make terrible pets.", pbpg will match on the first substring in the list of given alternatives. If this were specified as `"Cat" | "Caterpillar"`, the parser would use "Cat", and the user would likely not get the intended result. This is also the fundamental shortcoming of PEGs. 

To avoid both the complexity of maintaining a stateful lexer, and the difficulty in expressing Unicode-supported lexemes, pbpg provides a `lex()` rule. This rule calls a user-supplied lexer function that expects a lexeme and number of characters read, or an error. pbpg can generate stub lexer functions for the user by using the `-stub` flag. By using lexer functions in the specification, pbpg itself maintains the state of what is expected in the token stream, leaving _just_ the actual lexing to the user. 

Actions are code fragments that are executed at the successful reduction of a production, and are specified after a production as `Action { ... }`. All action fragments are executed as functions of a user-supplied data object, and this is where the user can build parse trees, maintain other state, and return data to the code calling the generated parser. Action blocks have access to the elements of the production they are called in by their position, similar to how `yacc` works. Variables in Action blocks are named `v1, v2 ...` and have the concrete type of the type they were specified with in the type declarators. Additionally, groups of alternatives also pass integers indicating which alternative was taken. For example, `foo | bar | baz` will generate variables `v1, v2, v3` and `a1Pos`. `a1Pos` indicates that it's the 1st alternative group in the production, and is a position indicator. `a1Pos` will point to which token (v1, v2, or v3) is valid.

Along with actions, the user can supply an `Error { ... }` code fragment, that will be called in place of a production's default error, if one is encountered. The Error fragment is given the default error, position, pretty-printed position, and the most recently generated lexeme and literal value. This enables the user to directly create more useful errors.

Whitespace *is trimmed* when parsing string literals. "foo" will match on both the input "foo bar" and "   foobar".

Every element that can fail and be retried is a speculative attempt: each alternative of a choice, each group and option, and each iteration of a repetition. The parser saves its position before an attempt and restores it when the attempt fails, so every alternative starts where the first did, as in a PEG. Restoring it is the attempt's job, not the production's: a production that fails can leave the position partway through what it matched, and the attempt around it, if there is one, puts it back. It also resets the variables the attempt set to their zero values, so an action sees the terms of a failed attempt as unmatched. In `[ Label ":" ] Name`, for example, `v1` is empty unless the option matched. Each iteration of a repetition starts with its variables reset the same way, so the slices an action gets hold zero values for the terms an iteration didn't match. A repetition nested in another appends to its slice once per match of its own, rather than once per iteration of the enclosing one, and a failed attempt takes back what its iterations appended. Alternative positions are undone the same way: an `aPos` names the alternative its group matched in the result, and is `0` when the group matched only in attempts the parser discarded.

The parser can't undo what actions, lexer functions, and error functions do to the data object, though. An action runs as soon as its production matches, even inside an attempt that fails afterwards, so an action that records something in the data object, such as a declared name, records it for attempts the parser then discards. A lexer function that counts brackets has the same problem. When the data object implements the generated `<prefix>Savepointer` interface, the parser calls it around every attempt:

```go
// savepoint saves the state to restore if the attempt that's starting fails.
func (p *<prefix>Data) savepoint()

// rollback restores the state saved by the latest open savepoint, and closes it.
func (p *<prefix>Data) rollback()

// release closes the latest open savepoint, keeping the changes made since.
func (p *<prefix>Data) release()
```

Savepoints nest as attempts do, so a stack of saved state is the usual implementation; `testdata/backtrack.b` has an example, and pbpg's own `pbpgData` in `emitter.go` keeps a log for undoing the map entries its actions add. The parser only checks whether the data object implements the interface, so a misspelled method silently turns rollback off; `var _ <prefix>Savepointer = (*<prefix>Data)(nil)` makes that a compile error. That guard doesn't catch a value receiver, though: `func (p <prefix>Data) savepoint()` still puts the method in the method set of `*<prefix>Data`, so the assertion compiles and the parser calls it, but each call saves and restores a copy of the data object. The three methods have to be on the pointer receiver. State that should survive a failed parse, such as completions offered to an editor, should be left out of what rollback restores, since the attempts that fail in a failed parse are rolled back too.

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

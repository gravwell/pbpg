# A grammar whose failed parses show how the parser reports errors: what it
# expected at the furthest position it reached, in one message, with labels
# standing in for what their productions expected, and real errors from lexer
# functions instead of expectations. TestErrors parses with it in string and
# token mode, writing main and the lexer functions for each.
{
package main

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// only string mode's generated code uses these
var _, _ = unicode.IsSpace, utf8.RuneError
}

Program			= Statement { Statement } .
Statement "a statement"	= Assignment | Call .
Assignment		= Name "=" Expression ";" .
Call			= Name "(" [ Expression ] ")" ";" .
Expression "an expression" = Term { Operator Term } .
Operator "an operator"	= "+" | "-" .
Term			= Number | Name | ( "(" Expression ")" ) .
Number			= lex(number) .
Name			= lex(name) .

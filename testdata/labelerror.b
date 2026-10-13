# A grammar whose Pair production has both a label and an Error fragment.
# TestLabelError checks that the fragment sees the error with the label
# applied, and that the error it returns is reported where the production's
# own errors were, which a failure partway through leaves ahead of where the
# parser stopped. Tail is there to fail further on than Pair's own errors,
# so a replacement error recorded in the wrong place would be dropped.
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

// only the generated parser uses these
var _, _ = errors.New, strings.TrimSpace
}

Program       = Pair | Tail .
Pair "a pair" = "<" Item ">" .		Error { return fmt.Errorf("at %v: %v", errPos, err) }
Item          = "x" { "," "x" } .
Tail          = "<" "x" "," "x" "," .

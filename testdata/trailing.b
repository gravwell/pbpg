# A grammar that matches almost nothing, so that anything past it is trailing
# input. TestTrailingInput checks the parser reports trailing input rather
# than returning success, both when it has expectations left to report and
# when its error stack is empty.
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
var _, _, _ = errors.New, strings.TrimSpace, fmt.Errorf
}

Program = "x" [ "y" ] .

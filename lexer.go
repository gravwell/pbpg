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
	"unicode"
)

func (p *pbpgData) lexcomment(input string) (int, string, error) {
	// eat until the end of the line
	offset := 0
	for r, s := getRune(input, offset); s > 0; r, s = getRune(input, offset) {
		offset += s
		if r == '\n' {
			break
		}
	}
	var token string
	if offset > 0 {
		token = input[:offset-1]
	}
	return offset, token, nil
}

func (p *pbpgData) lexcode(input string) (int, string, error) {
	// lexCode has to track bracket depth, escaped values, and quoted states
	offset := 0
	bdepth := 0
	quote := false
	escape := false

	for r, s := getRune(input, offset); s > 0; r, s = getRune(input, offset) {
		offset += s

		if escape {
			escape = false
			continue
		}

		if r == '\\' {
			escape = true
			continue
		}

		if quote && r != '"' {
			continue
		}

		switch r {
		case '}':
			if bdepth == 0 {
				offset--
				return offset, input[:offset], nil
			}
			bdepth--
		case '{':
			bdepth++
		case '"':
			quote = !quote
		}
	}
	return 0, "", fmt.Errorf("could not extract token")
}

func (p *pbpgData) lexname(input string) (int, string, error) {
	offset := countLeadingWhitespace(input)
	val := ""

	for r, s := getRune(input, offset); s > 0; r, s = getRune(input, offset) {
		if unicode.IsLetter(r) {
			val += string(r)
			offset += s
			continue
		}
		if len(val) == 0 {
			return 0, "", fmt.Errorf("could not extract name, got %v", string(r))
		}
		return offset, val, nil
	}
	return 0, "", fmt.Errorf("could not extract token")
}

func (p *pbpgData) lexfunctionname(input string) (int, string, error) {
	return p.lexname(input)
}

func (p *pbpgData) lexquotedstring(input string) (int, string, error) {
	// everything up to the closing quote but no leading or trailing
	// whitespace -- interior whitespace is okay
	offset := 0
	escape := false

	for r, s := getRune(input, offset); s > 0; r, s = getRune(input, offset) {
		offset += s

		if escape {
			escape = false
			continue
		}

		if r == '\\' {
			escape = true
			continue
		}

		if r == '"' {
			offset--

			val := input[:offset]

			// unescape
			val, err := strconv.Unquote(`"` + val + `"`)
			if err != nil {
				return 0, "", err
			}

			if strings.TrimSpace(val) != val {
				return 0, "", fmt.Errorf("string cannot contain leading or trailing whitespace")
			}
			return offset, val, nil
		}
	}
	return 0, "", fmt.Errorf("could not extract token")
}

func (p *pbpgData) lextype(input string) (int, string, error) {
	offset := 0
	for r, s := getRune(input, offset); s > 0; r, s = getRune(input, offset) {
		offset += s
		if r == '\n' {
			break
		}
	}
	var token string
	if offset > 0 {
		token = input[:offset-1]
	}
	return offset, token, nil
}

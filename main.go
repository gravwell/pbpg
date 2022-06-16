//go:generate pbpg pbpg.g

/*************************************************************************
 * Copyright 2022 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/
package main

import (
	"flag"
	"fmt"
	"go/format"
	"log"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	fPrefix = flag.String("prefix", "pbpg", "Prefix for user parser, data structs, and filename.")
	fStub   = flag.Bool("stub", false, "Write lexer/merge/data stub to <prefix>Data.go")
	fDebug  = flag.Bool("debug", false, "Enable debug output to stderr in the generated parser.")
)

const (
	PREFIX     = "_PREFIX_"
	ENTRYPOINT = "_ENTRYPOINT_"
)

func main() {
	flag.Parse()

	if flag.NArg() != 1 {
		log.Fatalln("need filename")
	}

	data, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		log.Fatalln(err)
	}

	p, err := Parsepbpg(string(data))
	if err != nil {
		log.Fatalln(err)
	}

	err = p.Data.verify()
	if err != nil {
		log.Fatalln(err)
	}

	p.Data.out.WriteString(strings.ReplaceAll(strings.ReplaceAll(header, PREFIX, *fPrefix), ENTRYPOINT, p.Data.entryPoint))

	formatted, err := format.Source([]byte(p.Data.out.String()))
	if err != nil {
		fmt.Println(p.Data.out.String())
		log.Fatalln(err)
	}

	os.WriteFile(fmt.Sprintf("%v.go", *fPrefix), formatted, 0644)

	if *fStub {
		funcs := make(map[string]bool)
		for _, v := range p.Data.stateMap {
			m := lexFunctions(v)
			for k, _ := range m {
				funcs[k] = true
			}
		}

		var o strings.Builder

		o.WriteString(strings.ReplaceAll(stubHeader, PREFIX, *fPrefix))
		for k, _ := range funcs {
			o.WriteString(fmt.Sprintf("func (p *%vData) lex%v(input string) (int, string, error) { return 0, \"\", nil }\n\n", *fPrefix, k))
		}

		formatted, err = format.Source([]byte(o.String()))
		if err != nil {
			fmt.Println(o.String())
			log.Fatalln(err)
		}
		os.WriteFile(fmt.Sprintf("%vData.go", *fPrefix), formatted, 0644)
	}
}

func countLeadingWhitespace(data string) int {
	count := 0
	for r, s := utf8.DecodeRuneInString(data[count:]); s > 0 && unicode.IsSpace(r); r, s = utf8.DecodeRuneInString(data[count:]) {
		count += s
	}
	return count
}

func getRune(data string, offset int) (rune, int) {
	if offset >= len(data) {
		return utf8.RuneError, 0
	}
	return utf8.DecodeRuneInString(data[offset:])
}

//go:generate pbpg pbpg.b

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
	fToken  = flag.Bool("token", false, "Use token mode instead of a string based lexer.")
	fPrint  = flag.Bool("p", false, "print the formatted grammar to stdout and exit.")
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

	input, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		log.Fatalln(err)
	}

	data := &pbpgData{
		typeMap:    make(map[string]string),
		stateMap:   make(map[string]*Expression),
		statesUsed: make(map[string]bool),
	}
	err = Parsepbpg(string(input), data)
	if err != nil {
		fmt.Println(data.out.String())
		log.Fatalln(err)
	}

	err = data.verify()
	if err != nil {
		log.Fatalln(err)
	}

	if *fPrint {
		fmt.Println(data.PrintGrammar())
		return
	}

	var h string
	if *fToken {
		h = strings.ReplaceAll(strings.ReplaceAll(headerTokenMode, PREFIX, *fPrefix), ENTRYPOINT, data.entryPoint)
	} else {
		h = strings.ReplaceAll(strings.ReplaceAll(header, PREFIX, *fPrefix), ENTRYPOINT, data.entryPoint)
	}

	// if the top level production has a type, then we have the parser return it
	if ftype, ok := data.typeMap[data.entryPoint]; ok {
		data.out.WriteString(fmt.Sprintf(h, "("+ftype+", error)", "ret, err", "return ret, err", "return ret, err"))
	} else {
		data.out.WriteString(fmt.Sprintf(h, "error", "err", "return err", "return err"))
	}

	data.out.WriteString(strings.ReplaceAll(errorRecovery, PREFIX, *fPrefix))

	formatted, err := format.Source([]byte(data.out.String()))
	if err != nil {
		fmt.Println("uh", data.out.String())
		log.Fatalln(err)
	}

	os.WriteFile(fmt.Sprintf("%v.go", *fPrefix), formatted, 0644)

	if *fStub {
		funcs := make(map[string]bool)
		for _, v := range data.stateMap {
			m := lexFunctions(v)
			for k := range m {
				funcs[k] = true
			}
		}

		var o strings.Builder

		for k := range funcs {
			if *fToken {
				o.WriteString(fmt.Sprintf("func (p *%vData) lex%v(input []string) (int, string, error) { return 0, \"\", nil }\n\n", *fPrefix, k))
			} else {
				o.WriteString(fmt.Sprintf("func (p *%vData) lex%v(input string) (int, string, error) { return 0, \"\", nil }\n\n", *fPrefix, k))
			}
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

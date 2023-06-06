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

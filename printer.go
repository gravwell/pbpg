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
	"strings"
	"text/tabwriter"
)

func (d *pbpgData) PrintGrammar() string {
	var s strings.Builder
	w := tabwriter.NewWriter(&s, 0, 0, 1, ' ', 0)
	for _, v := range d.orderedStates {
		e := d.stateMap[v]
		row := fmt.Sprintf("%v\t=\t%v\n", v, e.String())
		w.Write([]byte(row))
	}
	w.Flush()
	return s.String()
}

package table_test

import (
	"fmt"
	"os"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/table"
)

func ExampleTable() {
	tbl := table.New(
		table.Column{Key: "name", Header: "NAME"},
		table.Column{Key: "status", Header: "STATUS", Style: spec.Styler.Success},
		table.Column{Key: "size", Header: "SIZE", Right: true},
	)
	tbl.Add("alpha", "ok", 12)
	tbl.Add("bravo-longer", "ok", 3456)

	// The zero Styler writes no colour; a command passes Output.Styler for the stream instead.
	if err := tbl.Render(os.Stdout, spec.Styler{}); err != nil {
		fmt.Println(err)
	}
	// Output:
	// NAME          STATUS  SIZE
	// alpha         ✓ ok      12
	// bravo-longer  ✓ ok    3456
}

func ExampleTable_Rows() {
	tbl := table.New(
		table.Column{Key: "name", Header: "NAME"},
		table.Column{Key: "size", Header: "SIZE", Right: true},
	)
	tbl.Add("alpha", 12)
	tbl.Add("bravo", 3456)

	// The command owns the envelope: the key the rows sit under and the schema number.
	payload := struct {
		Items []table.Row `json:"items"`
	}{Items: tbl.Rows()}
	if err := spec.WriteJSON(os.Stdout, 1, payload); err != nil {
		fmt.Println(err)
	}
	// Output:
	// {
	//   "schema": 1,
	//   "items": [
	//     {
	//       "name": "alpha",
	//       "size": 12
	//     },
	//     {
	//       "name": "bravo",
	//       "size": 3456
	//     }
	//   ]
	// }
}

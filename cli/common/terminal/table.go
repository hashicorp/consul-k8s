// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"github.com/olekukonko/tablewriter"
)

const (
	Yellow  = "yellow"
	Green   = "green"
	Red     = "red"
	Blue    = "blue"
	Magenta = "magenta"
	HiWhite = "hiwhite"
)

var colorMapping = map[string]int{
	Green:   tablewriter.FgGreenColor,
	Yellow:  tablewriter.FgYellowColor,
	Red:     tablewriter.FgRedColor,
	Blue:    tablewriter.FgBlueColor,
	Magenta: tablewriter.FgMagentaColor,
	HiWhite: tablewriter.FgHiWhiteColor,
}

// Passed to UI.Table to provide a nicely formatted table.
type Table struct {
	Headers []string
	Rows    [][]Cell
}

// Cell is a single entry for a table.
type Cell struct {
	Value string
	Color string
}

// Table creates a new Table structure that can be used with UI.Table.
func NewTable(headers ...string) *Table {
	return &Table{
		Headers: headers,
	}
}

// AddRow adds a row to the table.
func (t *Table) AddRow(cols []string, colors []string) {
	var row []Cell

	for i, col := range cols {
		if i < len(colors) {
			row = append(row, Cell{Value: col, Color: colors[i]})
		} else {
			row = append(row, Cell{Value: col})
		}
	}

	t.Rows = append(t.Rows, row)
}

func (t *Table) ToJson() []map[string]interface{} {
	if t == nil {
		return make([]map[string]interface{}, 0)
	}
	jsonRes := make([]map[string]interface{}, 0)
	for _, row := range t.Rows {
		jsonRow := make(map[string]interface{})
		for i, ent := range row {
			jsonRow[t.Headers[i]] = ent.Value
		}
		jsonRes = append(jsonRes, jsonRow)
	}
	return jsonRes
}

// Table implements UI.
func (u *basicUI) Table(tbl *Table, opts ...Option) {
	// Build our config and set our options
	cfg := &config{Writer: u.bufOut}
	for _, opt := range opts {
		opt(cfg)
	}

	table := tablewriter.NewWriter(cfg.Writer)

	table.SetHeader(tbl.Headers)
	table.SetBorder(false)
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("")
	table.SetRowSeparator("")
	table.SetHeaderLine(false)
	table.SetTablePadding("\t") // pad with tabs
	table.SetNoWhiteSpace(true)

	for _, row := range tbl.Rows {
		colors := make([]tablewriter.Colors, len(row))
		entries := make([]string, len(row))

		for i, ent := range row {
			entries[i] = ent.Value

			color, ok := colorMapping[ent.Color]
			if ok {
				colors[i] = tablewriter.Colors{color}
			}
		}

		table.Rich(entries, colors)
	}

	table.Render()
}

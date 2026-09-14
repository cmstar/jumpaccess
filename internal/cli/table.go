package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/rivo/uniseg"
)

var tableCellReplacer = strings.NewReplacer(
	"\t", " ",
	"\r", " ",
	"\n", " ",
)

// writeTable 输出带表头、自动对齐且适合终端阅读的文本表格。
func writeTable(output io.Writer, headers []string, rows [][]string) error {
	if len(headers) == 0 {
		return fmt.Errorf("table headers are required")
	}

	for index, row := range rows {
		if len(row) != len(headers) {
			return fmt.Errorf("table row %d has %d columns; want %d", index+1, len(row), len(headers))
		}
	}
	// 按终端显示宽度对齐，中文占两列，组合字符按一个字形计算。
	values := make([][]string, 0, len(rows)+1)
	values = append(values, headers)
	values = append(values, rows...)
	widths := make([]int, len(headers))
	for index, row := range values {
		cells := make([]string, len(row))
		for column, cell := range row {
			cells[column] = tableCellReplacer.Replace(cell)
			widths[column] = max(widths[column], uniseg.StringWidth(cells[column]))
		}
		values[index] = cells
	}
	for _, row := range values {
		var line strings.Builder
		for column, cell := range row {
			line.WriteString(cell)
			if column < len(row)-1 {
				line.WriteString(strings.Repeat(" ", widths[column]-uniseg.StringWidth(cell)+2))
			}
		}
		if _, err := fmt.Fprintln(output, strings.TrimRight(line.String(), " ")); err != nil {
			return err
		}
	}
	return nil
}

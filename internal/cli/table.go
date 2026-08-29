package cli

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
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

	var formatted bytes.Buffer
	writer := tabwriter.NewWriter(&formatted, 0, 4, 2, ' ', 0)
	if err := writeTableRow(writer, headers); err != nil {
		return err
	}
	for index, row := range rows {
		if len(row) != len(headers) {
			return fmt.Errorf("table row %d has %d columns; want %d", index+1, len(row), len(headers))
		}
		if err := writeTableRow(writer, row); err != nil {
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}

	for _, line := range strings.Split(strings.TrimSuffix(formatted.String(), "\n"), "\n") {
		if _, err := fmt.Fprintln(output, strings.TrimRight(line, " ")); err != nil {
			return err
		}
	}
	return nil
}

func writeTableRow(output io.Writer, cells []string) error {
	values := make([]string, len(cells))
	for index, cell := range cells {
		values[index] = tableCellReplacer.Replace(cell)
	}
	_, err := fmt.Fprintln(output, strings.Join(values, "\t"))
	return err
}

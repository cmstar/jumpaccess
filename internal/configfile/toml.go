// Package configfile 在更新已知配置时保留其他版本写入的 TOML 字段。
package configfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"reflect"
	"strings"

	"github.com/BurntSushi/toml"
)

func Read(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read existing config: %w", err)
	}
	var document map[string]any
	if _, err := toml.Decode(string(data), &document); err != nil {
		return nil, fmt.Errorf("decode existing config: %w", err)
	}
	return document, nil
}

// Encode 以当前模型为准更新已知字段，保留未知字段和较高版本号。
// 动态映射只保留当前实体；带 id 的数组按 id 匹配，避免排序后扩展字段串位。
func Encode(writer io.Writer, value any, original map[string]any) error {
	var buffer bytes.Buffer
	if err := toml.NewEncoder(&buffer).Encode(value); err != nil {
		return err
	}
	var current map[string]any
	if _, err := toml.Decode(buffer.String(), &current); err != nil {
		return err
	}
	previousVersion, _ := tableValue(original, "version").(int64)
	currentVersion, _ := current["version"].(int64)
	merged := merge(original, current, reflect.TypeOf(value)).(map[string]any)
	if previousVersion > currentVersion {
		merged["version"] = previousVersion
	}
	return toml.NewEncoder(writer).Encode(merged)
}

func merge(original, current any, schema reflect.Type) any {
	for schema.Kind() == reflect.Pointer {
		schema = schema.Elem()
	}
	switch schema.Kind() {
	case reflect.Struct:
		currentTable, ok := current.(map[string]any)
		if !ok {
			// Duration 等自定义编码的结构体是标量。
			return current
		}
		previousTable, _ := original.(map[string]any)
		result := make(map[string]any, len(previousTable)+len(currentTable))
		for key, value := range previousTable {
			result[key] = value
		}
		for _, field := range reflect.VisibleFields(schema) {
			if !field.IsExported() {
				continue
			}
			name := strings.Split(field.Tag.Get("toml"), ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			// TOML 解码器允许已知字段的大小写变体，保存时统一为模型键名。
			for key := range result {
				if strings.EqualFold(key, name) {
					delete(result, key)
				}
			}
			if next, exists := currentTable[name]; exists {
				result[name] = merge(tableValue(previousTable, name), next, field.Type)
			}
		}
		return result
	case reflect.Map:
		currentTable, ok := current.(map[string]any)
		if !ok {
			return current
		}
		previousTable, _ := original.(map[string]any)
		for key, value := range currentTable {
			currentTable[key] = merge(previousTable[key], value, schema.Elem())
		}
		return currentTable
	case reflect.Slice, reflect.Array:
		currentRows, ok := current.([]map[string]any)
		if !ok {
			return current
		}
		previousRows := tableRows(original)
		byID := make(map[string]map[string]any, len(previousRows))
		for _, row := range previousRows {
			if id, ok := row["id"].(string); ok {
				byID[id] = row
			}
		}
		for index, row := range currentRows {
			var previous map[string]any
			if id, ok := row["id"].(string); ok {
				previous = byID[id]
			} else if index < len(previousRows) {
				previous = previousRows[index]
			}
			currentRows[index] = merge(previous, row, schema.Elem()).(map[string]any)
		}
		return currentRows
	default:
		return current
	}
}

func tableValue(table map[string]any, name string) any {
	if value, exists := table[name]; exists {
		return value
	}
	for key, value := range table {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return nil
}

func tableRows(value any) []map[string]any {
	if rows, ok := value.([]map[string]any); ok {
		return rows
	}
	// 行内表数组与 [[table]] 的解析表示不同，但保留规则相同。
	values, _ := value.([]any)
	rows := make([]map[string]any, len(values))
	for index, value := range values {
		rows[index], _ = value.(map[string]any)
	}
	return rows
}

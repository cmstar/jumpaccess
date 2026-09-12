package desktop

import (
	"encoding/json"
	"testing"

	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

func TestAssetViewPreservesTypeIdentifiersAndDisplayLabels(t *testing.T) {
	for _, fixture := range []string{
		`{"type":{"value":"mysql","label":"MySQL 数据库"},"category":{"value":"database","label":"数据库"}}`,
		`{"type":{"value":"mysql"},"category":{"value":"database"}}`,
	} {
		var asset jumpserver.Asset
		if err := json.Unmarshal([]byte(fixture), &asset); err != nil {
			t.Fatal(err)
		}
		view := assetView(asset, nil)
		encoded, err := json.Marshal(view)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		if fields["typeValue"] != "mysql" || fields["categoryValue"] != "database" {
			t.Fatalf("类型标识丢失: %s", encoded)
		}
		wantType, wantCategory := asset.Type.Label, asset.Category.Label
		if wantType == "" {
			wantType, wantCategory = "mysql", "database"
		}
		if view.Type != wantType || view.Category != wantCategory {
			t.Fatalf("显示名称不正确: %+v", view)
		}
	}
}

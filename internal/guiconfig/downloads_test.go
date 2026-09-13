package guiconfig

import "testing"

func TestDownloadPreferences(t *testing.T) {
	old, err := Decode([]byte("version = 11\n"))
	if err != nil || old.Downloads.Mode != "ask" || old.Downloads.LastDirectory != "" {
		t.Fatalf("旧配置默认值: %#v, %v", old, err)
	}
	for _, mode := range []string{"ask", "automatic", "remember", "custom"} {
		value := Default()
		value.Downloads.Mode = mode
		value.Downloads.Directory = t.TempDir()
		value.Downloads.LastDirectory = t.TempDir()
		store := Store{Path: t.TempDir() + "/gui.toml"}
		if err := store.Save(value); err != nil {
			t.Fatal(err)
		}
		got, err := store.Load()
		if err != nil || got.Downloads != value.Downloads {
			t.Fatalf("往返保存: %#v, %v", got, err)
		}
	}
	value := Default()
	value.Downloads.Mode = "invalid"
	if value.Validate() == nil {
		t.Fatal("必须拒绝未知下载行为")
	}
}

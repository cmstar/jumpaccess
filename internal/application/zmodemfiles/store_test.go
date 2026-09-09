package zmodemfiles

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadConfinesNamesPreservesExistingAndCleansPartial(t *testing.T) {
	var store Store
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.bin"), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	grant, err := store.GrantDirectory("ssh", dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../escape", "/tmp/escape", `C:\escape`, "a/b", "a\\b", ".", "..", "test:stream", "bad\nname"} {
		if _, err := store.CreateDownload("ssh", grant, name, 1); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	if _, err := store.CreateDownload("other", grant, "test", 1); err == nil {
		t.Fatal("cross-session grant accepted")
	}
	file, err := store.CreateDownload("ssh", grant, "test.bin", 3)
	if err != nil {
		t.Fatal(err)
	}
	if file.Name == "test.bin" {
		t.Fatal("existing filename reused")
	}
	if err := store.Write(file.ID, []byte{0, 128, 255}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(file.ID, true); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, file.Name))
	if !bytes.Equal(got, []byte{0, 128, 255}) {
		t.Fatalf("download = %v", got)
	}
	got, _ = os.ReadFile(filepath.Join(dir, "test.bin"))
	if string(got) != "existing" {
		t.Fatal("overwrote existing file")
	}
	partial, err := store.CreateDownload("ssh", grant, "partial", 10)
	if err != nil {
		t.Fatal(err)
	}
	store.CloseSession("ssh")
	if _, err := os.Stat(filepath.Join(dir, partial.Name)); !os.IsNotExist(err) {
		t.Fatal("partial file left behind")
	}
}

func TestUploadStreamsBinaryAndIncompleteDownloadFails(t *testing.T) {
	var store Store
	dir := t.TempDir()
	data := bytes.Repeat([]byte{0, 128, 255}, 100_000)
	path := filepath.Join(dir, "source.bin")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	files, err := store.OpenUploads("ssh", []string{path})
	if err != nil {
		t.Fatal(err)
	}
	var got []byte
	for {
		chunk, err := store.Read(files[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(chunk) > 64*1024 {
			t.Fatal("unbounded read")
		}
		if len(chunk) == 0 {
			break
		}
		got = append(got, chunk...)
	}
	if !bytes.Equal(data, got) {
		t.Fatal("upload corrupted")
	}
	store.CloseSession("ssh")
	grant, _ := store.GrantDirectory("ssh", dir)
	download, _ := store.CreateDownload("ssh", grant, "short", 5)
	if err := store.Close(download.ID, true); err == nil {
		t.Fatal("accepted truncated download")
	}
	if _, err := os.Stat(filepath.Join(dir, "short")); !os.IsNotExist(err) {
		t.Fatal("truncated file left behind")
	}
}

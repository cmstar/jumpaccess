package sftpsession

import (
	"context"
	"fmt"
	"os"
	"testing"
)

type noHardLinks struct{ *os.Root }

func (noHardLinks) Link(string, string) error {
	return fmt.Errorf("hard links unsupported by filesystem")
}
func TestLocalPublishSupportsFilesystemsWithoutHardLinks(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	_ = root.WriteFile("temp", []byte("completed bytes"), 0600)
	if err := publishLocal(context.Background(), noHardLinks{root}, "temp", "download.txt"); err != nil {
		t.Fatal(err)
	}
	b, _ := root.ReadFile("download.txt")
	if string(b) != "completed bytes" {
		t.Fatalf("published=%q", b)
	}
}

func TestFailedLocalPublishRemovesOnlyItsPartialFile(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	_ = root.Mkdir("unreadable-source", 0700)
	if err := publishLocal(context.Background(), noHardLinks{root}, "unreadable-source", "download.txt"); err == nil {
		t.Fatal("expected source read failure")
	}
	if _, err := root.Lstat("download.txt"); !os.IsNotExist(err) {
		t.Fatalf("partial destination remains: %v", err)
	}
}

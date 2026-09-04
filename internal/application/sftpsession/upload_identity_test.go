package sftpsession

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUploadRejectsSourceReplacedWhileConflictWaits(t *testing.T) {
	remote := &standardRenameRemote{newLocalRemote(t)}
	if err := os.WriteFile(remote.local("/report.txt"), []byte("remote original"), 0600); err != nil {
		t.Fatal(err)
	}
	manager := testManager(t, remote)
	session := activeSession(t, manager)
	source := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(source, []byte("selected source"), 0600); err != nil {
		t.Fatal(err)
	}
	events, err := manager.StartTransfer(TransferRequest{SessionID: session.ID, Direction: "upload", Sources: []string{source}, Destination: "/"})
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, manager, events[0].ID, "conflict")
	if err := os.Rename(source, source+".selected"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("different file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := manager.ResolveConflict(events[0].ID, "overwrite", false); err != nil {
		t.Fatal(err)
	}
	failed := waitTransfer(t, manager, events[0].ID, "failed")
	if failed.Transferred != 0 || !strings.Contains(failed.Error, "source changed") {
		t.Fatalf("unexpected failure: %+v", failed)
	}
	data, err := os.ReadFile(remote.local("/report.txt"))
	if err != nil || string(data) != "remote original" {
		t.Fatalf("remote original changed: %q, %v", data, err)
	}
}

func TestDirectoryUploadRejectsSourceReplacedWhileConflictWaits(t *testing.T) {
	remote := newLocalRemote(t)
	if err := os.WriteFile(remote.local("/folder"), []byte("remote original"), 0600); err != nil {
		t.Fatal(err)
	}
	manager := testManager(t, remote)
	session := activeSession(t, manager)
	source := filepath.Join(t.TempDir(), "folder")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "selected.txt"), []byte("selected source"), 0600); err != nil {
		t.Fatal(err)
	}
	events, err := manager.StartTransfer(TransferRequest{SessionID: session.ID, Direction: "upload", Sources: []string{source}, Destination: "/"})
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, manager, events[0].ID, "conflict")
	if err := os.Rename(source, source+".selected"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "different.txt"), []byte("different directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := manager.ResolveConflict(events[0].ID, "keep-both", false); err != nil {
		t.Fatal(err)
	}
	failed := waitTransfer(t, manager, events[0].ID, "failed")
	if failed.Transferred != 0 || !strings.Contains(failed.Error, "source changed") {
		t.Fatalf("unexpected failure: %+v", failed)
	}
	entries, err := os.ReadDir(remote.root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "folder" {
		t.Fatalf("remote directory changed: %+v, %v", entries, err)
	}
}

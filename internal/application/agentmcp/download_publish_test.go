package agentmcp

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func publishFixture(t *testing.T, names []remoteEntry, root string) (*os.File, remoteArtifact) {
	t.Helper()
	var data bytes.Buffer
	format := "gzip"
	gz := gzip.NewWriter(&data)
	if len(names) == 1 && !names[0].Directory {
		_, _ = gz.Write([]byte("log"))
	} else {
		format = "tar.gz"
		tw := tar.NewWriter(gz)
		for _, entry := range names {
			header := &tar.Header{Name: entry.Name, Mode: 0600, Size: entry.Size}
			if entry.Directory {
				header.Typeflag, header.Mode = tar.TypeDir, 0700
			}
			if err := tw.WriteHeader(header); err != nil {
				t.Fatal(err)
			}
			if !entry.Directory {
				_, _ = tw.Write([]byte("log"))
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(t.TempDir(), "artifact")
	if err := os.WriteFile(name, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return file, remoteArtifact{Format: format, Size: int64(data.Len()), Entries: names, RootName: root}
}

func TestPublishDownloadUsesSingleTopLevelAndKeepsCollisions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []remoteEntry
	}{
		{"app.log", []remoteEntry{{Name: "app.log", Source: "/log", Size: 3}}},
		{"logs", []remoteEntry{{Name: "logs", Directory: true}, {Name: "logs/app.log", Source: "/log", Size: 3}, {Name: "logs/empty", Directory: true}}},
		{"JumpAccess-task", []remoteEntry{{Name: "JumpAccess-task", Directory: true}, {Name: "JumpAccess-task/app.log", Source: "/log", Size: 3}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			original := filepath.Join(base, tc.name)
			if err := os.WriteFile(original, []byte("existing"), 0600); err != nil {
				t.Fatal(err)
			}
			file, a := publishFixture(t, tc.entries, tc.name)
			result, err := publishDownload(context.Background(), file, base, a, "test")
			if err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(original)
			if string(got) != "existing" {
				t.Fatal("overwrote existing target")
			}
			objects, err := os.ReadDir(base)
			if err != nil || len(objects) != 2 {
				t.Fatalf("unexpected layout or leftover: %v %v", objects, err)
			}
			if len(result.Files) != 1 || result.Files[0].Path == original || !filepath.IsAbs(result.Files[0].Path) {
				t.Fatalf("invalid result: %+v", result)
			}
			got, err = os.ReadFile(result.Files[0].Path)
			if err != nil || string(got) != "log" {
				t.Fatalf("content: %s, %v", got, err)
			}
			if tc.name == "app.log" && result.Directory != base {
				t.Fatalf("single file created permanent task directory: %+v", result)
			}
			if tc.name != "app.log" && filepath.Dir(result.Directory) != base {
				t.Fatalf("directory nested twice: %+v", result)
			}
		})
	}
}

func TestPublishDownloadCancelledLeavesOnlyExistingFiles(t *testing.T) {
	base := t.TempDir()
	file, a := publishFixture(t, []remoteEntry{{Name: "app.log", Size: 3}}, "app.log")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := publishDownload(ctx, file, base, a, "cancelled")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	entries, _ := os.ReadDir(base)
	if len(entries) != 0 {
		t.Fatalf("partial result leaked: %v", entries)
	}
}

func TestPublishDownloadConcurrentSameNameNeverMerges(t *testing.T) {
	base := t.TempDir()
	const count = 4
	var wg sync.WaitGroup
	results := make(chan DownloadResult, count)
	for i := 0; i < count; i++ {
		file, a := publishFixture(t, []remoteEntry{{Name: "logs", Directory: true}, {Name: "logs/app.log", Size: 3}}, "logs")
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := publishDownload(context.Background(), file, base, a, "concurrent")
			if err != nil {
				t.Error(err)
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	seen := map[string]bool{}
	for result := range results {
		if seen[result.Directory] {
			t.Fatal("shared result directory")
		}
		seen[result.Directory] = true
	}
	entries, _ := os.ReadDir(base)
	if len(entries) != count {
		t.Fatalf("unexpected output entries: %v", entries)
	}
}

func TestPublishDownloadEmptyDirectoryDoesNotMergeExistingDirectory(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(base, "empty", "existing")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	file, a := publishFixture(t, []remoteEntry{{Name: "empty", Directory: true}}, "empty")
	result, err := publishDownload(context.Background(), file, base, a, "empty")
	if err != nil {
		t.Fatal(err)
	}
	if result.Directory == filepath.Join(base, "empty") || len(result.Files) != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	children, err := os.ReadDir(result.Directory)
	if err != nil || len(children) != 0 {
		t.Fatalf("empty directory was lost: %v %v", children, err)
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "keep" {
		t.Fatalf("existing directory changed: %v", err)
	}
}

func TestPublishDownloadInvalidArchiveCleansStagingBeforePublishing(t *testing.T) {
	base := t.TempDir()
	file, a := publishFixture(t, []remoteEntry{{Name: "app.log", Size: 4}}, "app.log")
	_, err := publishDownload(context.Background(), file, base, a, "bad-size")
	if err == nil {
		t.Fatal("accepted incorrect restored size")
	}
	children, err := os.ReadDir(base)
	if err != nil || len(children) != 0 {
		t.Fatalf("partial extraction left behind: %v %v", children, err)
	}
}

func TestPublishDownloadMissingTargetDoesNotCreateIt(t *testing.T) {
	base := filepath.Join(t.TempDir(), "missing")
	file, a := publishFixture(t, []remoteEntry{{Name: "app.log", Size: 3}}, "app.log")
	_, err := publishDownload(context.Background(), file, base, a, "missing")
	if !os.IsNotExist(err) {
		t.Fatalf("missing directory changed error classification: %v", err)
	}
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Fatalf("created missing target: %v", err)
	}
}

type publishingContext struct {
	context.Context
	err func() error
}

func (c publishingContext) Err() error { return c.err() }

func TestPublishDownloadMidPublishCancellationCleansOnlyOwnedPaths(t *testing.T) {
	for _, addForeignFile := range []bool{false, true} {
		t.Run(fmt.Sprint(addForeignFile), func(t *testing.T) {
			base := t.TempDir()
			file, a := publishFixture(t, []remoteEntry{{Name: "logs", Directory: true}, {Name: "logs/a.log", Size: 3}, {Name: "logs/b.log", Size: 3}}, "logs")
			foreign := filepath.Join(base, "logs", "foreign")
			cancelled := false
			ctx := publishingContext{Context: context.Background(), err: func() error {
				if !cancelled {
					children, _ := os.ReadDir(filepath.Join(base, "logs"))
					if len(children) > 0 {
						cancelled = true
						if addForeignFile {
							if err := os.WriteFile(foreign, []byte("keep"), 0600); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
				if cancelled {
					return context.Canceled
				}
				return nil
			}}
			result, err := publishDownload(ctx, file, base, a, "cancel")
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation not preserved: %v", err)
			}
			children, err := os.ReadDir(base)
			if err != nil {
				t.Fatal(err)
			}
			if !addForeignFile && len(children) != 0 {
				t.Fatalf("owned partial was not cleaned: %v", children)
			}
			if addForeignFile {
				got, err := os.ReadFile(foreign)
				if err != nil || string(got) != "keep" {
					t.Fatalf("removed foreign file: %v", err)
				}
				remaining, err := os.ReadDir(filepath.Dir(foreign))
				if err != nil || len(remaining) != 1 || len(result.Warnings) == 0 {
					t.Fatalf("partial cleanup or residue warning missing: %v %+v %v", remaining, result, err)
				}
			}
		})
	}
}

func TestRemoveOwnedDownloadDirectoryNeverRemovesReplacement(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		t.Run(fmt.Sprint(replaced), func(t *testing.T) {
			base := t.TempDir()
			root, err := os.OpenRoot(base)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if err = root.Mkdir("owned", 0700); err != nil {
				t.Fatal(err)
			}
			info, err := root.Lstat("owned")
			if err != nil {
				t.Fatal(err)
			}
			if replaced {
				if err = os.Rename(filepath.Join(base, "owned"), filepath.Join(base, "moved")); err != nil {
					t.Fatal(err)
				}
				if err = root.Mkdir("owned", 0700); err != nil {
					t.Fatal(err)
				}
			}
			marker := filepath.Join(base, "owned", "marker")
			if err = os.WriteFile(marker, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			err = removeOwnedDownloadDirectory(root, "owned", info)
			if replaced {
				if err == nil {
					t.Fatal("replacement accepted for cleanup")
				}
				if data, err := os.ReadFile(marker); err != nil || string(data) != "keep" {
					t.Fatalf("removed replacement: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := root.Lstat("owned"); !os.IsNotExist(err) {
					t.Fatalf("owned temporary directory retained: %v", err)
				}
			}
		})
	}
}

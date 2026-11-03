package agentmcp

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareUploadContentAndCleanup(t *testing.T) {
	for _, format := range []string{"gzip", "raw", "tar.gz"} {
		t.Run(format, func(t *testing.T) {
			base := t.TempDir()
			name := "config.txt"
			if format == "raw" {
				name = "bundle.zip"
			}
			source := filepath.Join(base, name)
			content := bytes.Repeat([]byte("configuration\n"), 1000)
			if err := os.WriteFile(source, content, 0600); err != nil {
				t.Fatal(err)
			}
			paths := []string{source}
			if format == "tar.gz" {
				if err := os.Mkdir(filepath.Join(base, "empty"), 0700); err != nil {
					t.Fatal(err)
				}
				paths = []string{base, source, base}
			}
			p, err := prepareUpload(context.Background(), paths, 1<<30)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			if p.Format != format {
				t.Fatalf("format = %s", p.Format)
			}
			if format == "raw" {
				if p.File.Name() != source {
					t.Fatal("raw file was copied")
				}
				got, _ := io.ReadAll(p.File)
				if !bytes.Equal(got, content) {
					t.Fatal("raw content differs")
				}
			} else {
				gz, err := gzip.NewReader(p.File)
				if err != nil {
					t.Fatal(err)
				}
				if format == "gzip" {
					got, err := io.ReadAll(gz)
					if err != nil || !bytes.Equal(got, content) {
						t.Fatalf("gzip content: %v", err)
					}
				} else {
					tr := tar.NewReader(gz)
					names := map[string]bool{}
					for {
						h, err := tr.Next()
						if err == io.EOF {
							break
						}
						if err != nil {
							t.Fatal(err)
						}
						names[h.Name] = true
					}
					if len(names) != 3 || !names[filepath.Base(base)+"/empty"] {
						t.Fatalf("archive layout: %v", names)
					}
				}
				gz.Close()
			}
			artifact := p.File.Name()
			if err := p.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(source); err != nil {
				t.Fatal("source removed", err)
			}
			if format != "raw" {
				if _, err := os.Stat(artifact); !os.IsNotExist(err) {
					t.Fatal("temporary artifact remains")
				}
			}
		})
	}
}

func TestPrepareUploadLimitsConflictsAndCancellation(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "large.txt")
	os.WriteFile(source, bytes.Repeat([]byte("a"), 100000), 0600)
	p, err := prepareUpload(context.Background(), []string{source}, 1000)
	if err != nil {
		t.Fatal("limit must count compressed bytes", err)
	}
	p.Close()
	_, err = prepareUpload(context.Background(), []string{source}, 10)
	var f *Fault
	if !errors.As(err, &f) || f.Code != "TRANSFER_TOO_LARGE" {
		t.Fatalf("limit error = %v", err)
	}
	other := filepath.Join(base, "other")
	os.Mkdir(other, 0700)
	duplicate := filepath.Join(other, "large.txt")
	os.WriteFile(duplicate, []byte("b"), 0600)
	_, err = prepareUpload(context.Background(), []string{source, duplicate}, 1<<30)
	if !errors.As(err, &f) || f.Code != "UPLOAD_NAME_CONFLICT" {
		t.Fatalf("duplicate names = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = prepareUpload(ctx, []string{source}, 1<<30); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	link := filepath.Join(base, "link")
	if err = os.Symlink(source, link); err == nil {
		if _, err = prepareUpload(context.Background(), []string{link}, 1<<30); err == nil {
			t.Fatal("accepted symlink")
		}
	}
}

func TestPrepareUploadMissingSourceHasDistinctError(t *testing.T) {
	_, err := prepareUpload(context.Background(), []string{filepath.Join(t.TempDir(), "missing")}, 1<<30)
	var f *Fault
	if !errors.As(err, &f) || f.Code != "LOCAL_SOURCE_NOT_FOUND" {
		t.Fatalf("missing source: %v", err)
	}
}

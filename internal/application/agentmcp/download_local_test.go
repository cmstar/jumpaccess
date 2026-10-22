package agentmcp

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreDownloadVerifiesAndRejectsArchiveTraversal(t *testing.T) {
	content := []byte("bounded log\n")
	for _, name := range []string{"logs/app.log", "../escape", "C:/escape", "logs/CON", "logs/evil:stream"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			var data bytes.Buffer
			gz := gzip.NewWriter(&data)
			tw := tar.NewWriter(gz)
			_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(content))})
			_, _ = tw.Write(content)
			tw.Close()
			gz.Close()
			artifact := remoteArtifact{Format: "tar.gz", Size: int64(data.Len()), Entries: []remoteEntry{{Name: name, Source: "/log", Size: int64(len(content))}}}
			filename := filepath.Join(root, "artifact")
			os.WriteFile(filename, data.Bytes(), 0600)
			out := filepath.Join(root, "out")
			os.Mkdir(out, 0700)
			_, err := restoreDownload(context.Background(), filename, out, artifact)
			if name == "logs/app.log" {
				if err != nil {
					t.Fatal(err)
				}
				got, _ := os.ReadFile(filepath.Join(out, "logs", "app.log"))
				if !bytes.Equal(got, content) {
					t.Fatal("incorrect contents")
				}
			} else if err == nil {
				t.Fatal("unsafe entry accepted")
			}
		})
	}
}
func TestRestoreDownloadPreservesRequestedArchiveWithoutDigest(t *testing.T) {
	root := t.TempDir()
	data := []byte("original gz bytes")
	file := filepath.Join(root, "artifact")
	os.WriteFile(file, data, 0600)
	artifact := remoteArtifact{Format: "raw", Size: int64(len(data)), Entries: []remoteEntry{{Name: "app.log.gz", Size: int64(len(data))}}}
	out := filepath.Join(root, "out")
	os.Mkdir(out, 0700)
	files, err := restoreDownload(context.Background(), file, out, artifact)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(out, "app.log.gz"))
	if !bytes.Equal(got, data) {
		t.Fatal("original archive changed")
	}
	encoded, err := json.Marshal(files)
	if err != nil || bytes.Contains(encoded, []byte(`"sha256"`)) {
		t.Fatalf("不应返回未计算的内容哈希：%s %v", encoded, err)
	}
}

func TestRestoreDownloadWithoutDigestChecksLengthsAndGzipTrailer(t *testing.T) {
	content := bytes.Repeat([]byte("bounded log\n"), 8000)
	for _, format := range []string{"raw", "gzip", "tar.gz"} {
		t.Run(format, func(t *testing.T) {
			var data bytes.Buffer
			if format == "raw" {
				data.Write(content)
			} else {
				gz := gzip.NewWriter(&data)
				if format == "tar.gz" {
					tw := tar.NewWriter(gz)
					if err := tw.WriteHeader(&tar.Header{Name: "app.log", Mode: 0600, Size: int64(len(content))}); err != nil {
						t.Fatal(err)
					}
					if _, err := tw.Write(content); err != nil {
						t.Fatal(err)
					}
					if err := tw.Close(); err != nil {
						t.Fatal(err)
					}
				} else if _, err := gz.Write(content); err != nil {
					t.Fatal(err)
				}
				if err := gz.Close(); err != nil {
					t.Fatal(err)
				}
			}
			for _, scenario := range []string{"valid", "artifact-short", "artifact-long", "entry-short", "entry-long", "crc", "trailer-short", "missing-entry"} {
				if format == "raw" && (scenario == "crc" || scenario == "trailer-short") {
					continue
				}
				if scenario == "missing-entry" && format != "tar.gz" {
					continue
				}
				t.Run(scenario, func(t *testing.T) {
					payload := bytes.Clone(data.Bytes())
					a := remoteArtifact{Format: format, Size: int64(len(payload)), Entries: []remoteEntry{{Name: "app.log", Size: int64(len(content))}}}
					switch scenario {
					case "artifact-short":
						a.Size++
					case "artifact-long":
						a.Size--
					case "entry-short":
						a.Entries[0].Size++
					case "entry-long":
						a.Entries[0].Size--
					case "crc":
						payload[len(payload)-8] ^= 0xff
					case "trailer-short":
						payload = payload[:len(payload)-2]
						a.Size = int64(len(payload))
					case "missing-entry":
						a.Entries = append(a.Entries, remoteEntry{Name: "missing.log", Size: 1})
					}
					root := t.TempDir()
					filename := filepath.Join(root, "artifact")
					if err := os.WriteFile(filename, payload, 0600); err != nil {
						t.Fatal(err)
					}
					out := filepath.Join(root, "out")
					if err := os.Mkdir(out, 0700); err != nil {
						t.Fatal(err)
					}
					_, err := restoreDownload(context.Background(), filename, out, a)
					if scenario == "valid" {
						if err != nil {
							t.Fatal(err)
						}
						got, err := os.ReadFile(filepath.Join(out, "app.log"))
						if err != nil || !bytes.Equal(got, content) {
							t.Fatalf("还原内容不符：%v", err)
						}
					} else if err == nil {
						t.Fatal("损坏或不完整的内容被接受")
					}
					if scenario == "crc" && !errors.Is(err, gzip.ErrChecksum) {
						t.Fatalf("应读取至 gzip 尾部并拒绝 CRC 错误：%v", err)
					}
					if scenario == "trailer-short" && !errors.Is(err, io.ErrUnexpectedEOF) {
						t.Fatalf("应拒绝缺失的 gzip 尾部：%v", err)
					}
				})
			}
		})
	}
}

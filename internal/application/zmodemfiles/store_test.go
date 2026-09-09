package zmodemfiles

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadBuffersUntilCompletion(t *testing.T) {
	var store Store
	dir := t.TempDir()
	grant, err := store.GrantDirectory("ssh", dir)
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte{0, 128, 255}, 100_000)
	file, err := store.CreateDownload("ssh", grant, "buffered.bin", int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.CloseSession("") })
	for offset := 0; offset < len(data); offset += ChunkSize {
		if err := store.Write(file.ID, data[offset:min(offset+ChunkSize, len(data))]); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(filepath.Join(dir, file.Name))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("下载完成前写入了 %d 字节", info.Size())
	}
	if err := store.Close(file.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, file.Name))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("缓冲内容未完整保存: %v", err)
	}
}

func TestDownloadBufferBudgetBatchingAndCleanup(t *testing.T) {
	var store Store
	dir := t.TempDir()
	t.Cleanup(func() { store.CloseSession("") })
	create := func(session, name string, size int64) File {
		t.Helper()
		grant, err := store.GrantDirectory(session, dir)
		if err != nil {
			t.Fatal(err)
		}
		file, err := store.CreateDownload(session, grant, name, size)
		if err != nil {
			t.Fatal(err)
		}
		return file
	}
	large := create("first", "large", downloadBufferLimit+ChunkSize)
	other := create("second", "other", ChunkSize)
	if store.bufferReserved != downloadBufferLimit {
		t.Fatalf("缓冲总额 = %d", store.bufferReserved)
	}
	chunk := bytes.Repeat([]byte{0xa5}, ChunkSize)
	for range downloadBufferLimit / ChunkSize {
		if err := store.Write(large.ID, chunk); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(filepath.Join(dir, large.Name))
	if err != nil || info.Size() != 0 {
		t.Fatalf("恰好 50 MiB 时不应提前刷盘: %v", err)
	}
	if err := store.Write(large.ID, chunk); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(filepath.Join(dir, large.Name))
	if err != nil || info.Size() != downloadBufferLimit {
		t.Fatalf("越过缓冲边界应批量写入 50 MiB: %v", err)
	}
	if err := store.Write(other.ID, chunk); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(other.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(large.ID, true); err != nil {
		t.Fatal(err)
	}
	for _, file := range []File{large, other} {
		got, err := os.ReadFile(filepath.Join(dir, file.Name))
		if err != nil || int64(len(got)) != file.Size || bytes.Count(got, []byte{0xa5}) != len(got) {
			t.Fatalf("缓冲或降级写入丢失数据: %v", err)
		}
	}
	if store.bufferReserved != 0 {
		t.Fatal("完成后未释放缓冲额度")
	}
	partial := create("cancel", "partial", downloadBufferLimit)
	if err := store.Write(partial.ID, chunk); err != nil {
		t.Fatal(err)
	}
	store.CloseSession("cancel")
	if store.bufferReserved != 0 {
		t.Fatal("断连后未释放缓冲额度")
	}
	if _, err := os.Stat(filepath.Join(dir, partial.Name)); !os.IsNotExist(err) {
		t.Fatal("断连后留下部分文件")
	}
	failed := create("failure", "failed", int64(len(chunk)))
	if err := store.Write(failed.ID, chunk); err != nil {
		t.Fatal(err)
	}
	// 模拟保存时底层文件句柄失效，Flush 失败不能报告完成。
	if err := store.files[failed.ID].file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(failed.ID, true); err == nil {
		t.Fatal("忽略了保存错误")
	}
	if store.bufferReserved != 0 {
		t.Fatal("保存失败后未释放缓冲额度")
	}
	if _, err := os.Stat(filepath.Join(dir, failed.Name)); !os.IsNotExist(err) {
		t.Fatal("保存失败后留下部分文件")
	}
	empty := create("empty", "empty", 0)
	if err := store.Close(empty.ID, true); err != nil {
		t.Fatal(err)
	}
}

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

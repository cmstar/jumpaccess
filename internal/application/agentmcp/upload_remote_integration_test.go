package agentmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func uploadLinuxFixture(t *testing.T) (EngineOptions, string, string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		return downloadLinuxFixture(t)
	}
	distro := os.Getenv("JUMPACCESS_TEST_WSL")
	if distro == "" {
		t.Skip("设置 JUMPACCESS_TEST_WSL 以运行 POSIX 上传集成测试")
	}
	output, err := exec.Command("wsl.exe", "-d", distro, "--", "python3", "-c", "import tempfile;print(tempfile.mkdtemp(prefix='jumpaccess-upload-test-',dir='/home'))").Output()
	if err != nil {
		t.Fatal(err)
	}
	remote := strings.TrimSpace(string(output))
	if !strings.HasPrefix(remote, "/home/jumpaccess-upload-test-") || strings.ContainsAny(strings.TrimPrefix(remote, "/home/"), "/\\\r\n") {
		t.Fatal("invalid test directory")
	}
	directory := `\\wsl.localhost\` + distro + filepath.FromSlash(remote)
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Error(err)
		}
	})
	return downloadLinuxFixtureAt(t, directory)
}

func TestUploadRemoteThroughSSH(t *testing.T) {
	opts, directory, stage := uploadLinuxFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	client, err := opts.Dial(ctx, ResolvedTarget{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	source := filepath.Join(directory, "config.txt")
	os.WriteFile(source, []byte("upload config"), 0600)
	p, err := prepareUpload(ctx, []string{source}, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	name := ".jumpaccess-upload-integration"
	body, owned, err := remoteUploadCall(ctx, client, map[string]any{"op": "create_stage", "stage": linuxDownloadPath(t, stage), "name": name}, nil)
	if err != nil || len(owned) != 1 {
		t.Fatalf("create stage: %s %v %v", body, owned, err)
	}
	var remote ownedRemoteFile
	if err = json.Unmarshal(body, &remote); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p.File.Name())
	os.WriteFile(filepath.Join(stage, name, "payload"), data, 0600)
	destination := filepath.Join(directory, "destination")
	os.Mkdir(destination, 0700)
	body, restored, err := remoteUploadCall(ctx, client, map[string]any{"op": "restore", "stage": remote, "destination": linuxDownloadPath(t, destination), "format": p.Format, "size": p.Size, "entries": p.Entries}, nil)
	if err != nil {
		t.Fatalf("restore: %s %v", body, err)
	}
	actual, err := os.ReadFile(filepath.Join(destination, "config.txt"))
	if err != nil || string(actual) != "upload config" {
		t.Fatalf("published: %q %v result=%s", actual, err, body)
	}
	_, _, err = remoteUploadCall(ctx, client, map[string]any{"op": "cleanup", "files": append(owned, restored...)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	left, _ := os.ReadDir(stage)
	if len(left) != 0 {
		t.Fatalf("stage residue: %v", left)
	}
	// 大目录清单经 stdin 发送，不能受 SSH 命令参数长度限制。
	entries := []remoteEntry{{Name: "large", Directory: true}}
	for i := 0; i < 999; i++ {
		entries = append(entries, remoteEntry{Name: fmt.Sprintf("large/%04d-%s", i, strings.Repeat("a", 160))})
	}
	_, _, err = remoteUploadCall(ctx, client, map[string]any{"op": "check_target", "destination": linuxDownloadPath(t, destination), "format": "tar.gz", "size": 0, "entries": entries}, nil)
	if err != nil {
		t.Fatal("large manifest", err)
	}
}

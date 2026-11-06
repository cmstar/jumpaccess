package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cmstar/jumpaccess/internal/filelock"
)

func TestFixedTestExecutables(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		t.Helper()
		filename := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.test/fixture\n\ngo 1.25.0\n")
	fixture := func(version string) string {
		return fmt.Sprintf(`package fixture
import ("fmt"; "os"; "testing"; "time")
func TestLocation(t *testing.T) {
  exe, err := os.Executable(); if err != nil { t.Fatal(err) }
  cwd, err := os.Getwd(); if err != nil { t.Fatal(err) }
  fmt.Printf("LOCATION=%%s|%%s|%s\n", exe, cwd)
}
func TestFailure(t *testing.T) { t.Fatal("fixture failure") }
func TestTimeout(t *testing.T) { time.Sleep(time.Minute) }
func TestUnexpectedExit(t *testing.T) { os.Exit(0) }
`, version)
	}
	// 根包与两个子包末级名称相同，不能按包名共用一个输出文件。
	for _, relative := range []string{"", "a/fixture", "b/fixture"} {
		write(filepath.Join(relative, "fixture_test.go"), fixture("first"))
	}
	write("empty/empty.go", "package empty\n")
	run := func(args ...string) (string, error) {
		t.Helper()
		var output bytes.Buffer
		options, err := parseOptions(args, &output)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		err = runTests(ctx, root, options, &output, &output)
		return output.String(), err
	}
	executable := func(relative string) string {
		if relative == "" {
			relative = "_root"
		}
		suffix := ""
		if runtime.GOOS == "windows" {
			suffix = ".exe"
		}
		return filepath.Join(root, ".tmp", "testbin", relative, "fixture.test"+suffix)
	}
	first, err := run("-run", "^TestLocation$", "-count", "2")
	if err != nil {
		t.Fatalf("首次测试失败: %v\n%s", err, first)
	}
	if strings.Count(first, "LOCATION=") != 6 || !strings.Contains(first, "[no test files]") {
		t.Fatalf("重复次数或无测试包输出错误:\n%s", first)
	}
	for _, relative := range []string{"", "a/fixture", "b/fixture"} {
		want := fmt.Sprintf("LOCATION=%s|%s|first", executable(relative), filepath.Join(root, relative))
		if !strings.Contains(first, want) {
			t.Fatalf("未从固定路径和对应包目录运行，缺少 %s\n%s", want, first)
		}
	}
	write("a/fixture/fixture_test.go", fixture("second"))
	second, err := run("-run", "^TestLocation$", "./a/fixture")
	if err != nil || strings.Count(second, "LOCATION=") != 1 || !strings.Contains(second, "LOCATION="+executable("a/fixture")+"|"+filepath.Join(root, "a/fixture")+"|second") {
		t.Fatalf("重新编译后应在原路径运行新版本: %v\n%s", err, second)
	}
	for _, tc := range []struct {
		name, diagnostic string
		args             []string
	}{
		{"failure", "fixture failure", []string{"-run", "^TestFailure$"}},
		{"timeout", "test timed out", []string{"-run", "^TestTimeout$", "-timeout", "100ms"}},
		{"unexpected exit", "unexpected call to os.Exit(0)", []string{"-run", "^TestUnexpectedExit$"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, err := run(append(tc.args, "./a/fixture")...)
			if err == nil || !strings.Contains(output, tc.diagnostic) {
				t.Fatalf("失败未正确传递: %v\n%s", err, output)
			}
		})
	}
	write("empty/empty.go", "package empty\nvar value int = \"invalid\"\n")
	if output, err := run("./empty"); err == nil {
		t.Fatalf("没有测试的包也必须编译检查:\n%s", output)
	}
	write("a/fixture/fixture_test.go", fixture("third")+"\ninvalid Go syntax\n")
	if output, err := run("-run", "^TestLocation$", "./a/fixture"); err == nil || strings.Contains(output, "LOCATION=") {
		t.Fatalf("编译失败后不得运行旧可执行文件: %v\n%s", err, output)
	}
}

func TestConcurrentRunnerWaitCanBeCancelled(t *testing.T) {
	root := t.TempDir()
	unlock, err := (filelock.Locker{Dir: filepath.Join(root, ".tmp", "testbin")}).Lock(context.Background(), "runner")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var output bytes.Buffer
	options, err := parseOptions(nil, &output)
	if err != nil {
		t.Fatal(err)
	}
	if err := runTests(ctx, root, options, &output, &output); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("并发运行应等待同一文件锁并响应取消，实际 %v", err)
	}
}

// testgo 从固定路径运行各包测试，避免为临时构建路径反复授权防火墙。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cmstar/jumpaccess/internal/filelock"
)

type testOptions struct {
	race, short, verbose bool
	tags, run, skip      string
	count                int
	timeout              time.Duration
	packages             []string
}

func parseOptions(args []string, output io.Writer) (testOptions, error) {
	var options testOptions
	flags := flag.NewFlagSet("testgo", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.BoolVar(&options.race, "race", false, "启用竞态检测")
	flags.BoolVar(&options.short, "short", false, "运行短测试")
	flags.BoolVar(&options.verbose, "v", false, "显示测试详细输出")
	flags.StringVar(&options.tags, "tags", "", "构建标签，同时用于枚举包和编译")
	flags.StringVar(&options.run, "run", "", "要运行的测试正则表达式")
	flags.StringVar(&options.skip, "skip", "", "要跳过的测试正则表达式")
	flags.IntVar(&options.count, "count", 1, "每项测试执行次数")
	flags.DurationVar(&options.timeout, "timeout", 10*time.Minute, "单包测试超时，0 表示不限时")
	flags.Usage = func() {
		fmt.Fprintln(output, "用法: go run ./scripts/testgo [选项] [包...]（默认 ./...）")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if options.count < 1 || options.timeout < 0 {
		return options, errors.New("count 必须大于 0，timeout 不能为负数")
	}
	options.packages = flags.Args()
	if len(options.packages) == 0 {
		options.packages = []string{"./..."}
	}
	return options, nil
}

func main() {
	if err := runMain(); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runMain() error {
	options, err := parseOptions(os.Args[1:], os.Stderr)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	module, err := exec.CommandContext(ctx, "go", "env", "GOMOD").Output()
	if err != nil {
		return fmt.Errorf("读取 Go module 路径: %w", err)
	}
	modulePath := strings.TrimSpace(string(module))
	if modulePath == "" || modulePath == os.DevNull {
		return errors.New("请在仓库的 Go module 内运行")
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(modulePath))
	if err != nil {
		return err
	}
	return runTests(ctx, root, options, os.Stdout, os.Stderr)
}

type goPackage struct {
	ImportPath, Dir           string
	TestGoFiles, XTestGoFiles []string
}

func runTests(ctx context.Context, root string, options testOptions, stdout, stderr io.Writer) error {
	binaryRoot := filepath.Join(root, ".tmp", "testbin")
	unlock, err := (filelock.Locker{Dir: binaryRoot}).Lock(ctx, "runner")
	if err != nil {
		return fmt.Errorf("等待固定路径测试锁: %w", err)
	}
	defer unlock()
	run := func(directory string, output io.Writer, program string, args ...string) error {
		command := exec.CommandContext(ctx, program, args...)
		command.Dir, command.Stdout, command.Stderr = directory, output, stderr
		command.Stdin = os.Stdin
		return command.Run()
	}
	// 固定路径入口运行本机测试；拒绝误用交叉编译环境运行异平台二进制。
	var environment bytes.Buffer
	if err := run(root, &environment, "go", "env", "GOOS", "GOARCH"); err != nil {
		return err
	}
	if strings.Join(strings.Fields(environment.String()), "/") != runtime.GOOS+"/"+runtime.GOARCH {
		return errors.New("测试目标 GOOS/GOARCH 必须与当前运行平台一致")
	}
	buildFlags := []string{}
	if options.race {
		buildFlags = append(buildFlags, "-race")
	}
	if options.tags != "" {
		buildFlags = append(buildFlags, "-tags="+options.tags)
	}
	var listed bytes.Buffer
	listArgs := append([]string{"list", "-json"}, buildFlags...)
	listArgs = append(listArgs, options.packages...)
	if err := run(root, &listed, "go", listArgs...); err != nil {
		return fmt.Errorf("枚举 Go 测试包: %w", err)
	}
	decoder := json.NewDecoder(&listed)
	var packages []goPackage
	for {
		var pkg goPackage
		if err := decoder.Decode(&pkg); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return fmt.Errorf("解析 Go 测试包: %w", err)
		}
		packages = append(packages, pkg)
	}
	if len(packages) == 0 {
		return errors.New("没有匹配的 Go 包")
	}
	testFlags := []string{fmt.Sprintf("-test.count=%d", options.count), "-test.timeout=" + options.timeout.String(), "-test.paniconexit0"}
	if options.run != "" {
		testFlags = append(testFlags, "-test.run="+options.run)
	}
	if options.skip != "" {
		testFlags = append(testFlags, "-test.skip="+options.skip)
	}
	if options.short {
		testFlags = append(testFlags, "-test.short")
	}
	if options.verbose {
		testFlags = append(testFlags, "-test.v")
	}
	for _, pkg := range packages {
		relative, err := filepath.Rel(root, pkg.Dir)
		if err != nil || !filepath.IsLocal(relative) {
			return fmt.Errorf("测试包必须位于当前仓库内: %s", pkg.ImportPath)
		}
		if relative == "." {
			relative = "_root"
		}
		outputDirectory := filepath.Join(binaryRoot, relative)
		if err := os.MkdirAll(outputDirectory, 0o700); err != nil {
			return err
		}
		filename := path.Base(pkg.ImportPath) + ".test"
		if runtime.GOOS == "windows" {
			filename += ".exe"
		}
		executable := filepath.Join(outputDirectory, filename)
		fmt.Fprintf(stdout, "==> %s\n", pkg.ImportPath)
		// -o 单独使用只保存副本；必须 -c 后直接执行，才能固定运行路径。
		compileArgs := append([]string{"test", "-c"}, buildFlags...)
		compileArgs = append(compileArgs, "-o", executable, pkg.ImportPath)
		if err := run(root, stdout, "go", compileArgs...); err != nil {
			return fmt.Errorf("编译测试 %s: %w", pkg.ImportPath, err)
		}
		// 没有测试的包也必须编译与 vet，但不会生成可执行文件。
		if len(pkg.TestGoFiles)+len(pkg.XTestGoFiles) == 0 {
			continue
		}
		if err := run(pkg.Dir, stdout, executable, testFlags...); err != nil {
			return fmt.Errorf("运行测试 %s: %w", pkg.ImportPath, err)
		}
	}
	return nil
}

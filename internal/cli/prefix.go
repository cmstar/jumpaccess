package cli

import (
	"fmt"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

var prefixMatchingOnce sync.Once

func configureCommandPrefixes(root *cobra.Command) {
	// Cobra 使用进程级开关；只写入一次，避免多个 NewRoot 与执行并发时重复写入。
	prefixMatchingOnce.Do(func() { cobra.EnablePrefixMatching = true })
	// 提前建立内置命令，使 completion 也参与匹配并采用统一的错误处理。
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	configureCommandGroups(root)
	for _, child := range root.Commands() {
		if child.Name() == "help" {
			child.Args = helpTopicArgs
		}
	}
}

func helpTopicArgs(help *cobra.Command, args []string) error {
	command, remaining, err := help.Root().Find(args)
	if err != nil {
		return err
	}
	if command.HasSubCommands() {
		return commandGroupArgs(command, remaining)
	}
	return cobra.NoArgs(command, remaining)
}

func configureCommandGroups(command *cobra.Command) {
	if command.HasSubCommands() && !command.Runnable() {
		command.Args = commandGroupArgs
		// Cobra 对不可执行的命令组会在校验参数前直接显示帮助。
		command.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
	}
	for _, child := range command.Commands() {
		configureCommandGroups(child)
	}
}

func commandGroupArgs(command *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	// 唯一匹配已由 Cobra 分派；留在命令组的参数是歧义前缀或未知命令。
	prefix := args[0]
	var candidates []string
	for _, child := range command.Commands() {
		matches := strings.HasPrefix(child.Name(), prefix)
		for _, alias := range child.Aliases {
			matches = matches || strings.HasPrefix(alias, prefix)
		}
		if matches {
			candidates = append(candidates, child.Name())
		}
	}
	if len(candidates) > 1 {
		return fmt.Errorf("ambiguous command %q for %q\nAvailable commands:\n  %s", prefix, command.CommandPath(), strings.Join(candidates, "\n  "))
	}
	return fmt.Errorf("unknown command %q for %q", prefix, command.CommandPath())
}

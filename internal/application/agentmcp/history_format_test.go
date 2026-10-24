package agentmcp

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestFormatLogLineMatchesGUIStylesAndPlainLayout(t *testing.T) {
	stamp := time.Date(2026, time.October, 25, 3, 4, 5, 0, time.Local)
	for _, test := range []struct {
		severity, color, padded string
	}{
		{"DEBUG", "36", "DEBUG"},
		{"INFO", "32", "INFO "},
		{"WARN", "33", "WARN "},
		{"ERROR", "31", "ERROR"},
	} {
		t.Run(test.severity, func(t *testing.T) {
			line := LogLine{Time: stamp, Severity: test.severity, ConnectionID: "instance:c:1", Text: "调用完成", Event: "tool_call"}
			plain := FormatLogLine(line, false)
			want := "03:04:05  " + test.padded + " [instance:c:1] 调用完成"
			if plain != want {
				t.Errorf("纯文本布局 = %q，期望 %q", plain, want)
			}
			colored := FormatLogLine(line, true)
			for _, fragment := range []string{
				"\x1b[2m03:04:05\x1b[0m",
				"\x1b[" + test.color + "m" + test.padded + "\x1b[0m",
				"\x1b[34m[instance:c:1]\x1b[0m",
			} {
				if !strings.Contains(colored, fragment) {
					t.Errorf("缺少 GUI 对应样式 %q：%q", fragment, colored)
				}
			}
			if stripped := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(colored, ""); stripped != plain {
				t.Fatalf("ANSI 改变文字或对齐：%q != %q", stripped, plain)
			}
		})
	}
}

func TestFormatLogLineHighlightsOnlyCommandInputAndResetsStyle(t *testing.T) {
	line := LogLine{Time: time.Date(2026, time.October, 25, 3, 4, 5, 0, time.Local), Severity: "DEBUG", Event: "command_input", Text: `command="pwd"  cwd="/tmp"`}
	colored := FormatLogLine(line, true)
	if !strings.HasSuffix(colored, "\x1b[33m"+line.Text+"\x1b[0m") {
		t.Fatalf("命令输入应为黄色并恢复样式：%q", colored)
	}
	if plain := FormatLogLine(line, false); plain != "03:04:05  DEBUG "+line.Text {
		t.Fatalf("无连接时布局错误：%q", plain)
	}
	line.Event = "tool_conditions"
	if ordinary := FormatLogLine(line, true); strings.Contains(ordinary, "\x1b[33m") {
		t.Fatalf("普通正文不应套用命令输入样式：%q", ordinary)
	}
}

func TestFormatLogLineNeverPassesExternalTerminalControlSequences(t *testing.T) {
	line := LogLine{Severity: "INFO\x1b[2J", ConnectionID: "id\x1b]0;title\a\r", Event: "command_input", Text: "command\x1b[?1049h\n\x1b[3J"}
	plain := FormatLogLine(line, false)
	colored := FormatLogLine(line, true)
	stripped := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(colored, "")
	if stripped != plain || strings.ContainsAny(stripped, "\x1b\r\n\a") {
		t.Fatalf("外部控制字符进入日志，或 ANSI 改变内容：plain=%q，ANSI=%q", plain, colored)
	}
}

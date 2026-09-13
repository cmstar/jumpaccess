package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	connectapp "github.com/cmstar/jumpaccess/internal/application/connect"
	"github.com/cmstar/jumpaccess/internal/jumpserver"
	"github.com/spf13/cobra"
)

type prefixResourceService struct {
	fakeResourceService
	calls   int
	profile string
}

func (s *prefixResourceService) ListOrganizations(_ context.Context, profile string) ([]jumpserver.Organization, error) {
	s.calls++
	s.profile = profile
	return []jumpserver.Organization{{ID: "org-1", Name: "One"}}, nil
}

func TestCommandPrefixesExecuteOrganizationList(t *testing.T) {
	for _, input := range []string{
		"organization list", "org list", "org l", "or l", "orga li", "o l",
		"or l --profile pr", "or --profile pr l", "or l --profile=pr",
	} {
		t.Run(input, func(t *testing.T) {
			var stdout bytes.Buffer
			service := &prefixResourceService{}
			root := NewRoot(Dependencies{Resources: service, Stdout: &stdout})
			root.SetArgs(strings.Fields(input))
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if service.calls != 1 || stdout.String() != "ID     NAME\norg-1  One\n" {
				t.Fatalf("calls = %d, stdout = %q", service.calls, stdout.String())
			}
			if strings.Contains(input, "--profile") && service.profile != "pr" {
				t.Fatalf("profile = %q, want literal pr", service.profile)
			}
		})
	}
}

func TestCommandPrefixesRejectAmbiguity(t *testing.T) {
	for _, test := range []struct {
		input, parent, prefix, candidates string
	}{
		{"pr", "jumpctl", "pr", "profile\n  proxy"},
		{"a", "jumpctl", "a", "account\n  alias\n  asset\n  auth"},
		{"c", "jumpctl", "c", "completion\n  config"},
		{"auth l", "jumpctl auth", "l", "login\n  logout"},
		{"au lo", "jumpctl auth", "lo", "login\n  logout"},
		{"prof u", "jumpctl profile", "u", "update\n  use"},
		{"help au l", "jumpctl auth", "l", "login\n  logout"},
		{"help pr", "jumpctl", "pr", "profile\n  proxy"},
	} {
		t.Run(test.input, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			auth := &fakeAuthService{}
			root := NewRoot(Dependencies{Auth: auth, Stdout: &stdout, Stderr: &stderr})
			executed := false
			root.PersistentPreRun = func(*cobra.Command, []string) { executed = true }
			root.SetArgs(strings.Fields(test.input))
			err := root.Execute()
			want := "ambiguous command \"" + test.prefix + "\" for \"" + test.parent + "\"\nAvailable commands:\n  " + test.candidates
			if err == nil || err.Error() != want {
				t.Fatalf("error = %v, want %q", err, want)
			}
			// 错误交给进程入口写入 stderr，解析阶段不输出帮助或协议外数据。
			if stdout.Len() != 0 || stderr.Len() != 0 || executed {
				t.Fatalf("unexpected output: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestCommandPrefixesRejectUnknownCommandsAndFlagAbbreviations(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"nonexistent", `unknown command "nonexistent" for "jumpctl"`},
		{"auth nonexistent", `unknown command "nonexistent" for "jumpctl auth"`},
		{"org nonexistent", `unknown command "nonexistent" for "jumpctl organization"`},
		{"completion nonexistent", `unknown command "nonexistent" for "jumpctl completion"`},
		{"help auth nonexistent", `unknown command "nonexistent" for "jumpctl auth"`},
		{"OR l", `unknown command "OR" for "jumpctl"`},
		{"org l --prof pr", "unknown flag: --prof"},
	} {
		t.Run(test.input, func(t *testing.T) {
			service := &prefixResourceService{}
			var stdout bytes.Buffer
			root := NewRoot(Dependencies{Resources: service, Stdout: &stdout})
			root.SetArgs(strings.Fields(test.input))
			if err := root.Execute(); err == nil || err.Error() != test.want {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if service.calls != 0 || stdout.Len() != 0 {
				t.Fatalf("calls = %d, stdout = %q", service.calls, stdout.String())
			}
		})
	}
}

func TestCommandPrefixesPreserveConnectionArguments(t *testing.T) {
	for _, input := range []string{"s pr --profile or --account l", "prox pr --profile or --account l", "s -- --profile"} {
		t.Run(input, func(t *testing.T) {
			var stdout bytes.Buffer
			preparer := &fakeConnectionPreparer{}
			called := false
			root := NewRoot(Dependencies{
				Connect: preparer, Stdout: &stdout,
				RunSSH:   func(context.Context, connectapp.Prepared, SSHOptions) error { called = true; return nil },
				RunProxy: func(context.Context, connectapp.Prepared) error { called = true; return nil },
			})
			root.SetArgs(strings.Fields(input))
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if !called || stdout.Len() != 0 {
				t.Fatalf("called=%v stdout=%q", called, stdout.String())
			}
			if input == "s -- --profile" {
				if preparer.options.Target.Target != "--profile" {
					t.Fatalf("target = %q", preparer.options.Target.Target)
				}
			} else if preparer.options.Target.Target != "pr" || preparer.options.Target.Profile != "or" || preparer.options.Target.Account != "l" {
				t.Fatalf("arguments changed: %#v", preparer.options.Target)
			}
		})
	}
}

func TestCommandPrefixesReportAliasCandidatesOnce(t *testing.T) {
	root := NewRoot(Dependencies{})
	root.AddCommand(&cobra.Command{Use: "environment", Aliases: []string{"prod", "production"}, Run: func(*cobra.Command, []string) { t.Fatal("ambiguous command executed") }})
	root.SetArgs([]string{"pr"})
	err := root.Execute()
	want := "ambiguous command \"pr\" for \"jumpctl\"\nAvailable commands:\n  environment\n  profile\n  proxy"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestCommandPrefixesPreferExactNameAndAlias(t *testing.T) {
	for _, input := range []string{"org l", "licenses", "license", "lic"} {
		t.Run(input, func(t *testing.T) {
			var stdout bytes.Buffer
			service := &prefixResourceService{}
			root := NewRoot(Dependencies{Resources: service, Licenses: "license text", Stdout: &stdout})
			root.AddCommand(&cobra.Command{Use: "organize", Run: func(*cobra.Command, []string) { t.Fatal("wrong command") }})
			root.AddCommand(&cobra.Command{Use: "licenses-extra", Run: func(*cobra.Command, []string) { t.Fatal("wrong command") }})
			if input == "lic" {
				// 正式名称与其别名同时匹配时，只算一个候选命令。
				for _, command := range root.Commands() {
					if command.Name() == "licenses-extra" {
						root.RemoveCommand(command)
					}
				}
			}
			root.SetArgs(strings.Fields(input))
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if input == "org l" {
				if service.calls != 1 {
					t.Fatalf("calls = %d", service.calls)
				}
			} else if stdout.String() != "license text" {
				t.Fatalf("stdout = %q", stdout.String())
			}
		})
	}
}

func TestCommandPrefixesPreserveHelpAndCompletion(t *testing.T) {
	for _, input := range []string{"", "org", "or", "auth", "completion", "or l --help", "help or l", "comp ba"} {
		t.Run(input, func(t *testing.T) {
			var stdout bytes.Buffer
			root := NewRoot(Dependencies{Stdout: &stdout})
			root.SetArgs(strings.Fields(input))
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if stdout.Len() == 0 {
				t.Fatal("missing help or completion output")
			}
		})
	}
}

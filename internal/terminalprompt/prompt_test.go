package terminalprompt

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cmstar/jumpaccess/internal/jumpserver"
)

func TestSelectAccountShowsNonSecretIdentityAndReturnsChoice(t *testing.T) {
	var output bytes.Buffer
	accounts := []jumpserver.Account{
		{ID: "account-1", Name: "Root account", Username: "root"},
		{ID: "account-2", Name: "Ubuntu account", Username: "ubuntu"},
	}
	got, err := SelectAccount(strings.NewReader("2\n"), &output, accounts)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "account-2" || !strings.Contains(output.String(), "1) root") || !strings.Contains(output.String(), "2) ubuntu") {
		t.Fatalf("account = %#v, output = %q", got, output.String())
	}
}

func TestConfirmHostKeyRequiresExplicitYes(t *testing.T) {
	for input, want := range map[string]bool{"yes\n": true, "y\n": true, "\n": false, "no\n": false} {
		var output bytes.Buffer
		got, err := ConfirmHostKey(strings.NewReader(input), &output, "gateway:22", "SHA256:fingerprint")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("ConfirmHostKey(%q) = %v, want %v", input, got, want)
		}
		if !strings.Contains(output.String(), "SHA256:fingerprint") {
			t.Fatalf("output = %q", output.String())
		}
	}
}

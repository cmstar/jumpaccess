package systemopen

import "testing"

func TestCommandForUsesPlatformFileLauncher(t *testing.T) {
	tests := []struct {
		goos     string
		path     string
		wantName string
		wantArgs []string
	}{
		{
			goos:     "windows",
			path:     `C:\Users\alice\AppData\Local\JumpAccess\config.toml`,
			wantName: "rundll32.exe",
			wantArgs: []string{"url.dll,FileProtocolHandler", `C:\Users\alice\AppData\Local\JumpAccess\config.toml`},
		},
		{
			goos:     "darwin",
			path:     "/Users/alice/Library/Application Support/JumpAccess/config.toml",
			wantName: "open",
			wantArgs: []string{"/Users/alice/Library/Application Support/JumpAccess/config.toml"},
		},
	}

	for _, test := range tests {
		t.Run(test.goos, func(t *testing.T) {
			name, args, err := CommandFor(test.goos, test.path)
			if err != nil {
				t.Fatalf("CommandFor returned error: %v", err)
			}
			if name != test.wantName || len(args) != len(test.wantArgs) {
				t.Fatalf("CommandFor = %q %#v, want %q %#v", name, args, test.wantName, test.wantArgs)
			}
			for index := range args {
				if args[index] != test.wantArgs[index] {
					t.Fatalf("args[%d] = %q, want %q", index, args[index], test.wantArgs[index])
				}
			}
		})
	}
}

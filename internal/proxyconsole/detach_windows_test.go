//go:build windows

package proxyconsole

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDetachPrivateProxyDetachesRedirectedPrivateConsole(t *testing.T) {
	api := &fakeConsoleAPI{processCount: 1}

	if !detachPrivateProxy(api, [2]windows.Handle{1, 2}) {
		t.Fatal("detachPrivateProxy = false, want true")
	}
	if api.freeCalls != 1 {
		t.Fatalf("FreeConsole calls = %d, want 1", api.freeCalls)
	}
}

func TestDetachPrivateProxyRequiresRedirectedTransportHandles(t *testing.T) {
	for index, name := range []string{"stdin", "stdout"} {
		consoleHandle := windows.Handle(index + 1)
		t.Run(name, func(t *testing.T) {
			api := &fakeConsoleAPI{
				consoleHandles: map[windows.Handle]bool{},
				processCount:   1,
			}
			api.consoleHandles[consoleHandle] = true

			if detachPrivateProxy(api, [2]windows.Handle{1, 2}) {
				t.Fatal("detachPrivateProxy = true, want false")
			}
			if api.freeCalls != 0 {
				t.Fatalf("FreeConsole calls = %d, want 0", api.freeCalls)
			}
		})
	}
}

func TestDetachPrivateProxyLeavesSharedConsoleAttached(t *testing.T) {
	api := &fakeConsoleAPI{processCount: 2}

	if detachPrivateProxy(api, [2]windows.Handle{1, 2}) {
		t.Fatal("detachPrivateProxy = true, want false")
	}
	if api.freeCalls != 0 {
		t.Fatalf("FreeConsole calls = %d, want 0", api.freeCalls)
	}
}

func TestDetachPrivateProxyFailsClosedWhenInspectionFails(t *testing.T) {
	tests := []struct {
		name string
		api  *fakeConsoleAPI
	}{
		{
			name: "standard handle",
			api: &fakeConsoleAPI{
				inspectErr:   errors.New("inspect handle"),
				processCount: 1,
			},
		},
		{
			name: "process list",
			api: &fakeConsoleAPI{
				processCountErr: errors.New("inspect console"),
			},
		},
		{
			name: "detach",
			api: &fakeConsoleAPI{
				processCount: 1,
				freeErr:      errors.New("detach console"),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if detachPrivateProxy(test.api, [2]windows.Handle{1, 2}) {
				t.Fatal("detachPrivateProxy = true, want false")
			}
		})
	}
}

type fakeConsoleAPI struct {
	consoleHandles  map[windows.Handle]bool
	inspectErr      error
	processCount    uint32
	processCountErr error
	freeErr         error
	freeCalls       int
}

func (f *fakeConsoleAPI) ProcessCount() (uint32, error) {
	return f.processCount, f.processCountErr
}

func (f *fakeConsoleAPI) IsConsole(handle windows.Handle) (bool, error) {
	if f.inspectErr != nil {
		return false, f.inspectErr
	}
	return f.consoleHandles[handle], nil
}

func (f *fakeConsoleAPI) Free() error {
	f.freeCalls++
	return f.freeErr
}

//go:build windows

package systemfont

import "testing"

func TestMonospacedFamiliesFindsInstalledWindowsFonts(t *testing.T) {
	families, err := MonospacedFamilies()
	if err != nil {
		t.Fatalf("MonospacedFamilies() error = %v", err)
	}
	if len(families) == 0 {
		t.Fatal("MonospacedFamilies() returned no installed fonts")
	}
}

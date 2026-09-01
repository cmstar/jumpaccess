package systemfont

import (
	"reflect"
	"testing"
)

func TestNormalizeFamiliesRemovesDuplicatesAndVerticalAliases(t *testing.T) {
	got := normalizeFamilies([]string{
		"JetBrains Mono",
		" @Vertical Font ",
		"Cascadia Mono",
		"jetbrains mono",
		"",
		" Menlo ",
	})
	want := []string{"Cascadia Mono", "JetBrains Mono", "Menlo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeFamilies() = %#v, want %#v", got, want)
	}
}

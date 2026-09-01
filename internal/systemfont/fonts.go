package systemfont

import (
	"sort"
	"strings"
)

// MonospacedFamilies returns the visible fixed-pitch font families reported by
// the current desktop platform. An empty result lets the GUI retain its manual
// font-family input and generic monospace fallback.
func MonospacedFamilies() ([]string, error) {
	families, err := platformMonospacedFamilies()
	if err != nil {
		return nil, err
	}
	return normalizeFamilies(families), nil
}

func normalizeFamilies(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || strings.HasPrefix(value, "@") {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result
}

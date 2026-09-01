//go:build !windows && (!darwin || !cgo)

package systemfont

func platformMonospacedFamilies() ([]string, error) {
	return nil, nil
}

//go:build windows

package ghavm

import "os"

// writeFile writes data to a file, creating it if necessary. Note that this
// is not an atomic operation on Windows.
func writeFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

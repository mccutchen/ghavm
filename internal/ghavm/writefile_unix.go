//go:build !windows

package ghavm

import (
	"os"

	renameio "github.com/google/renameio/v2"
)

// writeFile writes data to a file, creating it if necessary or replacing it
// atomically otherwise (on macOS/Linux).
func writeFile(path string, data []byte, perm os.FileMode) error {
	return renameio.WriteFile(path, data, perm)
}

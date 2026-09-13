package edit

import "github.com/julianbei/jade/internal/writes"

// writeFile is the edit package's only way to change a file (internal/writes).
func writeFile(path string, data []byte) error {
	return writes.File(path, data)
}

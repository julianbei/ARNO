package code

import "github.com/julianbei/arno/internal/writes"

// writeFile and writeFiles are the index's only way to change a file: atomic,
// and all or nothing across files (internal/writes).
func writeFile(path string, data []byte) error {
	return writes.File(path, data)
}

// removeFile deletes a file through the write path, so a checkpoint records
// what it held first.
func removeFile(path string) error {
	return writes.Remove(path)
}

func writeFiles(paths []string, bodies [][]byte) error {
	changes := make([]writes.Change, len(paths))
	for i := range paths {
		changes[i] = writes.Change{Path: paths[i], Data: bodies[i]}
	}
	return writes.Files(changes)
}

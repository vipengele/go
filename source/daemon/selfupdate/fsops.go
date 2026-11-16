package selfupdate

import (
	"io/fs"
	"os"
)

// fsOps are the file system calls a rollback and a Machine make on the binary, its backup and the
// replaced image.
type fsOps struct {
	rename func(oldpath, newpath string) error
	remove func(name string) error
	lstat  func(name string) (fs.FileInfo, error)
}

func osFSOps() fsOps {
	return fsOps{rename: os.Rename, remove: os.Remove, lstat: os.Lstat}
}

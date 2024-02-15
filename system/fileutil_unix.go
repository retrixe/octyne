//go:build !windows

package system

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"
)

// IsFileLocked checks if a file is locked.
func IsFileLocked(_ error) bool {
	// We could look for advisory locks with `lsof` but this is generally not what Unix users expect...
	return false
}

// CanDeleteFolder checks if a folder can be deleted, checking its children recursively.
func CanDeleteFolder(path string) (int, string) {
	groups, err := os.Getgroups()
	if err == nil {
		user := os.Geteuid()
		err = filepath.Walk(path, func(_ string, stat fs.FileInfo, err error) error {
			if err != nil {
				return err
			} else if stat.IsDir() {
				sysStat, ok := stat.Sys().(*syscall.Stat_t)
				if !ok || !((int(sysStat.Uid) == user && stat.Mode().Perm()&0o200 != 0) ||
					(slices.Contains(groups, int(sysStat.Gid)) && stat.Mode().Perm()&0o020 != 0) ||
					stat.Mode().Perm()&0o002 != 0) {
					return os.ErrPermission
				}
			}
			return nil
		})
		if err != nil && os.IsNotExist(err) {
			return 400, "The file specified in path does not exist!"
		} else if err != nil && os.IsPermission(err) {
			return 403, "Insufficient permissions to delete the file/folder!"
		} else if err != nil {
			return 500, "Internal Server Error! " + err.Error()
		}
	} else {
		return 500, "Internal Server Error! " + err.Error()
	}
	return 0, ""
}

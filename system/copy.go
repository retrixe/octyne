package system

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Copy copies a file, symlink or folder from one place to another.
func Copy(fromMode fs.FileMode, path string, dest string) error {
	switch fromMode & os.ModeType {
	case os.ModeDir:
		if err := os.MkdirAll(dest, 0777); err != nil {
			return err
		}
		return CopyDirectory(path, dest)
	case os.ModeSymlink:
		return CopySymLink(path, dest)
	default:
		return CopyFile(path, dest)
	}
}

// CopyDirectory copies a folder from one place to another.
// Requires the destination path to exist already.
func CopyDirectory(path string, dest string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		sourcePath := filepath.Join(path, entry.Name())
		destPath := filepath.Join(dest, entry.Name())

		/* Not Windows compatible: stat, ok := entry.Sys().(*syscall.Stat_t)
		if !ok {return fmt.Errorf("failed to get raw syscall.Stat_t data for '%s'", sourcePath)} */

		if err := Copy(entry.Type(), sourcePath, destPath); err != nil {
			return err
		}

		// if err := os.Lchown(destPath, int(stat.Uid), int(stat.Gid)); err != nil {return err}

		isSymlink := entry.Type()&os.ModeSymlink != 0
		if !isSymlink {
			if stat, err := entry.Info(); err == nil {
				if err := os.Chmod(destPath, stat.Mode()); err != nil {
					return err
				}
			} else {
				return err
			}
		}
	}
	return nil
}

// CopySymlink copies a symlink from one place to another.
func CopySymLink(source, dest string) error {
	link, err := os.Readlink(source)
	if err != nil {
		return err
	}
	return os.Symlink(link, dest)
}

// CopyFile copies a file from one place to another.
func CopyFile(path string, dest string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() {
		return fmt.Errorf("%s is not a proper file", path)
	}

	// Open path.
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Create dest.
	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer destFile.Close()

	// Copy from file to copy.
	_, err = io.Copy(destFile, file)
	if err != nil {
		return err
	}
	return nil
}

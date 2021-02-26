package system

import (
	"fmt"
	"io"
	"os"
)

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

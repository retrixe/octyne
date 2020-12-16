package system

import (
	"fmt"
	"io"
	"os"
)

// CopyFile ... Copy a file from one place to another.
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
	defer file.Close()
	if err != nil {
		return err
	}

	// Create dest.
	copy, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer copy.Close()

	// Copy from file to copy.
	_, err = io.Copy(copy, file)
	if err != nil {
		return err
	}
	return nil
}

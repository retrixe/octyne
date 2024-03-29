package system

import (
	"archive/zip"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func joinPath(elem ...string) string {
	return filepath.FromSlash(path.Join(elem...))
}

// UnzipFile unzips a file to a location.
func UnzipFile(zipFile string, location string) error {
	r, err := zip.OpenReader(zipFile)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		fpath := filepath.Join(location, f.Name) // skipcq GSC-G305

		// Check for ZipSlip. More Info: http://bit.ly/2MsjAWE
		if !strings.HasPrefix(fpath, filepath.Clean(location)+string(os.PathSeparator)) {
			continue // "%s: illegal file path"
		}
		// Create folders.
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}
		// Create parent folder of file if needed.
		err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm)
		if err != nil {
			return err
		}
		// Open target file.
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		// Open file in zip.
		rc, err := f.Open()
		if err != nil {
			return err
		}
		// Copy file from zip to disk.
		_, err = io.CopyN(outFile, rc, int64(f.UncompressedSize64))
		if err != nil {
			return err
		}
		outFile.Close()
		rc.Close()
	}
	return nil
}

// AddFileToZip adds a file or folder to a zip.Writer using Deflate.
func AddFileToZip(archive *zip.Writer, dir string, file string, compress bool) error {
	fileToZip, err := os.Open(joinPath(dir, file))
	if err != nil {
		return err
	}
	defer fileToZip.Close()
	info, err := fileToZip.Stat()
	if err != nil {
		return err
	}
	// If the file is a folder, recursively add its contents to the zip.
	if info.IsDir() {
		files, err := os.ReadDir(joinPath(dir, file))
		if err != nil {
			return err
		}
		for _, child := range files {
			err = AddFileToZip(archive, dir, joinPath(file, child.Name()), compress)
			if err != nil {
				return err
			}
		}
		return nil
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	// Using FileInfoHeader() above only uses the basename of the file. If we want
	// to preserve the folder structure we can overwrite this with the full path.
	header.Name = file
	// Change to deflate to gain better compression.
	if compress {
		header.Method = zip.Deflate
	}
	writer, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, fileToZip)
	if err != nil {
		return err
	}
	return nil
}

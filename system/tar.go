package system

import (
	"archive/tar"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ExtractTarFile extracts a tar archive to a location.
// It supports gzip and bzip2 natively, and xz/zstd if installed on your system on the CLI.
func ExtractTarFile(tarFile string, location string) error {
	file, err := os.Open(tarFile)
	if err != nil {
		return err
	}
	var reader io.Reader = file
	if strings.HasSuffix(tarFile, ".gz") || strings.HasSuffix(tarFile, ".tgz") {
		reader, err = gzip.NewReader(file)
		if err != nil {
			return err
		}
	} else if strings.HasSuffix(tarFile, ".bz") || strings.HasSuffix(tarFile, ".tbz") ||
		strings.HasSuffix(tarFile, ".bz2") || strings.HasSuffix(tarFile, ".tbz2") {
		reader = bzip2.NewReader(file)
	} else if strings.HasSuffix(tarFile, ".xz") || strings.HasSuffix(tarFile, ".txz") {
		reader = NativeCompressionReader(file, "xz")
	} else if strings.HasSuffix(tarFile, ".zst") || strings.HasSuffix(tarFile, ".tzst") {
		reader = NativeCompressionReader(file, "zstd")
	}
	archive := tar.NewReader(reader)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return err
		}

		fpath := filepath.Join(location, header.Name) // skipcq GSC-G305

		// Check for ZipSlip. More Info: http://bit.ly/2MsjAWE
		if !strings.HasPrefix(fpath, filepath.Clean(location)+string(os.PathSeparator)) {
			continue // "%s: illegal file path"
		}
		// Create folders.
		if header.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}
		// Create parent folder of file if needed.
		err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm)
		if err != nil {
			return err
		}
		// Open target file.
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, header.FileInfo().Mode())
		if err != nil {
			return err
		}
		// Copy file from tar to disk.
		_, err = io.CopyN(outFile, archive, header.Size)
		if err != nil {
			return err
		}
		outFile.Close()
	}
	return nil
}

// AddFileToTar adds a file/folder to a tar.Writer.
func AddFileToTar(archive *tar.Writer, dir string, file string) error {
	fileToTar, err := os.Open(joinPath(dir, file))
	if err != nil {
		return err
	}
	defer fileToTar.Close()
	info, err := fileToTar.Stat()
	if err != nil {
		return err
	}
	// If the file is a folder, recursively add its contents to the tar.
	link := ""
	if info.IsDir() {
		files, err := os.ReadDir(joinPath(dir, file))
		if err != nil {
			return err
		}
		for _, child := range files {
			err = AddFileToTar(archive, dir, joinPath(file, child.Name()))
			if err != nil {
				return err
			}
		}
		return nil
	} else if info.Mode()&os.ModeSymlink != 0 {
		link, err = filepath.EvalSymlinks(joinPath(dir, file))
		if err != nil {
			return err
		}
	}
	header, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return err
	}
	// Using FileInfoHeader() above only uses the basename of the file. If we want
	// to preserve the folder structure we can overwrite this with the full path.
	header.Name = file
	err = archive.WriteHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(archive, fileToTar)
	if err != nil {
		return err
	}
	return nil
}

// NativeCompressionReader can use xz/zstd installed in your system PATH for decompression.
func NativeCompressionReader(r io.Reader, algorithm string) io.ReadCloser {
	rpipe, wpipe := io.Pipe()
	cmd := exec.Command(algorithm, "--decompress", "--stdout")
	cmd.Stdin = r
	cmd.Stdout = wpipe
	go func() {
		err := cmd.Run()
		wpipe.CloseWithError(err)
	}()
	return rpipe
}

// NativeCompressionWriter can use xz/zstd installed in your system PATH for compression.
// It uses zstd long distance mode for better compression.
func NativeCompressionWriter(w io.Writer, algorithm string) io.WriteCloser {
	rpipe, wpipe := io.Pipe()
	cmd := exec.Command(algorithm, "--stdout")
	if algorithm == "zstd" {
		cmd.Args = append(cmd.Args, "--long")
	}
	cmd.Stdin = rpipe
	cmd.Stdout = w
	go func() {
		err := cmd.Run()
		wpipe.CloseWithError(err)
	}()
	return wpipe
}

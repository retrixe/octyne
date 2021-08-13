package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gorilla/mux"
	"github.com/retrixe/octyne/system"
)

// joinPath joins any number of path elements into a single path, adding a separating slash if necessary.
func joinPath(elem ...string) string {
	return filepath.FromSlash(path.Join(elem...))
}

func (connector *Connector) registerFileRoutes() {
	// GET /server/{id}/files?path=path
	type serverFilesResponse struct {
		Name         string `json:"name"`
		Size         int64  `json:"size"`
		MimeType     string `json:"mimeType"`
		Folder       bool   `json:"folder"`
		LastModified int64  `json:"lastModified"`
	}
	connector.Router.HandleFunc("/server/{id}/files", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		if !connector.Validate(w, r) {
			return
		}
		// Get the server being accessed.
		id := mux.Vars(r)["id"]
		server, err := connector.Processes.Get(id)
		// In case the server doesn't exist.
		if !err {
			http.Error(w, "{\"error\":\"This server does not exist!\"}", http.StatusNotFound)
			return
		}
		// Check if folder is in the server directory or not.
		folderPath := joinPath(server.Directory, r.URL.Query().Get("path"))
		if !strings.HasPrefix(folderPath, path.Clean(server.Directory)) {
			http.Error(w, "{\"error\":\"The folder requested is outside the server!\"}", http.StatusForbidden)
			return
		}
		// Get list of files and folders in the directory.
		folder, err1 := os.Open(folderPath)
		if err1 != nil {
			http.Error(w, "{\"error\":\"This folder does not exist!\"}", http.StatusNotFound)
			return
		}
		defer folder.Close()
		contents, err2 := folder.Readdir(-1)
		if err2 != nil {
			http.Error(w, "{\"error\":\"This is not a folder!\"}", http.StatusBadRequest)
			return
		}
		// Send the response.
		toSend := make(map[string]([]serverFilesResponse))
		toSend["contents"] = make([]serverFilesResponse, 0, len(contents))
		for _, value := range contents {
			// Determine the MIME-Type of the file.
			mimeType := ""
			if value.Mode()&os.ModeSymlink != 0 {
				mimeType = "inode/symlink"
			} else if !value.IsDir() {
				var length int64 = 512
				if value.Size() < 512 {
					length = value.Size()
				}
				buffer := make([]byte, length)
				path := joinPath(server.Directory, r.URL.Query().Get("path"), value.Name())
				file, err := os.Open(path)
				if err == nil {
					file.Read(buffer) // skipcq GSC-G104
					mimeType = http.DetectContentType(buffer)
					file.Close() // skipcq GSC-G104
				}
			}
			toSend["contents"] = append(toSend["contents"], serverFilesResponse{
				Folder:       value.IsDir() || mimeType == "inode/symlink",
				Name:         value.Name(),
				Size:         value.Size(),
				LastModified: value.ModTime().Unix(),
				MimeType:     mimeType,
			})
		}
		json.NewEncoder(w).Encode(toSend) // skipcq GSC-G104
	})

	// GET /server/{id}/file?path=path&ticket=ticket
	// DOWNLOAD /server/{id}/file?path=path
	// POST /server/{id}/file?path=path
	// DELETE /server/{id}/file?path=path
	// PATCH /server/{id}/file?path=path
	connector.Router.HandleFunc("/server/{id}/file", func(w http.ResponseWriter, r *http.Request) {
		if t, e := connector.GetTicket(r.URL.Query().Get("ticket")); e && r.Method == "GET" && t.IPAddr == GetIP(r) {
			connector.DeleteTicket(r.URL.Query().Get("ticket"))
		} else if !connector.Validate(w, r) {
			return
		}
		// Get the server being accessed.
		id := mux.Vars(r)["id"]
		server, err := connector.Processes.Get(id)
		// In case the server doesn't exist.
		if !err {
			http.Error(w, "{\"error\":\"This server does not exist!\"}", http.StatusNotFound)
			return
		}
		// Check if path is in the server directory or not.
		filePath := joinPath(server.Directory, r.URL.Query().Get("path"))
		if (r.Method == "GET" || r.Method == "POST" || r.Method == "DELETE") &&
			!strings.HasPrefix(filePath, path.Clean(server.Directory)) {
			http.Error(w, "{\"error\":\"The file requested is outside the server!\"}", http.StatusForbidden)
			return
		}
		if r.Method == "GET" {
			// Get list of files and folders in the directory.
			file, err := os.Open(filePath)
			stat, err1 := file.Stat()
			if err != nil || err1 != nil {
				http.Error(w, "{\"error\":\"This file does not exist!\"}", http.StatusNotFound)
				return
			}
			// Send the response.
			buffer := make([]byte, 512)
			file.Read(buffer) // skipcq GSC-G104
			file.Close()      // skipcq GSC-G104
			w.Header().Set("Content-Disposition", "attachment; filename="+stat.Name())
			w.Header().Set("Content-Type", http.DetectContentType(buffer))
			w.Header().Set("Content-Length", fmt.Sprint(stat.Size()))
			file, _ = os.Open(filePath)
			defer file.Close()
			io.Copy(w, file)
		} else if r.Method == "DELETE" {
			// Check if the file exists.
			if filePath == "/" {
				http.Error(w, "{\"error\":\"This operation is dangerous and has been forbidden!\"}", http.StatusForbidden)
				return
			}
			_, err := os.Stat(filePath)
			if err != nil || os.IsNotExist(err) {
				http.Error(w, "{\"error\":\"This file does not exist!\"}", http.StatusNotFound)
				return
			}
			err = os.RemoveAll(filePath)
			if err != nil && err.(*os.PathError).Err != nil && err.(*os.PathError).Err.Error() ==
				"The process cannot access the file because it is being used by another process." {
				http.Error(w, "{\"error\":\""+err.(*os.PathError).Err.Error()+"\"}", http.StatusConflict)
				return
			} else if err != nil {
				http.Error(w, "{\"error\":\"Internal Server Error!\"}", http.StatusInternalServerError)
				return
			}
			fmt.Fprint(w, "{\"success\":true}")
		} else if r.Method == "POST" {
			// Parse our multipart form, 5120 << 20 specifies a maximum upload of a 5 GB file.
			err := r.ParseMultipartForm(5120 << 20)
			if err != nil {
				http.Error(w, "{\"error\":\"Invalid form sent!\"}", http.StatusBadRequest)
				return
			}
			// FormFile returns the first file for the given key `upload`
			file, meta, err := r.FormFile("upload")
			if err != nil {
				http.Error(w, "{\"error\":\"Invalid form sent!\"}", http.StatusBadRequest)
				return
			}
			defer file.Close()
			// read the file.
			toWrite, err := os.Create(joinPath(server.Directory, r.URL.Query().Get("path"), meta.Filename))
			stat, err1 := toWrite.Stat()
			if err != nil {
				http.Error(w, "{\"error\":\"Internal Server Error!\"}", http.StatusInternalServerError)
				return
			} else if err1 == nil && stat.IsDir() {
				http.Error(w, "{\"error\":\"This is a folder!\"}", http.StatusBadRequest)
				return
			}
			defer toWrite.Close()
			// write this byte array to our file
			io.Copy(toWrite, file)
			fmt.Fprintf(w, "{\"success\":true}")
		} else if r.Method == "PATCH" {
			// Get the request body to check the operation.
			var body bytes.Buffer
			_, err := body.ReadFrom(r.Body)
			if err != nil {
				http.Error(w, "{\"error\":\"Failed to read body!\"}", http.StatusBadRequest)
				return
			}
			operation := strings.Split(body.String(), "\n")
			// Possible operations: mv, cp
			if operation[0] == "mv" || operation[0] == "cp" {
				if len(operation) != 3 {
					http.Error(w, "{\"error\":\""+operation[0]+" operation requires two arguments!\"}", http.StatusMethodNotAllowed)
					return
				}
				// Check if original file exists.
				// TODO: Needs better sanitation.
				oldpath := joinPath(server.Directory, operation[1])
				newpath := joinPath(server.Directory, operation[2])
				if !strings.HasPrefix(oldpath, path.Clean(server.Directory)) ||
					!strings.HasPrefix(newpath, path.Clean(server.Directory)) {
					http.Error(w, "{\"error\":\"The files requested are outside the server!\"}", http.StatusForbidden)
					return
				}
				file, err := os.Open(oldpath)
				fileStat, err1 := file.Stat()
				if err != nil || os.IsNotExist(err1) {
					http.Error(w, "{\"error\":\"This file does not exist!\"}", http.StatusNotFound)
					return
				}
				file.Close() // skipcq GSC-G104
				// Check if destination file exists.
				file, err = os.Open(newpath)
				stat, err1 := file.Stat()
				if (err == nil || os.IsExist(err1)) && stat != nil && !stat.IsDir() {
					http.Error(w, "{\"error\":\"This file already exists!\"}", http.StatusMethodNotAllowed)
					defer file.Close()
					return
				} else if stat != nil && stat.IsDir() {
					newpath = joinPath(newpath, path.Base(oldpath))
				}
				// Move file if operation is mv.
				if operation[0] == "mv" {
					err := os.Rename(oldpath, newpath)
					if err != nil && err.(*os.LinkError).Err != nil && err.(*os.LinkError).Err.Error() ==
						"The process cannot access the file because it is being used by another process." {
						http.Error(w, "{\"error\":\""+err.(*os.LinkError).Err.Error()+"\"}", http.StatusConflict)
						return
					} else if err != nil {
						http.Error(w, "{\"error\":\"Internal Server Error!\"}", http.StatusInternalServerError)
						return
					}
					fmt.Fprintf(w, "{\"success\":true}")
				} else {
					var failed bool
					if fileStat.IsDir() { // TODO: Not robust, write a better Copy implementation.
						var cmd *exec.Cmd
						if runtime.GOOS == "windows" {
							cmd = exec.Command("robocopy", oldpath, newpath, "/E")
						} else {
							cmd = exec.Command("cp", "-r", oldpath, newpath)
						}
						err := cmd.Run()
						failed = err != nil && (runtime.GOOS != "windows" || cmd.ProcessState.ExitCode() != 1)
					} else {
						err := system.CopyFile(oldpath, newpath)
						failed = err != nil
					}
					if failed {
						http.Error(w, "{\"error\":\"Internal Server Error!\"}", http.StatusInternalServerError)
						return
					}
					fmt.Fprintf(w, "{\"success\":true}")
				}
			} else {
				http.Error(w, "{\"error\":\"Invalid operation! Operations available: mv,cp\"}", http.StatusMethodNotAllowed)
			}
		} else {
			http.Error(w, "{\"error\":\"Only GET, POST, PATCH and DELETE are allowed!\"}", http.StatusMethodNotAllowed)
		}
	})

	// POST /server/{id}/folder?path=path
	connector.Router.HandleFunc("/server/{id}/folder", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		if !connector.Validate(w, r) {
			return
		}
		// Get the server being accessed.
		id := mux.Vars(r)["id"]
		server, err := connector.Processes.Get(id)
		// In case the server doesn't exist.
		if !err {
			http.Error(w, "{\"error\":\"This server does not exist!\"}", http.StatusNotFound)
			return
		}
		if r.Method == "POST" {
			// Check if the folder already exists.
			file := joinPath(server.Directory, r.URL.Query().Get("path"))
			// Check if folder is in the server directory or not.
			if !strings.HasPrefix(file, path.Clean(server.Directory)) {
				http.Error(w, "{\"error\":\"The folder requested is outside the server!\"}", http.StatusForbidden)
				return
			}
			_, err := os.Stat(file)
			if !os.IsNotExist(err) {
				http.Error(w, "{\"error\":\"This folder already exists!\"}", http.StatusBadRequest)
				return
			}
			// Create the folder.
			err = os.Mkdir(file, os.ModePerm)
			if err != nil {
				http.Error(w, "{\"error\":\"Internal Server Error!\"}", http.StatusInternalServerError)
				return
			}
			fmt.Fprintf(w, "{\"success\":true}")
		} else {
			http.Error(w, "{\"error\":\"Only POST is allowed!\"}", http.StatusMethodNotAllowed)
		}
	})

	// POST /server/{id}/compress?path=path&compress=true/false (compress is optional, default: true)
	connector.Router.HandleFunc("/server/{id}/compress", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		if !connector.Validate(w, r) {
			return
		}
		// Get the server being accessed.
		id := mux.Vars(r)["id"]
		server, err := connector.Processes.Get(id)
		// In case the server doesn't exist.
		if !err {
			http.Error(w, "{\"error\":\"This server does not exist!\"}", http.StatusNotFound)
			return
		}
		if r.Method == "POST" {
			// Get the body.
			var buffer bytes.Buffer
			_, err := buffer.ReadFrom(r.Body)
			if err != nil {
				http.Error(w, "{\"error\":\"Failed to read body!\"}", http.StatusBadRequest)
				return
			}
			// Decode the array body and send it to files.
			var files []string
			err = json.Unmarshal(buffer.Bytes(), &files)
			if err != nil {
				http.Error(w, "{\"error\":\"Invalid JSON body!\"}", http.StatusBadRequest)
				return
			}
			// Validate every path.
			for _, file := range files {
				filepath := joinPath(server.Directory, file)
				if !strings.HasPrefix(filepath, path.Clean(server.Directory)) {
					http.Error(w, "{\"error\":\"One of the paths provided is outside the server directory!\"}", http.StatusForbidden)
					return
				} else if _, err := os.Stat(filepath); err != nil {
					if os.IsNotExist(err) {
						http.Error(w, "{\"error\":\"The file "+file+" does not exist!\"}", http.StatusBadRequest)
					} else {
						http.Error(w, "{\"error\":\"Internal Server Error!\"}", http.StatusInternalServerError)
					}
					return
				}
			}
			// Check if a file exists at the location of the ZIP file.
			zipPath := joinPath(server.Directory, r.URL.Query().Get("path"))
			if !strings.HasPrefix(zipPath, path.Clean(server.Directory)) {
				http.Error(w, "{\"error\":\"The requested ZIP file is outside the server directory!\"}", http.StatusForbidden)
				return
			}
			_, exists := os.Stat(zipPath)
			if !os.IsNotExist(exists) {
				http.Error(w, "{\"error\":\"A file already exists at the path of requested ZIP!\"}", http.StatusBadRequest)
				return
			}

			// Begin compressing a ZIP.
			zipFile, err := os.Create(zipPath)
			if err != nil {
				http.Error(w, "{\"error\":\"Internal Server Error!\"}", http.StatusInternalServerError)
				return
			}
			defer zipFile.Close()
			archive := zip.NewWriter(zipFile)
			defer archive.Close()
			// Archive stuff inside.
			for _, file := range files {
				err := system.AddFileToZip(archive, server.Directory, file, r.Header.Get("compress") != "false")
				if err != nil {
					http.Error(w, "{\"error\":\"Internal Server Error!\"}", http.StatusInternalServerError)
					return
				}
			}
			fmt.Fprintf(w, "{\"success\":true}")
		} else {
			http.Error(w, "{\"error\":\"Only POST is allowed!\"}", http.StatusMethodNotAllowed)
		}
	})

	// POST /server/{id}/decompress?path=path
	connector.Router.HandleFunc("/server/{id}/decompress", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		if !connector.Validate(w, r) {
			return
		}
		// Get the server being accessed.
		id := mux.Vars(r)["id"]
		server, err := connector.Processes.Get(id)
		// In case the server doesn't exist.
		if !err {
			http.Error(w, "{\"error\":\"This server does not exist!\"}", http.StatusNotFound)
			return
		}
		directory := path.Clean(server.Directory)
		if r.Method == "POST" {
			// Check if the ZIP file exists.
			zipPath := joinPath(directory, r.URL.Query().Get("path"))
			if !strings.HasPrefix(zipPath, path.Clean(server.Directory)) {
				http.Error(w, "{\"error\":\"The ZIP file is outside the server directory!\"}", http.StatusForbidden)
				return
			}
			_, exists := os.Stat(zipPath)
			if os.IsNotExist(exists) {
				http.Error(w, "{\"error\":\"The requested ZIP does not exist!\"}", http.StatusBadRequest)
				return
			} else if exists != nil {
				http.Error(w, "{\"error\":\"Internal Server Error!\"}", http.StatusInternalServerError)
				return
			}
			// Check if there is a file/folder at the destination.
			var body bytes.Buffer
			_, err := body.ReadFrom(r.Body)
			if err != nil {
				http.Error(w, "{\"error\":\"Failed to read body!\"}", http.StatusBadRequest)
				return
			}
			unpackPath := joinPath(server.Directory, body.String())
			if !strings.HasPrefix(unpackPath, path.Clean(server.Directory)) {
				http.Error(w, "{\"error\":\"The ZIP file is outside the server directory!\"}", http.StatusForbidden)
				return
			}
			stat, exists := os.Stat(unpackPath)
			if os.IsNotExist(exists) {
				err := os.Mkdir(unpackPath, os.ModePerm)
				if err != nil {
					http.Error(w, "{\"error\":\"Internal Server Error!\"}", http.StatusInternalServerError)
					return
				}
			} else if exists != nil {
				http.Error(w, "{\"error\":\"Internal Server Error!\"}", http.StatusInternalServerError)
				return
			} else if !stat.IsDir() {
				http.Error(w, "{\"error\":\"There is a file at the requested unpack destination!\"}", http.StatusBadRequest)
				return
			}
			// Decompress the ZIP.
			err = system.UnzipFile(zipPath, unpackPath)
			if err != nil {
				http.Error(w, "{\"error\":\"An error occurred while unzipping!\"}", http.StatusInternalServerError)
				return
			}
			fmt.Fprintf(w, "{\"success\":true}")
		} else {
			http.Error(w, "{\"error\":\"Only POST is allowed!\"}", http.StatusMethodNotAllowed)
		}
	})
}

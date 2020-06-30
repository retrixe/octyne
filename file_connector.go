package main

import (
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
		server, err := connector.Processes[id]
		// In case the server doesn't exist.
		if !err {
			http.Error(w, "{\"error\":\"This server does not exist!\"}", 404)
			return
		}
		// Get list of files and folders in the directory.
		folder, err1 := os.Open(joinPath(server.Directory, r.URL.Query().Get("path")))
		defer folder.Close()
		if err1 != nil {
			http.Error(w, "{\"error\":\"This folder does not exist!\"}", 404)
			return
		}
		contents, err2 := folder.Readdir(-1)
		if err2 != nil {
			http.Error(w, "{\"error\":\"This is not a folder!\"}", 400)
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
					file.Read(buffer)
					mimeType = http.DetectContentType(buffer)
					file.Close()
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
		json.NewEncoder(w).Encode(toSend)
	})

	// GET /server/{id}/file?path=path
	// DOWNLOAD /server/{id}/file?path=path
	// POST /server/{id}/file?path=path
	// DELETE /server/{id}/file?path=path
	// PATCH /server/{id}/file?path=path
	connector.Router.HandleFunc("/server/{id}/file", func(w http.ResponseWriter, r *http.Request) {
		if !connector.Validate(w, r) {
			return
		}
		// Get the server being accessed.
		id := mux.Vars(r)["id"]
		server, err := connector.Processes[id]
		// In case the server doesn't exist.
		if !err {
			http.Error(w, "{\"error\":\"This server does not exist!\"}", 404)
			return
		}
		if r.Method == "GET" {
			// Get list of files and folders in the directory.
			file, err := os.Open(joinPath(server.Directory, r.URL.Query().Get("path")))
			stat, err1 := file.Stat()
			if err != nil || err1 != nil {
				http.Error(w, "{\"error\":\"This file does not exist!\"}", 404)
				return
			}
			// Send the response.
			buffer := make([]byte, 512)
			file.Read(buffer)
			file.Close()
			w.Header().Set("Content-Disposition", "attachment; filename="+stat.Name())
			w.Header().Set("Content-Type", http.DetectContentType(buffer))
			w.Header().Set("Content-Length", fmt.Sprint(stat.Size()))
			file, _ = os.Open(joinPath(server.Directory, r.URL.Query().Get("path")))
			defer file.Close()
			io.Copy(w, file)
		} else if r.Method == "DELETE" {
			// Check if the file exists.
			file := joinPath(server.Directory, r.URL.Query().Get("path"))
			if file == "/" {
				http.Error(w, "{\"error\":\"This operation is dangerous and has been forbidden!\"}", 403)
				return
			}
			_, err := os.Stat(file)
			if err != nil || os.IsNotExist(err) {
				http.Error(w, "{\"error\":\"This file does not exist!\"}", 404)
				return
			}
			err = os.RemoveAll(file)
			if err != nil && err.(*os.PathError).Err != nil && err.(*os.PathError).Err.Error() ==
				"The process cannot access the file because it is being used by another process." {
				http.Error(w, "{\"error\":\""+err.(*os.PathError).Err.Error()+"\"}", 409)
				return
			} else if err != nil {
				http.Error(w, "{\"error\":\"Internal Server Error!\"}", 500)
				return
			}
			fmt.Fprint(w, "{\"success\":true}")
		} else if r.Method == "POST" {
			// Parse our multipart form, 100 << 20 specifies a maximum upload of 100 MB files.
			r.ParseMultipartForm(100 << 20)
			// FormFile returns the first file for the given key `upload`
			file, meta, err := r.FormFile("upload")
			if err != nil {
				return
			}
			defer file.Close()
			// read the file.
			toWrite, err := os.Create(joinPath(server.Directory, r.URL.Query().Get("path"), meta.Filename))
			stat, err1 := toWrite.Stat()
			if err != nil {
				http.Error(w, "{\"error\":\"Internal Server Error!\"}", 500)
				return
			} else if err1 == nil && stat.IsDir() {
				http.Error(w, "{\"error\":\"This is a folder!\"}", 400)
				return
			}
			defer toWrite.Close()
			// write this byte array to our file
			io.Copy(toWrite, file)
			fmt.Fprintf(w, "{\"success\":true}")
		} else if r.Method == "PATCH" {
			// Get the request body to check the operation.
			var body bytes.Buffer
			body.ReadFrom(r.Body)
			operation := strings.Split(body.String(), "\n")
			// Possible operations: mv, cp
			if operation[0] == "mv" || operation[0] == "cp" {
				if len(operation) != 3 {
					http.Error(w, "{\"error\":\""+operation[0]+" operation requires two arguments!\"}", 405)
					return
				}
				// Check if original file exists.
				// TODO: Needs better sanitation.
				oldpath := joinPath(server.Directory, operation[1])
				newpath := joinPath(server.Directory, operation[2])
				if !strings.HasPrefix(oldpath, path.Clean(server.Directory)) ||
					!strings.HasPrefix(newpath, path.Clean(server.Directory)) {
					http.Error(w, "{\"error\":\"The folder requested is outside the server!\"}", 403)
					return
				}
				file, err := os.Open(oldpath)
				_, err1 := file.Stat()
				if err != nil || os.IsNotExist(err1) {
					http.Error(w, "{\"error\":\"This file does not exist!\"}", 404)
					return
				}
				file.Close()
				// Check if destination file exists.
				file, err = os.Open(newpath)
				stat, err1 := file.Stat()
				if (err == nil || os.IsExist(err1)) && stat != nil && !stat.IsDir() {
					http.Error(w, "{\"error\":\"This file already exists!\"}", 405)
					file.Close()
					return
				} else if stat != nil && stat.IsDir() {
					newpath = joinPath(newpath, path.Base(oldpath))
				}
				// Move file if operation is mv.
				if operation[0] == "mv" {
					err := os.Rename(oldpath, newpath)
					if err != nil && err.(*os.LinkError).Err != nil && err.(*os.LinkError).Err.Error() ==
						"The process cannot access the file because it is being used by another process." {
						http.Error(w, "{\"error\":\""+err.(*os.LinkError).Err.Error()+"\"}", 409)
						return
					} else if err != nil {
						http.Error(w, "{\"error\":\"Internal Server Error!\"}", 500)
						return
					}
					fmt.Fprintf(w, "{\"success\":true}")
				} else {
					var cmd *exec.Cmd
					// TODO: Needs to be looked into, whether or not it actually works.
					if runtime.GOOS == "windows" {
						cmd = exec.Command("robocopy", oldpath, newpath)
					} else {
						cmd = exec.Command("cp", "-r", oldpath, newpath)
					}
					err := cmd.Run()
					if err != nil || cmd.ProcessState.ExitCode() == 16 {
						http.Error(w, "{\"error\":\"Internal Server Error!\"}", 500)
						return
					}
					fmt.Fprintf(w, "{\"success\":true}")
				}
			} else {
				http.Error(w, "{\"error\":\"Invalid operation! Operations available: mv,cp\"}", 405)
			}
		} else {
			http.Error(w, "{\"error\":\"Only GET, POST, PATCH and DELETE are allowed!\"}", 405)
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
		server, err := connector.Processes[id]
		// In case the server doesn't exist.
		if !err {
			http.Error(w, "{\"error\":\"This server does not exist!\"}", 404)
			return
		}
		if r.Method == "POST" {
			// Check if the folder already exists.
			file := joinPath(server.Directory, r.URL.Query().Get("path"))
			_, err := os.Stat(file)
			if !os.IsNotExist(err) {
				http.Error(w, "{\"error\":\"This folder already exists!\"}", 400)
				return
			}
			// Create the folder.
			err = os.Mkdir(file, os.ModePerm)
			if err != nil {
				http.Error(w, "{\"error\":\"Internal Server Error!\"}", 500)
				return
			}
			fmt.Fprintf(w, "{\"success\":true}")
		} else {
			http.Error(w, "{\"error\":\"Only POST is allowed!\"}", 405)
		}
	})
}

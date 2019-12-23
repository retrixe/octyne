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
	"runtime"
	"strings"

	"github.com/gorilla/mux"
)

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
		// TODO: Support symlinks.
		folder, err1 := os.Open(path.Join(server.Directory, r.URL.Query().Get("path")))
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
			if !value.IsDir() {
				var length int64 = 512
				if value.Size() < 512 {
					length = value.Size()
				}
				buffer := make([]byte, length)
				path := path.Join(server.Directory, r.URL.Query().Get("path"), value.Name())
				file, err := os.Open(path)
				if err == nil {
					file.Read(buffer)
					mimeType = http.DetectContentType(buffer)
				}
			}
			toSend["contents"] = append(toSend["contents"], serverFilesResponse{
				Folder:       value.IsDir(),
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
		// TODO: Check with authenticator.
		// if !connector.Validate(w, r) {
		// 	return
		// }
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
			file, err := os.Open(path.Join(server.Directory, r.URL.Query().Get("path")))
			stat, err1 := file.Stat()
			if err != nil || err1 != nil {
				http.Error(w, "{\"error\":\"This file does not exist!\"}", 404)
				return
			}
			// Send the response.
			buffer := make([]byte, 512)
			file.Read(buffer)
			w.Header().Set("Content-Disposition", "attachment; filename="+stat.Name())
			w.Header().Set("Content-Type", http.DetectContentType(buffer))
			w.Header().Set("Content-Length", fmt.Sprint(stat.Size()))
			file, _ = os.Open(path.Join(server.Directory, r.URL.Query().Get("path")))
			io.Copy(w, file)
		} else if r.Method == "DELETE" {
			// Check if the file exists.
			file := path.Join(server.Directory, r.URL.Query().Get("path"))
			_, err := os.Stat(file)
			if err != nil || os.IsNotExist(err) {
				http.Error(w, "{\"error\":\"This file does not exist!\"}", 404)
				return
			}
			err = os.RemoveAll(file)
			if err != nil {
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
			toWrite, err := os.Create(path.Join(server.Directory, r.URL.Query().Get("path"), meta.Filename))
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
			operation := strings.Split(body.String(), " ")
			// Possible operations: mv, cp
			if operation[0] == "mv" || operation[0] == "cp" {
				if len(operation) != 3 {
					http.Error(w, "{\"error\":\""+operation[0]+" operation requires two arguments!\"}", 405)
					return
				}
				// Check if original file exists.
				oldpath := path.Join(server.Directory, operation[1])
				newpath := path.Join(server.Directory, operation[2])
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
				// Check if destination file exists.
				file, err = os.Open(newpath)
				stat, err1 := file.Stat()
				if (err == nil || os.IsExist(err1)) && stat != nil && !stat.IsDir() {
					http.Error(w, "{\"error\":\"This file already exists!\"}", 405)
					return
				}
				// Move file if operation is mv.
				if operation[0] == "mv" {
					err := os.Rename(oldpath, newpath)
					if err != nil {
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
			file := path.Join(server.Directory, r.URL.Query().Get("path"))
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

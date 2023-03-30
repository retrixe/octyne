package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"github.com/retrixe/octyne/system"
)

// joinPath joins any number of path elements into a single path, adding a separating slash if necessary.
func joinPath(elem ...string) string {
	return filepath.FromSlash(path.Join(elem...))
}

// clean combines path.Clean with filepath.FromSlash.
func clean(pathToClean string) string {
	return filepath.FromSlash(path.Clean(pathToClean))
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
		if connector.Validate(w, r) == "" {
			return
		}
		// Get the process being accessed.
		id := mux.Vars(r)["id"]
		process, err := connector.Processes.Load(id)
		// In case the process doesn't exist.
		if !err {
			httpError(w, "This server does not exist!", http.StatusNotFound)
			return
		}
		// Check if folder is in the process directory or not.
		process.ServerConfigMutex.RLock()
		defer process.ServerConfigMutex.RUnlock()
		folderPath := joinPath(process.Directory, r.URL.Query().Get("path"))
		if !strings.HasPrefix(folderPath, clean(process.Directory)) {
			httpError(w, "The folder requested is outside the server!", http.StatusForbidden)
			return
		}
		// Get list of files and folders in the directory.
		folder, err1 := os.Open(folderPath)
		if err1 != nil {
			httpError(w, "This folder does not exist!", http.StatusNotFound)
			return
		}
		defer folder.Close()
		contents, err2 := folder.Readdir(-1)
		if err2 != nil {
			httpError(w, "This is not a folder!", http.StatusBadRequest)
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
				path := joinPath(process.Directory, r.URL.Query().Get("path"), value.Name())
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
	// POST /server/{id}/file?path=path
	// DELETE /server/{id}/file?path=path
	// PATCH /server/{id}/file?path=path
	connector.Router.HandleFunc("/server/{id}/file", func(w http.ResponseWriter, r *http.Request) {
		ticket, ticketExists := connector.Tickets.LoadAndDelete(r.URL.Query().Get("ticket"))
		user := ""
		if ticketExists && ticket.IPAddr == GetIP(r) && r.Method == "GET" {
			user = ticket.User
		} else if user = connector.Validate(w, r); user == "" {
			return
		}
		// Get the process being accessed.
		id := mux.Vars(r)["id"]
		process, err := connector.Processes.Load(id)
		// In case the process doesn't exist.
		if !err {
			httpError(w, "This server does not exist!", http.StatusNotFound)
			return
		}
		// Check if path is in the process directory or not.
		process.ServerConfigMutex.RLock()
		defer process.ServerConfigMutex.RUnlock()
		filePath := joinPath(process.Directory, r.URL.Query().Get("path"))
		if (r.Method == "GET" || r.Method == "POST" || r.Method == "DELETE") &&
			!strings.HasPrefix(filePath, clean(process.Directory)) {
			httpError(w, "The file requested is outside the server!", http.StatusForbidden)
			return
		}
		if r.Method == "GET" {
			// Get list of files and folders in the directory.
			file, err := os.Open(filePath)
			stat, err1 := file.Stat()
			if err != nil || err1 != nil {
				httpError(w, "This file does not exist!", http.StatusNotFound)
				return
			} else if !stat.Mode().IsRegular() {
				httpError(w, "This is not a file!", http.StatusBadRequest)
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
			connector.Info("server.files.download", "ip", GetIP(r), "user", user, "server", id,
				"path", clean(r.URL.Query().Get("path")))
			io.Copy(w, file)
		} else if r.Method == "DELETE" {
			// Check if the file exists.
			if filePath == "/" {
				httpError(w, "This operation is dangerous and has been forbidden!", http.StatusForbidden)
				return
			}
			_, err := os.Stat(filePath)
			if err != nil && os.IsNotExist(err) {
				httpError(w, "This file does not exist!", http.StatusNotFound)
				return
			}
			err = os.RemoveAll(filePath)
			if err != nil && err.(*os.PathError).Err != nil && err.(*os.PathError).Err.Error() ==
				"The process cannot access the file because it is being used by another process." {
				httpError(w, err.(*os.PathError).Err.Error(), http.StatusConflict)
				return
			} else if err != nil {
				log.Println("An error occurred when removing "+filePath, "("+process.Name+")", err)
				httpError(w, "Internal Server Error!", http.StatusInternalServerError)
				return
			}
			connector.Info("server.files.delete", "ip", GetIP(r), "user", user, "server", id,
				"path", clean(r.URL.Query().Get("path")))
			fmt.Fprintln(w, "{\"success\":true}")
		} else if r.Method == "POST" {
			// Parse our multipart form, 5120 << 20 specifies a maximum upload of a 5 GB file.
			err := r.ParseMultipartForm(5120 << 20)
			if err != nil {
				httpError(w, "Invalid form sent!", http.StatusBadRequest)
				return
			}
			// FormFile returns the first file for the given key `upload`
			file, meta, err := r.FormFile("upload")
			if err != nil {
				httpError(w, "Invalid form sent!", http.StatusBadRequest)
				return
			}
			defer file.Close()
			// read the file.
			filePath = joinPath(process.Directory, r.URL.Query().Get("path"), meta.Filename)
			toWrite, err := os.Create(filePath)
			stat, statErr := os.Stat(filePath)
			if statErr == nil && stat.IsDir() {
				httpError(w, "This is a folder!", http.StatusBadRequest)
				return
			} else if err != nil {
				log.Println("An error occurred when writing to "+filePath, "("+process.Name+")", err)
				httpError(w, "Internal Server Error!", http.StatusInternalServerError)
				return
			}
			defer toWrite.Close()
			// write this byte array to our file
			connector.Info("server.files.upload", "ip", GetIP(r), "user", user, "server", id,
				"path", clean(r.URL.Query().Get("path")), "filename", meta.Filename)
			io.Copy(toWrite, file)
			fmt.Fprintln(w, "{\"success\":true}")
		} else if r.Method == "PATCH" {
			// Get the request body to check the operation.
			var body bytes.Buffer
			_, err := body.ReadFrom(r.Body)
			if err != nil {
				httpError(w, "Failed to read body!", http.StatusBadRequest)
				return
			}
			// If the body doesn't start with {, parse as a legacy request. Remove this in Octyne 2.0.
			// Legacy requests will not support anything further than mv/cp operations.
			var req struct {
				Operation string `json:"operation"`
				Src       string `json:"src"`
				Dest      string `json:"dest"`
			}
			if strings.TrimSpace(body.String())[0] != '{' {
				split := strings.Split(body.String(), "\n")
				if len(split) != 3 {
					if split[0] == "mv" || split[0] == "cp" {
						httpError(w, split[0]+" operation requires two arguments!", http.StatusMethodNotAllowed)
					} else {
						httpError(w, "Invalid operation! Operations available: mv,cp", http.StatusMethodNotAllowed)
					}
					return
				}
				req.Operation = split[0]
				req.Src = split[1]
				req.Dest = split[2]
			} else if err = json.Unmarshal(body.Bytes(), &req); err != nil {
				httpError(w, "Invalid JSON body!", http.StatusBadRequest)
				return
			}
			// Possible operations: mv, cp
			if req.Operation == "mv" || req.Operation == "cp" {
				// Check if original file exists.
				oldpath := joinPath(process.Directory, req.Src)
				newpath := joinPath(process.Directory, req.Dest)
				if !strings.HasPrefix(oldpath, clean(process.Directory)) ||
					!strings.HasPrefix(newpath, clean(process.Directory)) {
					httpError(w, "The files requested are outside the server!", http.StatusForbidden)
					return
				}
				stat, err := os.Stat(oldpath)
				if os.IsNotExist(err) {
					httpError(w, "This file does not exist!", http.StatusNotFound)
					return
				} else if err != nil {
					log.Println("An error occurred in mv/cp API when checking for "+oldpath, "("+process.Name+")", err)
					httpError(w, "Internal Server Error!", http.StatusInternalServerError)
					return
				}
				// Check if destination file exists.
				if stat, err := os.Stat(newpath); err == nil && stat.IsDir() {
					newpath = joinPath(newpath, path.Base(oldpath))
				} else if err == nil {
					httpError(w, "This file already exists!", http.StatusMethodNotAllowed)
					return
				} else if err != nil && !os.IsNotExist(err) {
					log.Println("An error occurred in mv/cp API when checking for "+newpath, "("+process.Name+")", err)
					httpError(w, "Internal Server Error!", http.StatusInternalServerError)
					return
				}
				// Move file if operation is mv.
				if req.Operation == "mv" {
					err := os.Rename(oldpath, newpath)
					if err != nil && err.(*os.LinkError).Err != nil && err.(*os.LinkError).Err.Error() ==
						"The process cannot access the file because it is being used by another process." {
						httpError(w, err.(*os.LinkError).Err.Error(), http.StatusConflict)
						return
					} else if err != nil {
						log.Println("An error occurred when moving "+oldpath+" to "+newpath, "("+process.Name+")", err)
						httpError(w, "Internal Server Error!", http.StatusInternalServerError)
						return
					}
					connector.Info("server.files.move", "ip", GetIP(r), "user", user, "server", id,
						"src", clean(req.Src), "dest", clean(req.Dest))
					fmt.Fprintln(w, "{\"success\":true}")
				} else {
					err := system.Copy(stat.Mode(), oldpath, newpath)
					if err != nil {
						log.Println("An error occurred when copying "+oldpath+" to "+newpath, "("+process.Name+")", err)
						httpError(w, "Internal Server Error!", http.StatusInternalServerError)
						return
					}
					connector.Info("server.files.copy", "ip", GetIP(r), "user", user, "server", id,
						"src", clean(req.Src), "dest", clean(req.Dest))
					fmt.Fprintln(w, "{\"success\":true}")
				}
			} else {
				httpError(w, "Invalid operation! Operations available: mv,cp", http.StatusMethodNotAllowed)
			}
		} else {
			httpError(w, "Only GET, POST, PATCH and DELETE are allowed!", http.StatusMethodNotAllowed)
		}
	})

	// POST /server/{id}/folder?path=path
	connector.Router.HandleFunc("/server/{id}/folder", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		user := connector.Validate(w, r)
		if user == "" {
			return
		}
		// Get the process being accessed.
		id := mux.Vars(r)["id"]
		process, err := connector.Processes.Load(id)
		// In case the process doesn't exist.
		if !err {
			httpError(w, "This server does not exist!", http.StatusNotFound)
			return
		}
		if r.Method == "POST" {
			// Check if the folder already exists.
			process.ServerConfigMutex.RLock()
			defer process.ServerConfigMutex.RUnlock()
			file := joinPath(process.Directory, r.URL.Query().Get("path"))
			// Check if folder is in the process directory or not.
			if !strings.HasPrefix(file, clean(process.Directory)) {
				httpError(w, "The folder requested is outside the server!", http.StatusForbidden)
				return
			}
			_, err := os.Stat(file)
			if !os.IsNotExist(err) {
				httpError(w, "This folder already exists!", http.StatusBadRequest)
				return
			}
			// Create the folder.
			err = os.Mkdir(file, os.ModePerm)
			if err != nil {
				log.Println("An error occurred when creating folder "+file, "("+process.Name+")", err)
				httpError(w, "Internal Server Error!", http.StatusInternalServerError)
				return
			}
			connector.Info("server.files.createFolder", "ip", GetIP(r), "user", user, "server", id,
				"path", clean(r.URL.Query().Get("path")))
			fmt.Fprintln(w, "{\"success\":true}")
		} else {
			httpError(w, "Only POST is allowed!", http.StatusMethodNotAllowed)
		}
	})

	// POST /server/{id}/compress?path=path&compress=true/false (compress is optional, default: true)
	connector.Router.HandleFunc("/server/{id}/compress", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		user := connector.Validate(w, r)
		if user == "" {
			return
		}
		// Get the process being accessed.
		id := mux.Vars(r)["id"]
		process, err := connector.Processes.Load(id)
		// In case the process doesn't exist.
		if !err {
			httpError(w, "This server does not exist!", http.StatusNotFound)
			return
		}
		if r.Method == "POST" {
			// Get the body.
			var buffer bytes.Buffer
			_, err := buffer.ReadFrom(r.Body)
			if err != nil {
				httpError(w, "Failed to read body!", http.StatusBadRequest)
				return
			}
			// Decode the array body and send it to files.
			var files []string
			err = json.Unmarshal(buffer.Bytes(), &files)
			if err != nil {
				httpError(w, "Invalid JSON body!", http.StatusBadRequest)
				return
			}
			// Validate every path.
			process.ServerConfigMutex.RLock()
			defer process.ServerConfigMutex.RUnlock()
			for _, file := range files {
				filepath := joinPath(process.Directory, file)
				if !strings.HasPrefix(filepath, clean(process.Directory)) {
					httpError(w, "One of the paths provided is outside the server directory!", http.StatusForbidden)
					return
				} else if _, err := os.Stat(filepath); err != nil {
					if os.IsNotExist(err) {
						httpError(w, "The file "+file+" does not exist!", http.StatusBadRequest)
					} else {
						log.Println("An error occurred when checking "+filepath+" exists for compression", "("+process.Name+")", err)
						httpError(w, "Internal Server Error!", http.StatusInternalServerError)
					}
					return
				}
			}
			// Check if a file exists at the location of the ZIP file.
			zipPath := joinPath(process.Directory, r.URL.Query().Get("path"))
			if !strings.HasPrefix(zipPath, clean(process.Directory)) {
				httpError(w, "The requested ZIP file is outside the server directory!", http.StatusForbidden)
				return
			}
			_, exists := os.Stat(zipPath)
			if !os.IsNotExist(exists) {
				httpError(w, "A file/folder already exists at the path of requested ZIP!", http.StatusBadRequest)
				return
			}

			// Begin compressing a ZIP.
			zipFile, err := os.Create(zipPath)
			if err != nil {
				log.Println("An error occurred when creating "+zipPath+" for compression", "("+process.Name+")", err)
				httpError(w, "Internal Server Error!", http.StatusInternalServerError)
				return
			}
			defer zipFile.Close()
			archive := zip.NewWriter(zipFile)
			defer archive.Close()
			// Archive stuff inside.
			compressed := r.URL.Query().Get("compress") != "false"
			// TODO: Why is parent always process.Directory? Support different base path?
			for _, file := range files {
				err := system.AddFileToZip(archive, process.Directory, file, compressed)
				if err != nil {
					log.Println("An error occurred when adding "+file+" to "+zipPath, "("+process.Name+")", err)
					httpError(w, "Internal Server Error!", http.StatusInternalServerError)
					return
				}
			}
			connector.Info("server.files.compress", "ip", GetIP(r), "user", user, "server", id,
				"zipFile", clean(r.URL.Query().Get("path")), "files", files, "compressed", compressed)
			fmt.Fprintln(w, "{\"success\":true}")
		} else {
			httpError(w, "Only POST is allowed!", http.StatusMethodNotAllowed)
		}
	})

	// POST /server/{id}/decompress?path=path
	connector.Router.HandleFunc("/server/{id}/decompress", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		user := connector.Validate(w, r)
		if user == "" {
			return
		}
		// Get the process being accessed.
		id := mux.Vars(r)["id"]
		process, err := connector.Processes.Load(id)
		// In case the process doesn't exist.
		if !err {
			httpError(w, "This server does not exist!", http.StatusNotFound)
			return
		}
		process.ServerConfigMutex.RLock()
		defer process.ServerConfigMutex.RUnlock()
		directory := clean(process.Directory)
		if r.Method == "POST" {
			// Check if the ZIP file exists.
			zipPath := joinPath(directory, r.URL.Query().Get("path"))
			if !strings.HasPrefix(zipPath, directory) {
				httpError(w, "The ZIP file is outside the server directory!", http.StatusForbidden)
				return
			}
			zipStat, exists := os.Stat(zipPath)
			if os.IsNotExist(exists) {
				httpError(w, "The requested ZIP does not exist!", http.StatusBadRequest)
				return
			} else if exists != nil {
				log.Println("An error occurred when checking "+zipPath+" ZIP file exists", "("+process.Name+")", err)
				httpError(w, "Internal Server Error!", http.StatusInternalServerError)
				return
			} else if zipStat.IsDir() {
				httpError(w, "The requested ZIP is a folder!", http.StatusBadRequest)
				return
			}
			// Check if there is a file/folder at the destination.
			var body bytes.Buffer
			_, err := body.ReadFrom(r.Body)
			if err != nil {
				httpError(w, "Failed to read body!", http.StatusBadRequest)
				return
			}
			unpackPath := joinPath(directory, body.String())
			if !strings.HasPrefix(unpackPath, directory) {
				httpError(w, "The ZIP file is outside the server directory!", http.StatusForbidden)
				return
			}
			stat, err := os.Stat(unpackPath)
			if os.IsNotExist(err) {
				err = os.Mkdir(unpackPath, os.ModePerm)
				if err != nil {
					log.Println("An error occurred when creating "+unpackPath+" to unpack ZIP", "("+process.Name+")", err)
					httpError(w, "Internal Server Error!", http.StatusInternalServerError)
					return
				}
			} else if err != nil {
				log.Println("An error occurred when checking "+unpackPath+" exists to unpack ZIP to", "("+process.Name+")", err)
				httpError(w, "Internal Server Error!", http.StatusInternalServerError)
				return
			} else if !stat.IsDir() {
				httpError(w, "There is a file at the requested unpack destination!", http.StatusBadRequest)
				return
			}
			// Decompress the ZIP.
			err = system.UnzipFile(zipPath, unpackPath)
			if err != nil {
				httpError(w, "An error occurred while unzipping!", http.StatusInternalServerError)
				return
			}
			connector.Info("server.files.decompress", "ip", GetIP(r), "user", user, "server", id,
				"zipFile", clean(r.URL.Query().Get("path")), "destPath", body.String())
			fmt.Fprintln(w, "{\"success\":true}")
		} else {
			httpError(w, "Only POST is allowed!", http.StatusMethodNotAllowed)
		}
	})
}

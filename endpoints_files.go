package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/puzpuzpuz/xsync/v3"
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

// GET /server/{id}/files?path=path
type serverFilesResponse struct {
	Name         string `json:"name"`
	Size         int64  `json:"size"`
	MimeType     string `json:"mimeType"`
	Folder       bool   `json:"folder"`
	LastModified int64  `json:"lastModified"`
}

func filesEndpoint(connector *Connector, w http.ResponseWriter, r *http.Request) {
	// Check with authenticator.
	if connector.Validate(w, r) == "" {
		return
	}
	// Get the process being accessed.
	id := r.PathValue("id")
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
	writeJsonStructRes(w, toSend) // skipcq GSC-G104
}

// GET /server/{id}/file?path=path&ticket=ticket
// POST /server/{id}/file?path=path
// DELETE /server/{id}/file?path=path
// PATCH /server/{id}/file?path=path
func fileEndpoint(connector *Connector, w http.ResponseWriter, r *http.Request) {
	ticket, ticketExists := connector.Tickets.LoadAndDelete(r.URL.Query().Get("ticket"))
	user := ""
	if ticketExists && ticket.IPAddr == GetIP(r) && r.Method == "GET" {
		user = ticket.User
	} else if user = connector.Validate(w, r); user == "" {
		return
	}
	// Get the process being accessed.
	id := r.PathValue("id")
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
		fileEndpointGet(connector, w, r, id, filePath, user)
	} else if r.Method == "DELETE" {
		fileEndpointDelete(connector, w, r, id, filePath, user)
	} else if r.Method == "POST" {
		fileEndpointPost(connector, w, r, id, filePath, user)
	} else if r.Method == "PATCH" {
		fileEndpointPatch(connector, w, r, process, id, user)
	} else {
		httpError(w, "Only GET, POST, PATCH and DELETE are allowed!", http.StatusMethodNotAllowed)
	}
}

func fileEndpointGet(connector *Connector, w http.ResponseWriter, r *http.Request,
	id string, filePath string, user string) {
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
}

func fileEndpointPost(connector *Connector, w http.ResponseWriter, r *http.Request,
	id string, filePath string, user string) {
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
	filePath = joinPath(filePath, meta.Filename)
	toWrite, err := os.Create(filePath)
	stat, statErr := os.Stat(filePath)
	if statErr == nil && stat.IsDir() {
		httpError(w, "This is a folder!", http.StatusBadRequest)
		return
	} else if err != nil {
		log.Println("An error occurred when writing to "+filePath, "("+id+")", err)
		httpError(w, "Internal Server Error!", http.StatusInternalServerError)
		return
	}
	defer toWrite.Close()
	// write this byte array to our file
	connector.Info("server.files.upload", "ip", GetIP(r), "user", user, "server", id,
		"path", clean(r.URL.Query().Get("path")), "filename", meta.Filename)
	io.Copy(toWrite, file)
	writeJsonStringRes(w, "{\"success\":true}")
}

func fileEndpointPatch(connector *Connector, w http.ResponseWriter, r *http.Request,
	process *ExposedProcess, id string, user string) {
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
			writeJsonStringRes(w, "{\"success\":true}")
		} else {
			err := system.Copy(stat.Mode(), oldpath, newpath)
			if err != nil {
				log.Println("An error occurred when copying "+oldpath+" to "+newpath, "("+process.Name+")", err)
				httpError(w, "Internal Server Error!", http.StatusInternalServerError)
				return
			}
			connector.Info("server.files.copy", "ip", GetIP(r), "user", user, "server", id,
				"src", clean(req.Src), "dest", clean(req.Dest))
			writeJsonStringRes(w, "{\"success\":true}")
		}
	} else {
		httpError(w, "Invalid operation! Operations available: mv,cp", http.StatusMethodNotAllowed)
	}
}

func fileEndpointDelete(connector *Connector, w http.ResponseWriter, r *http.Request,
	id string, filePath string, user string) {
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
		log.Println("An error occurred when removing "+filePath, "("+id+")", err)
		httpError(w, "Internal Server Error!", http.StatusInternalServerError)
		return
	}
	connector.Info("server.files.delete", "ip", GetIP(r), "user", user, "server", id,
		"path", clean(r.URL.Query().Get("path")))
	writeJsonStringRes(w, "{\"success\":true}")
}

// POST /server/{id}/folder?path=path
func folderEndpoint(connector *Connector, w http.ResponseWriter, r *http.Request) {
	// Check with authenticator.
	user := connector.Validate(w, r)
	if user == "" {
		return
	}
	// Get the process being accessed.
	id := r.PathValue("id")
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
		writeJsonStringRes(w, "{\"success\":true}")
	} else {
		httpError(w, "Only POST is allowed!", http.StatusMethodNotAllowed)
	}
}

// GET /server/{id}/compress?token=token
// POST /server/{id}/compress?path=path&compress=true/false/zstd/xz/gzip&archiveType=zip/tar&basePath=path&async=boolean
// POST /server/{id}/compress/v2?path=path&compress=true/false/zstd/xz/gzip&archiveType=zip/tar&basePath=path&async=boolean
var compressionProgressMap = xsync.NewMapOf[string, string]()

func compressionEndpoint(connector *Connector, w http.ResponseWriter, r *http.Request) {
	// Check with authenticator.
	user := connector.Validate(w, r)
	if user == "" {
		return
	}
	// Get the process being accessed.
	id := r.PathValue("id")
	process, exists := connector.Processes.Load(id)
	// In case the process doesn't exist.
	if !exists {
		httpError(w, "This server does not exist!", http.StatusNotFound)
		return
	} else if r.Method == "GET" {
		if r.URL.Query().Get("token") == "" {
			httpError(w, "No token provided!", http.StatusBadRequest)
			return
		}
		progress, valid := compressionProgressMap.Load(r.URL.Query().Get("token"))
		if !valid {
			httpError(w, "Invalid token!", http.StatusBadRequest)
		} else if progress == "finished" {
			writeJsonStringRes(w, "{\"finished\":true}")
		} else if progress == "" {
			writeJsonStringRes(w, "{\"finished\":false}")
		} else {
			httpError(w, progress, http.StatusInternalServerError)
		}
		return
	} else if r.Method != "POST" {
		httpError(w, "Only GET and POST are allowed!", http.StatusMethodNotAllowed)
		return
	}
	// Decode parameters.
	async := r.URL.Query().Get("async") == "true"
	basePath := r.URL.Query().Get("basePath")
	archiveType := "zip"
	compression := "true"
	if r.URL.Query().Get("archiveType") != "" {
		archiveType = r.URL.Query().Get("archiveType")
	}
	if r.URL.Query().Get("compress") != "" {
		compression = r.URL.Query().Get("compress")
	}
	if archiveType != "zip" && archiveType != "tar" {
		httpError(w, "Invalid archive type!", http.StatusBadRequest)
		return
	} else if compression != "true" && compression != "false" &&
		compression != "zstd" && compression != "xz" && compression != "gzip" {
		httpError(w, "Invalid compression type!", http.StatusBadRequest)
		return
	}
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
	if !strings.HasPrefix(joinPath(process.Directory, basePath), clean(process.Directory)) {
		httpError(w, "The base path is outside the server directory!", http.StatusForbidden)
		return
	}
	for _, file := range files {
		filepath := joinPath(process.Directory, basePath, file)
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
	// Check if a file exists at the location of the archive.
	archivePath := joinPath(process.Directory, r.URL.Query().Get("path"))
	if !strings.HasPrefix(archivePath, clean(process.Directory)) {
		httpError(w, "The requested archive is outside the server directory!", http.StatusForbidden)
		return
	}
	_, err = os.Stat(archivePath)
	if !os.IsNotExist(err) {
		httpError(w, "A file/folder already exists at the path of requested archive!", http.StatusBadRequest)
		return
	}

	// Begin compressing the archive.
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		log.Println("An error occurred when creating "+archivePath+" for compression", "("+process.Name+")", err)
		httpError(w, "Internal Server Error!", http.StatusInternalServerError)
		return
	}
	tokenBytes := make([]byte, 16)
	rand.Read(tokenBytes) // Tolerate errors here, an error here is incredibly unlikely: skipcq GSC-G104
	token := hex.EncodeToString(tokenBytes)
	completionFunc := func() {
		defer archiveFile.Close()
		if archiveType == "zip" {
			archive := zip.NewWriter(archiveFile)
			defer archive.Close()
			// Archive stuff inside.
			for _, file := range files {
				err := system.AddFileToZip(archive, joinPath(process.Directory, basePath), file, compression != "false")
				if err != nil {
					log.Println("An error occurred when adding "+file+" to "+archivePath, "("+process.Name+")", err)
					if !async {
						httpError(w, "Internal Server Error!", http.StatusInternalServerError)
					} else {
						compressionProgressMap.Store(token, "Internal Server Error!")
					}
					return
				}
			}
		} else {
			var archive *tar.Writer
			if compression == "true" || compression == "gzip" || compression == "" {
				compressionWriter := gzip.NewWriter(archiveFile)
				defer compressionWriter.Close()
				archive = tar.NewWriter(compressionWriter)
			} else if compression == "xz" || compression == "zstd" {
				compressionWriter := system.NativeCompressionWriter(archiveFile, compression)
				defer compressionWriter.Close()
				archive = tar.NewWriter(compressionWriter)
			} else {
				archive = tar.NewWriter(archiveFile)
			}
			defer archive.Close()
			for _, file := range files {
				err := system.AddFileToTar(archive, joinPath(process.Directory, basePath), file)
				if err != nil {
					log.Println("An error occurred when adding "+file+" to "+archivePath, "("+process.Name+")", err)
					if !async {
						httpError(w, "Internal Server Error!", http.StatusInternalServerError)
					} else {
						compressionProgressMap.Store(token, "Internal Server Error!")
					}
					return
				}
			}
		}
		connector.Info("server.files.compress", "ip", GetIP(r), "user", user, "server", id,
			"archive", clean(r.URL.Query().Get("path")), "archiveType", archiveType,
			"compression", compression, "basePath", basePath, "files", files)
		if async {
			compressionProgressMap.Store(token, "finished")
			go func() { // We want our previous Close() defers to call *now*, so we do this in goroutine
				<-time.After(10 * time.Second)
				compressionProgressMap.Delete(token)
			}()
		} else {
			writeJsonStringRes(w, "{\"success\":true}")
		}
	}
	if async {
		compressionProgressMap.Store(token, "")
		writeJsonStringRes(w, "{\"token\":\""+token+"\"}")
		go completionFunc()
	} else {
		completionFunc()
	}
}

// POST /server/{id}/decompress?path=path
func decompressionEndpoint(connector *Connector, w http.ResponseWriter, r *http.Request) {
	// Check with authenticator.
	user := connector.Validate(w, r)
	if user == "" {
		return
	}
	// Get the process being accessed.
	id := r.PathValue("id")
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
		// Check if the archive exists.
		archivePath := joinPath(directory, r.URL.Query().Get("path"))
		if !strings.HasPrefix(archivePath, directory) {
			httpError(w, "The archive is outside the server directory!", http.StatusForbidden)
			return
		}
		archiveStat, exists := os.Stat(archivePath)
		if os.IsNotExist(exists) {
			httpError(w, "The requested archive does not exist!", http.StatusBadRequest)
			return
		} else if exists != nil {
			log.Println("An error occurred when checking "+archivePath+" archive file exists", "("+process.Name+")", err)
			httpError(w, "Internal Server Error!", http.StatusInternalServerError)
			return
		} else if archiveStat.IsDir() {
			httpError(w, "The requested archive is a folder!", http.StatusBadRequest)
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
			httpError(w, "The archive file is outside the server directory!", http.StatusForbidden)
			return
		}
		stat, err := os.Stat(unpackPath)
		if os.IsNotExist(err) {
			err = os.Mkdir(unpackPath, os.ModePerm)
			if err != nil {
				log.Println("An error occurred when creating "+unpackPath+" to unpack archive", "("+process.Name+")", err)
				httpError(w, "Internal Server Error!", http.StatusInternalServerError)
				return
			}
		} else if err != nil {
			log.Println("An error occurred when checking "+unpackPath+" exists to unpack archive to", "("+process.Name+")", err)
			httpError(w, "Internal Server Error!", http.StatusInternalServerError)
			return
		} else if !stat.IsDir() {
			httpError(w, "There is a file at the requested unpack destination!", http.StatusBadRequest)
			return
		}
		// Decompress the archive.
		if strings.HasSuffix(archivePath, ".zip") {
			err = system.UnzipFile(archivePath, unpackPath)
		} else if strings.HasSuffix(archivePath, ".tar") ||
			strings.HasSuffix(archivePath, ".tar.gz") || strings.HasSuffix(archivePath, ".tgz") ||
			strings.HasSuffix(archivePath, ".tar.bz") || strings.HasSuffix(archivePath, ".tbz") ||
			strings.HasSuffix(archivePath, ".tar.bz2") || strings.HasSuffix(archivePath, ".tbz2") ||
			strings.HasSuffix(archivePath, ".tar.xz") || strings.HasSuffix(archivePath, ".txz") ||
			strings.HasSuffix(archivePath, ".tar.zst") || strings.HasSuffix(archivePath, ".tzst") {
			err = system.ExtractTarFile(archivePath, unpackPath)
		} else {
			httpError(w, "Unsupported archive file!", http.StatusBadRequest)
			return
		}
		if err != nil {
			httpError(w, "An error occurred while decompressing archive!", http.StatusInternalServerError)
			return
		}
		connector.Info("server.files.decompress", "ip", GetIP(r), "user", user, "server", id,
			"archive", clean(r.URL.Query().Get("path")), "destPath", body.String())
		writeJsonStringRes(w, "{\"success\":true}")
	} else {
		httpError(w, "Only POST is allowed!", http.StatusMethodNotAllowed)
	}
}

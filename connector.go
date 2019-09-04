package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// An internal representation of Process along with the clients connected to it and its output.
type server struct {
	*Process
	Clients map[string]*websocket.Conn
	Console string
}

// Connector ...
// A connector used by octyne to create an HTTP API to interact with integrations.
type Connector struct {
	Config
	*Authenticator
	*mux.Router
	*websocket.Upgrader
	Processes map[string]*server
}

// InitializeConnector ... Initialize a connector to create an HTTP API for interaction.
func InitializeConnector(config Config) *Connector {
	// Create the connector.
	connector := &Connector{
		Config:        config,
		Router:        mux.NewRouter().StrictSlash(true),
		Processes:     make(map[string]*server),
		Authenticator: InitializeAuthenticator(config),
		Upgrader:      &websocket.Upgrader{},
	}
	// Initialize all routes for the connector.
	/*
		All routes:
		GET /login

		GET /servers

		- GET /server/{id} (FTP info, statistics e.g. server version, players online, uptime, CPU and RAM)
		POST /server/{id} (to start and stop a server)

		WS /server/{id}/console

		GET /server/{id}/files?path=path

		GET /server/{id}/file?path=path
		- DOWNLOAD /server/{id}/file?path=path
		- POST /server/{id}/file?path=path
		DELETE /server/{id}/file?path=path
		- PATCH /server/{id}/file?path=path (moving files, copying files and renaming them)


		POST /server/{id}/folder?path=path
	*/
	connector.registerRoutes()
	return connector
}

// AddProcess ... Add a process to the connector to be opened up.
func (connector *Connector) AddProcess(process *Process) {
	server := &server{
		Process: process,
		Clients: make(map[string]*websocket.Conn),
		Console: "",
	}
	connector.Processes[server.Name] = server
	// Run a function which will monitor the console output of this process.
	go (func() {
		scanner := bufio.NewScanner(server.Output)
		scanner.Split(bufio.ScanLines)
		for scanner.Scan() {
			m := scanner.Text()
			server.Console = server.Console + "\n" + m
			for _, connection := range server.Clients {
				connection.WriteMessage(websocket.TextMessage, []byte(m))
			}
		}
	})()
}

func (connector *Connector) registerRoutes() {
	// GET /
	connector.Router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Hi, octyne is online and listening to this port successfully!")
	})

	// GET /login
	type loginResponse struct {
		Token string `json:"token"`
	}
	connector.Router.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		// In case the username and password headers don't exist.
		username := r.Header.Get("Username")
		password := r.Header.Get("Password")
		if username == "" || password == "" {
			http.Error(w, "{\"error\":\"Username or password not provided!\"}", 400)
			return
		}
		// Authorize the user.
		token := connector.Login(username, password)
		if token == "" {
			http.Error(w, "{\"error\":\"Invalid username or password!\"}", 401)
			return
		}
		// Send the response.
		json.NewEncoder(w).Encode(loginResponse{Token: token})
	})

	// GET /servers
	type serversResponse struct {
		Servers map[string]int `json:"servers"`
	}
	connector.Router.HandleFunc("/servers", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		if !connector.Validate(w, r) {
			return
		}
		// Get a map of servers and their online status.
		servers := make(map[string]int)
		for _, v := range connector.Processes {
			servers[v.Name] = v.Online
		}
		// Send the list.
		json.NewEncoder(w).Encode(serversResponse{Servers: servers})
	})

	// GET /server/{id}
	// POST /server/{id}
	connector.Router.HandleFunc("/server/{id}", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		if !connector.Validate(w, r) {
			return
		}
		// Get the server being accessed.
		id := mux.Vars(r)["id"]
		_, err := connector.Config.Servers[id]
		// In case the server doesn't exist.
		if !err {
			http.Error(w, "{\"error\":\"This server does not exist!\"}", 404)
			return
		}
		// POST /server/{id}
		if r.Method == "POST" {
			// Get the request body to check whether the operation is to START or STOP.
			var body bytes.Buffer
			body.ReadFrom(r.Body)
			operation := strings.ToUpper(body.String())
			// Check whether the operation is correct or not.
			if operation == "START" {
				// Start process if required.
				if connector.Processes[id].Online != 1 {
					connector.Processes[id].StartProcess()
				}
				// Send a response.
				res := make(map[string]bool)
				res["success"] = true
				json.NewEncoder(w).Encode(res)
			} else if operation == "STOP" {
				// Stop process if required.
				if connector.Processes[id].Online == 1 {
					connector.Processes[id].StopProcess()
				}
				// Send a response.
				res := make(map[string]bool)
				res["success"] = true
				json.NewEncoder(w).Encode(res)
			} else {
				http.Error(w, "{\"error\":\"Invalid operation requested!\"}", 400)
				return
			}
			// GET /server/{id}
		} else if r.Method == "GET" {
		} else {
			http.Error(w, "{\"error\":\"Only GET and POST is allowed!\"}", 405)
		}
	})

	// WS /server/{id}/console
	connector.Upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	connector.Router.HandleFunc("/server/{id}/console", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		// TODO: Need to figure out how to impl. WS authentication.
		if !connector.Validate(w, r) {
			return
		}
		// Retrieve the token.
		token := r.Header.Get("Authorization")
		if r.Header.Get("Cookie") != "" && token == "" {
			cookie, exists := r.Cookie("X-Authentication")
			if exists == nil {
				token = cookie.Value
			}
		}
		// Get the server being accessed.
		id := mux.Vars(r)["id"]
		_, exists := connector.Config.Servers[id]
		// In case the server doesn't exist.
		if !exists {
			http.Error(w, "{\"error\":\"This server does not exist!\"", 404)
			return
		}
		// Upgrade WebSocket connection.
		c, err := connector.Upgrade(w, r, nil)
		if err == nil {
			defer c.Close()
			// Add connection to the process after sending current console output.
			process, _ := connector.Processes[id]
			c.WriteMessage(websocket.TextMessage, []byte(process.Console)) // TODO: Consider Mutexes.
			process.Clients[token] = c
			// Read messages from the user and execute them.
			for {
				// Another client has connected with the same token. Terminate existing connection.
				if process.Clients[token] != c {
					break
				}
				// Read messages from the user.
				_, message, err := c.ReadMessage()
				if err != nil {
					delete(process.Clients, token)
					break // The WebSocket connection has terminated.
				} else {
					process.SendCommand(string(message))
				}
			}
		}
	})

	// GET /server/{id}/files?path=path
	type serverFilesResponse struct {
		Name         string `json:"name"`
		Size         int64  `json:"size"`
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
		server, err := connector.Config.Servers[id]
		// In case the server doesn't exist.
		if !err {
			http.Error(w, "{\"error\":\"This server does not exist!\"}", 404)
			return
		}
		// Get list of files and folders in the directory.
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
			toSend["contents"] = append(toSend["contents"], serverFilesResponse{
				Folder:       value.IsDir(),
				Name:         value.Name(),
				Size:         value.Size(),
				LastModified: value.ModTime().Unix(),
			})
		}
		json.NewEncoder(w).Encode(toSend)
	})

	// GET /server/{id}/file?path=path
	// DOWNLOAD /server/{id}/file?path=path
	// POST /server/{id}/file?path=path
	// DELETE /server/{id}/file?path=path
	connector.Router.HandleFunc("/server/{id}/file", func(w http.ResponseWriter, r *http.Request) {
		// TODO: Check with authenticator.
		// if !connector.Validate(w, r) {
		// 	return
		// }
		// Get the server being accessed.
		id := mux.Vars(r)["id"]
		server, err := connector.Config.Servers[id]
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
			w.Write(buffer)
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
			/*
				} else if r.Method == "POST" {
					// Parse our multipart form, 1024 << 20 specifies a maximum upload of 1024 MB files.
					r.ParseMultipartForm(1024 << 20)
					// FormFile returns the first file for the given key `myFile`
					file, _, err := r.FormFile("myFile")
					if err != nil {
						return
					}
					defer file.Close()
					// read the file.
					toWrite, err := os.Open(path.Join(server.Directory, r.URL.Query().Get("path")))
					stat, err1 := toWrite.Stat()
					if err != nil {
						http.Error(w, "{\"error\":\"Internal Server Error!\"}", 500)
						return
					} else if err1 == nil && stat.IsDir() {
						http.Error(w, "{\"error\":\"This is a folder!\"}", 400)
					}
					defer toWrite.Close()
					// write this byte array to our temporary file
					io.Copy(toWrite, file)
					fmt.Fprintf(w, "{\"success\":true}")
			*/
		} else {
			http.Error(w, "{\"error\":\"Only GET, POST and DELETE are allowed!\"}", 405)
		}
	})
}

package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/retrixe/octyne/system"
)

// An internal representation of Process along with the clients connected to it and its output.
type server struct {
	*Process
	Clients     map[string]chan []byte
	Console     string
	ClientsLock sync.RWMutex
	ConsoleLock sync.RWMutex
}

// Ticket is a one-time ticket usable by browsers to quickly authenticate with the WebSocket API.
type Ticket struct {
	Time   int64
	Token  string
	IPAddr string
}

type processMap struct{ sync.Map }

// Get gets a *server from a processMap by using sync.Map Load function and type-casting.
func (p *processMap) Get(name string) (*server, bool) {
	item, err := p.Load(name)
	if !err {
		return nil, err
	}
	process, err := item.(*server)
	return process, err
}

// Connector is used to create an HTTP API for external apps to talk with octyne.
type Connector struct {
	Config
	Authenticator
	*mux.Router
	*websocket.Upgrader
	Processes   processMap
	Tickets     map[string]Ticket
	TicketsLock sync.RWMutex
}

// DeleteTicket deletes a ticket from the Connector and handles the TicketsLock.
func (connector *Connector) DeleteTicket(id string) {
	connector.TicketsLock.Lock()
	defer connector.TicketsLock.Unlock()
	delete(connector.Tickets, id)
}

// GetTicket gets a ticket from the Connector and handles the TicketsLock.
func (connector *Connector) GetTicket(id string) (Ticket, bool) {
	connector.TicketsLock.RLock()
	defer connector.TicketsLock.RUnlock()
	a, b := connector.Tickets[id]
	return a, b
}

// GetIP gets an IP address from http.Request.RemoteAddr.
func GetIP(r *http.Request) string {
	if r.Header.Get("x-forwarded-for") != "" {
		return strings.Split(r.Header.Get("x-forwarded-for"), ", ")[0]
	}
	index := strings.LastIndex(r.RemoteAddr, ":")
	if index == -1 {
		return r.RemoteAddr
	}
	return r.RemoteAddr[:index]
}

// InitializeConnector initializes a connector to create an HTTP API for interaction.
func InitializeConnector(config Config) *Connector {
	// Create the connector.
	connector := &Connector{
		Config:        config,
		Router:        mux.NewRouter().StrictSlash(true),
		Processes:     processMap{},
		Tickets:       make(map[string]Ticket),
		Authenticator: InitializeAuthenticator(config),
		Upgrader:      &websocket.Upgrader{},
	}
	// Initialize all routes for the connector.
	/*
		All routes:
		GET /login
		GET /logout

		GET /servers
		GET /ott (one-time ticket)

		- GET /server/{id} (FTP info, statistics e.g. server version, players online, uptime, CPU and RAM)
		POST /server/{id} (to start and stop a server)

		WS /server/{id}/console?ticket=ticket

		GET /server/{id}/files?path=path

		GET /server/{id}/file?path=path&ticket=ticket
		POST /server/{id}/file?path=path (takes a form file with the file name, path= is path to folder)
		DELETE /server/{id}/file?path=path
		PATCH /server/{id}/file (moving files, copying files and renaming them)

		POST /server/{id}/compress?path=path&compress=true/false (compress is optional, default: true)
		POST /server/{id}/decompress?path=path

		POST /server/{id}/folder?path=path

		NOTE: All routes marked with - are incomplete.
	*/
	connector.registerRoutes()
	connector.registerFileRoutes()
	return connector
}

// AddProcess adds a process to the connector to be accessed via the HTTP API.
func (connector *Connector) AddProcess(process *Process) {
	server := &server{
		Process: process,
		Clients: make(map[string]chan []byte),
		Console: "",
	}
	connector.Processes.Store(server.Name, server)
	// Run a function which will monitor the console output of this process.
	go (func() {
		for {
			scanner := bufio.NewScanner(server.Output)
			scanner.Split(bufio.ScanLines)
			buf := make([]byte, 0, 64*1024) // Support up to 1 MB lines.
			scanner.Buffer(buf, 1024*1024)
			for scanner.Scan() {
				m := scanner.Text()
				// Truncate the console scrollback to 2500 to prevent excess memory usage and download cost.
				// TODO: These limits aren't exactly the best, it maxes up to 2.5 GB.
				(func() {
					server.ConsoleLock.Lock()
					defer server.ConsoleLock.Unlock()
					truncate := strings.Split(server.Console, "\n")
					if len(truncate) >= 2500 {
						server.Console = strings.Join(truncate[len(truncate)-2500:], "\n")
					}
					server.Console = server.Console + "\n" + m
					server.ClientsLock.RLock()
					defer server.ClientsLock.RUnlock()
					for _, connection := range server.Clients {
						connection <- []byte(m)
					}
				})()
			}
			log.Println("Error in " + server.Name + " console: " + scanner.Err().Error())
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
			http.Error(w, "{\"error\":\"Username or password not provided!\"}", http.StatusBadRequest)
			return
		}
		// Authorize the user.
		token := connector.Login(username, password)
		if token == "" {
			http.Error(w, "{\"error\":\"Invalid username or password!\"}", http.StatusUnauthorized)
			return
		}
		// Send the response.
		json.NewEncoder(w).Encode(loginResponse{Token: token}) // skipcq GSC-G104
	})

	connector.Router.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		// In case the authorization header doesn't exist.
		token := r.Header.Get("Authorization")
		if token == "" {
			http.Error(w, "{\"error\":\"Token not provided!\"}", http.StatusBadRequest)
			return
		}
		// Authorize the user.
		success := connector.Logout(token)
		if !success {
			http.Error(w, "{\"error\":\"Invalid token, failed to logout!\"}", http.StatusUnauthorized)
			return
		}
		// Send the response.
		fmt.Fprint(w, "{\"success\": true}")
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
		connector.Processes.Range(func(k, v interface{}) bool {
			servers[v.(*server).Name] = v.(*server).Online
			return true
		})
		// Send the list.
		json.NewEncoder(w).Encode(serversResponse{Servers: servers}) // skipcq GSC-G104
	})

	// GET /ott
	connector.Router.HandleFunc("/ott", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		if !connector.Validate(w, r) {
			return
		}
		// Add a ticket.
		ticket := make([]byte, 4)
		rand.Read(ticket) // Tolerate errors here, an error here is incredibly unlikely: skipcq GSC-G104
		ticketString := base64.StdEncoding.EncodeToString(ticket)
		(func() {
			connector.TicketsLock.Lock()
			defer connector.TicketsLock.Unlock()
			connector.Tickets[ticketString] = Ticket{
				Time:   time.Now().Unix(),
				Token:  r.Header.Get("Authorization"),
				IPAddr: GetIP(r),
			}
		})()
		// Schedule deletion (cancellable).
		go (func() {
			<-time.After(2 * time.Minute)
			connector.DeleteTicket(ticketString)
		})()
		// Send the response.
		fmt.Fprint(w, "{\"ticket\": \""+ticketString+"\"}")
	})

	// GET /server/{id}
	// POST /server/{id}
	type serverResponse struct {
		Status      int     `json:"status"`
		CPUUsage    float64 `json:"cpuUsage"`
		MemoryUsage float64 `json:"memoryUsage"`
		TotalMemory int64   `json:"totalMemory"`
		Uptime      int64   `json:"uptime"`
	}
	totalMemory := int64(system.GetTotalSystemMemory())
	connector.Router.HandleFunc("/server/{id}", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		if !connector.Validate(w, r) {
			return
		}
		// Get the server being accessed.
		id := mux.Vars(r)["id"]
		process, err := connector.Processes.Get(id)
		// In case the server doesn't exist.
		if !err {
			http.Error(w, "{\"error\":\"This server does not exist!\"}", http.StatusNotFound)
			return
		}
		// POST /server/{id}
		if r.Method == "POST" {
			// Get the request body to check whether the operation is to START or STOP.
			var body bytes.Buffer
			_, err := body.ReadFrom(r.Body)
			if err != nil {
				http.Error(w, "{\"error\":\"Failed to read body!\"}", http.StatusBadRequest)
				return
			}
			operation := strings.ToUpper(body.String())
			// Check whether the operation is correct or not.
			if operation == "START" {
				// Start process if required.
				if process.Online != 1 {
					err = process.StartProcess()
				}
				// Send a response.
				res := make(map[string]bool)
				res["success"] = err == nil
				json.NewEncoder(w).Encode(res) // skipcq GSC-G104
			} else if operation == "STOP" {
				// Stop process if required.
				if process.Online == 1 {
					process.StopProcess()
				}
				// Send a response.
				res := make(map[string]bool)
				res["success"] = true
				json.NewEncoder(w).Encode(res) // skipcq GSC-G104
			} else {
				http.Error(w, "{\"error\":\"Invalid operation requested!\"}", http.StatusBadRequest)
				return
			}
			// GET /server/{id}
		} else if r.Method == "GET" {
			// Get the PID of the process.
			var stat system.ProcessStats
			if process.Command != nil && process.Command.Process != nil && process.Command.Process.Pid > 0 {
				// Get CPU usage and memory usage of the process.
				var err error
				stat, err = system.GetProcessStats(process.Command.Process.Pid)
				if err != nil {
					http.Error(w, "{\"error\":\"Internal Server Error! Is `ps` installed?\"}",
						http.StatusInternalServerError)
					return
				}
			}

			// Send a response.
			uptime := process.Uptime
			if uptime > 0 {
				uptime = time.Now().UnixNano() - process.Uptime
			}
			res := serverResponse{
				Status:      process.Online,
				Uptime:      uptime,
				CPUUsage:    stat.CPUUsage,
				MemoryUsage: stat.RSSMemory,
				TotalMemory: totalMemory,
			}
			json.NewEncoder(w).Encode(res) // skipcq GSC-G104
		} else {
			http.Error(w, "{\"error\":\"Only GET and POST is allowed!\"}", http.StatusMethodNotAllowed)
		}
	})

	// WS /server/{id}/console
	connector.Upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	connector.Router.HandleFunc("/server/{id}/console", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		t, e := connector.GetTicket(r.URL.Query().Get("ticket"))
		if e && t.IPAddr == GetIP(r) {
			connector.DeleteTicket(r.URL.Query().Get("ticket"))
		} else if !connector.Validate(w, r) {
			return
		}
		// Retrieve the token.
		token := r.Header.Get("Authorization")
		if e {
			token = t.Token
		} else if r.Header.Get("Cookie") != "" && token == "" {
			cookie, exists := r.Cookie("X-Authentication")
			if exists == nil {
				token = cookie.Value
			}
		}
		// Get the server being accessed.
		id := mux.Vars(r)["id"]
		process, exists := connector.Processes.Get(id)
		// In case the server doesn't exist.
		if !exists {
			http.Error(w, "{\"error\":\"This server does not exist!\"", http.StatusNotFound)
			return
		}
		// Upgrade WebSocket connection.
		c, err := connector.Upgrade(w, r, nil)
		if err == nil {
			defer c.Close()
			// Use a channel to synchronise all writes to the WebSocket.
			writeToWs := make(chan []byte, 8)
			defer close(writeToWs)
			go (func() {
				for {
					if data, ok := <-writeToWs; !ok {
						break
					} else {
						c.WriteMessage(websocket.TextMessage, data) // skipcq GSC-G104
					}
				}
			})()
			// Add connection to the process after sending current console output.
			(func() {
				process.ConsoleLock.RLock()
				defer process.ConsoleLock.RUnlock()
				writeToWs <- []byte(process.Console)

				process.ClientsLock.Lock()
				defer process.ClientsLock.Unlock()
				process.Clients[token] = writeToWs
			})()
			// Read messages from the user and execute them.
			for {
				var client chan []byte
				(func() { // Use inline function to be able to defer.
					process.ClientsLock.RLock()
					defer process.ClientsLock.RUnlock()
					client = process.Clients[token]
				})()
				// Another client has connected with the same token. Terminate existing connection.
				if client != writeToWs {
					break
				}
				// Read messages from the user.
				_, message, err := c.ReadMessage()
				if err != nil {
					process.ClientsLock.Lock()
					defer process.ClientsLock.Unlock()
					delete(process.Clients, token)
					break // The WebSocket connection has terminated.
				} else {
					process.SendCommand(string(message))
				}
			}
		}
	})
}

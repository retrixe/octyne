package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/puzpuzpuz/xsync/v2"
	"github.com/retrixe/octyne/auth"
	"github.com/retrixe/octyne/system"
)

// An internal representation of Process along with the clients connected to it and its output.
type managedProcess struct {
	*Process
	Clients     *xsync.MapOf[string, chan []byte]
	Console     string
	ConsoleLock sync.RWMutex
}

// Ticket is a one-time ticket usable by browsers to quickly authenticate with the WebSocket API.
type Ticket struct {
	Time   int64
	Token  string
	IPAddr string
}

// Connector is used to create an HTTP API for external apps to talk with octyne.
type Connector struct {
	auth.Authenticator
	*mux.Router
	*websocket.Upgrader
	Processes *xsync.MapOf[string, *managedProcess]
	Tickets   *xsync.MapOf[string, Ticket]
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
func InitializeConnector(config *Config) *Connector {
	// Create an authenticator.
	var authenticator auth.Authenticator
	if config.Redis.Enabled {
		authenticator = auth.NewRedisAuthenticator(config.Redis.URL)
	} else {
		authenticator = auth.NewMemoryAuthenticator()
	}
	// Create the connector.
	connector := &Connector{
		Router:        mux.NewRouter().StrictSlash(true),
		Processes:     xsync.NewMapOf[*managedProcess](),
		Tickets:       xsync.NewMapOf[Ticket](),
		Authenticator: &auth.ReplaceableAuthenticator{Engine: authenticator},
		Upgrader:      &websocket.Upgrader{Subprotocols: []string{"console-v2"}},
	}
	// Initialize all routes for the connector.
	/*
		All routes:
		GET /login
		GET /logout
		GET /ott (one-time ticket)

		GET /config/reload
		POST /accounts
		PATCH /accounts
		DELETE /accounts?username=username

		GET /servers

		GET /server/{id} (statistics like uptime, CPU and RAM)
		POST /server/{id} (to start and stop a server)

		WS /server/{id}/console?ticket=ticket

		GET /server/{id}/files?path=path
		GET /server/{id}/file?path=path&ticket=ticket
		POST /server/{id}/file?path=path (takes a form file with the file name, path= is path to folder)
		POST /server/{id}/folder?path=path
		DELETE /server/{id}/file?path=path
		PATCH /server/{id}/file (moving files, copying files and renaming them)

		POST /server/{id}/compress?path=path&compress=true/false (compress is optional, default: true)
		POST /server/{id}/decompress?path=path
	*/
	connector.registerMiscRoutes()
	connector.registerAuthRoutes()
	connector.registerFileRoutes()
	return connector
}

// AddProcess adds a process to the connector to be accessed via the HTTP API.
func (connector *Connector) AddProcess(proc *Process) {
	process := &managedProcess{
		Process: proc,
		Clients: xsync.NewMapOf[chan []byte](),
		Console: "",
	}
	connector.Processes.Store(process.Name, process)
	// Run a function which will monitor the console output of this process.
	go (func() {
		for {
			scanner := bufio.NewScanner(process.Output)
			scanner.Split(bufio.ScanLines)
			buf := make([]byte, 0, 64*1024) // Support up to 1 MB lines.
			scanner.Buffer(buf, 1024*1024)
			for scanner.Scan() {
				m := scanner.Text()
				// Truncate the console scrollback to 2500 to prevent excess memory usage and download cost.
				// TODO: These limits aren't exactly the best, it maxes up to 2.5 GB.
				(func() {
					process.ConsoleLock.Lock()
					defer process.ConsoleLock.Unlock()
					truncate := strings.Split(process.Console, "\n")
					if len(truncate) >= 2500 {
						process.Console = strings.Join(truncate[len(truncate)-2500:], "\n")
					}
					process.Console = process.Console + "\n" + m
					process.Clients.Range(func(key string, connection chan []byte) bool {
						connection <- []byte(m)
						return true
					})
				})()
			}
			log.Println("Error in " + process.Name + " console: " + scanner.Err().Error())
		}
	})()
}

func httpError(w http.ResponseWriter, error string, code int) {
	errorJson, err := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: error})
	if err == nil {
		http.Error(w, string(errorJson), code)
	} else {
		http.Error(w, "{\"error\": \"Internal Server Error!\"}", http.StatusInternalServerError)
	}
}

func (connector *Connector) registerMiscRoutes() {
	// GET /
	connector.Router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "{\"version\": \""+OctyneVersion+"\"}")
	})

	// GET /config/reload
	connector.Router.HandleFunc("/config/reload", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		if connector.Validate(w, r) == "" {
			return
		}
		// Read the new config.
		var config Config
		contents, err := os.ReadFile("config.json")
		if err != nil {
			log.Println("An error occurred while attempting to read config! " + err.Error())
			httpError(w, "An error occurred while reading config!", http.StatusInternalServerError)
			return
		}
		err = json.Unmarshal(contents, &config)
		if err != nil {
			log.Println("An error occurred while attempting to parse config! " + err.Error())
			httpError(w, "An error occurred while parsing config!", http.StatusInternalServerError)
			return
		}
		// Replace authenticator if changed. We are guaranteed that Authenticator is Replaceable.
		replaceableAuthenticator := connector.Authenticator.(*auth.ReplaceableAuthenticator)
		replaceableAuthenticator.EngineMutex.Lock()
		defer replaceableAuthenticator.EngineMutex.Unlock()
		redisAuthenticator, usingRedis := replaceableAuthenticator.Engine.(*auth.RedisAuthenticator)
		if usingRedis != config.Redis.Enabled ||
			(usingRedis && redisAuthenticator.URL != config.Redis.URL) {
			replaceableAuthenticator.Engine.Close() // Bypassing ReplaceableAuthenticator mutex Lock.
			if config.Redis.Enabled {
				replaceableAuthenticator.Engine = auth.NewRedisAuthenticator(config.Redis.URL)
			} else {
				replaceableAuthenticator.Engine = auth.NewMemoryAuthenticator()
			}
		}
		// Add new processes.
		for key := range config.Servers {
			if _, ok := connector.Processes.Load(key); !ok {
				go CreateProcess(key, config.Servers[key], connector)
			}
		}
		// Modify/remove existing processes.
		connector.Processes.Range(func(key string, value *managedProcess) bool {
			serverConfig, ok := config.Servers[key]
			if ok {
				value.Process.ServerConfigMutex.Lock()
				defer value.Process.ServerConfigMutex.Unlock()
				value.Process.ServerConfig = serverConfig
			} else {
				if value, loaded := connector.Processes.LoadAndDelete(key); loaded { // Yes, this is safe.
					value.KillProcess() // Other goroutines will cleanup.
					value.Clients.Range(func(key string, ws chan []byte) bool {
						ws <- nil
						return true
					})
					value.Clients.Clear()
				}
			}
			return true
		})
		// TODO: Reload HTTP server, mark server for deletion instead of instantly deleting them.
		// Send the response.
		fmt.Fprintln(w, "{\"success\":true}")
		info.Println("Config reloaded successfully!")
	})

	// GET /servers
	type serversResponse struct {
		Servers map[string]int `json:"servers"`
	}
	connector.Router.HandleFunc("/servers", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		if connector.Validate(w, r) == "" {
			return
		}
		// Get a map of processes and their online status.
		processes := make(map[string]int)
		connector.Processes.Range(func(k string, v *managedProcess) bool {
			processes[v.Name] = v.Online
			return true
		})
		// Send the list.
		json.NewEncoder(w).Encode(serversResponse{Servers: processes}) // skipcq GSC-G104
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
		// POST /server/{id}
		if r.Method == "POST" {
			// Get the request body to check whether the operation is to START or STOP.
			var body bytes.Buffer
			_, err := body.ReadFrom(r.Body)
			if err != nil {
				httpError(w, "Failed to read body!", http.StatusBadRequest)
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
			} else if operation == "STOP" || operation == "KILL" || operation == "TERM" {
				// Stop process if required.
				if process.Online == 1 {
					// Octyne 2.x should drop STOP or move it to SIGTERM.
					if operation == "KILL" || operation == "STOP" {
						process.KillProcess()
					} else {
						process.StopProcess()
					}
				}
				// Send a response.
				res := make(map[string]bool)
				res["success"] = true
				json.NewEncoder(w).Encode(res) // skipcq GSC-G104
			} else {
				httpError(w, "Invalid operation requested!", http.StatusBadRequest)
				return
			}
			// GET /server/{id}
		} else if r.Method == "GET" {
			// Get the PID of the process.
			var stat system.ProcessStats
			if process.Command != nil &&
				process.Command.Process != nil &&
				process.Command.ProcessState == nil &&
				process.Online == 1 {
				// Get CPU usage and memory usage of the process.
				var err error
				stat, err = system.GetProcessStats(process.Command.Process.Pid)
				if err != nil {
					log.Println("Failed to get server statistics for "+process.Name+"! Is ps available?", err)
					httpError(w, "Internal Server Error!", http.StatusInternalServerError)
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
			httpError(w, "Only GET and POST is allowed!", http.StatusMethodNotAllowed)
		}
	})

	// WS /server/{id}/console
	connector.Upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	connector.Router.HandleFunc("/server/{id}/console", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		ticket, ticketExists := connector.Tickets.LoadAndDelete(r.URL.Query().Get("ticket"))
		if !(ticketExists && ticket.IPAddr == GetIP(r)) && connector.Validate(w, r) == "" {
			return
		}
		// Retrieve the token.
		token := auth.GetTokenFromRequest(r)
		if ticketExists {
			token = ticket.Token
		}
		// Get the server being accessed.
		id := mux.Vars(r)["id"]
		process, exists := connector.Processes.Load(id)
		// In case the server doesn't exist.
		if !exists {
			httpError(w, "This server does not exist!", http.StatusNotFound)
			return
		}
		// Upgrade WebSocket connection.
		c, err := connector.Upgrade(w, r, nil)
		v2 := c.Subprotocol() == "console-v2"
		if err == nil {
			defer c.Close()
			// Setup deadlines.
			limit := 30 * time.Second
			c.SetReadLimit(1024 * 1024) // Limit WebSocket reads to 1 MB.
			c.SetReadDeadline(time.Now().Add(limit))
			c.SetPongHandler(func(string) error { c.SetReadDeadline(time.Now().Add(limit)); return nil })
			// If v2, send settings.
			if v2 {
				c.WriteJSON(struct { // skipcq GSC-G104
					Type string `json:"type"`
				}{"settings"})
			}
			// Use a channel to synchronise all writes to the WebSocket.
			writeToWs := make(chan []byte, 8)
			defer close(writeToWs)
			go (func() {
				for {
					data, ok := <-writeToWs
					if !ok {
						break
					} else if data == nil {
						c.Close()
						break
					}
					if v2 {
						c.WriteJSON(struct { // skipcq GSC-G104
							Type string `json:"type"`
							Data string `json:"data"`
						}{"output", string(data)})
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
				process.Clients.Store(token, writeToWs)
			})()
			// Read messages from the user and execute them.
			for {
				client, _ := process.Clients.Load(token)
				// Another client has connected with the same token. Terminate existing connection.
				if client != writeToWs {
					break
				}
				// Read messages from the user.
				_, message, err := c.ReadMessage()
				if err != nil {
					process.Clients.Delete(token)
					break // The WebSocket connection has terminated.
				}
				if v2 {
					var data map[string]string
					err := json.Unmarshal(message, &data)
					if err == nil {
						if data["type"] == "input" && data["data"] != "" {
							process.SendCommand(data["data"])
						} else if data["type"] == "ping" {
							c.WriteJSON(struct { // skipcq GSC-G104
								Type string `json:"type"`
								ID   string `json:"id"`
							}{"pong", data["id"]})
						} else {
							c.WriteJSON(struct { // skipcq GSC-G104
								Type    string `json:"type"`
								Message string `json:"message"`
							}{"error", "Invalid message type: " + data["type"]})
						}
					} else {
						c.WriteJSON(struct { // skipcq GSC-G104
							Type    string `json:"type"`
							Message string `json:"message"`
						}{"error", "Invalid message format"})
					}
				} else {
					process.SendCommand(string(message))
				}
			}
		}
	})
}

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/puzpuzpuz/xsync/v2"
	"github.com/retrixe/octyne/auth"
	"github.com/retrixe/octyne/system"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Logger handles file logging to zapLogger.
type Logger struct {
	Zap *zap.SugaredLogger
	LoggingConfig
	Lock sync.RWMutex
}

// Info calls the underlying zap.Logger if the action should be logged.
func (l *Logger) Info(action string, args ...interface{}) {
	l.Lock.RLock()
	defer l.Lock.RUnlock()
	if l.ShouldLog(action) {
		l.Zap.Infow("user performed action", append([]interface{}{"action", action}, args...)...)
	}
}

// CreateZapLogger creates a new zap.Logger instance.
func CreateZapLogger(config LoggingConfig) *zap.SugaredLogger {
	var w zapcore.WriteSyncer
	if config.Enabled {
		w = zapcore.AddSync(&lumberjack.Logger{
			Filename: filepath.Join(config.Path, "octyne.log"),
			MaxSize:  1, // megabytes
		})
	} else {
		w = zapcore.AddSync(io.Discard)
	}
	return zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), w, zap.InfoLevel)).Sugar()
}

// An internal representation of Process along with the clients connected to it and its output.
type managedProcess struct {
	*Process
	Clients     *xsync.MapOf[string, chan interface{}]
	Console     string
	ConsoleLock sync.RWMutex
}

// Ticket is a one-time ticket usable by browsers to quickly authenticate with the WebSocket API.
type Ticket struct {
	Time   int64
	User   string
	Token  string
	IPAddr string
}

// Connector is used to create an HTTP API for external apps to talk with octyne.
type Connector struct {
	auth.Authenticator
	*mux.Router
	*websocket.Upgrader
	*Logger
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
		authenticator = auth.NewRedisAuthenticator(UsersJsonPath, config.Redis.URL)
	} else {
		authenticator = auth.NewMemoryAuthenticator(UsersJsonPath)
	}
	// Create the connector.
	connector := &Connector{
		Router:        mux.NewRouter().StrictSlash(true),
		Logger:        &Logger{LoggingConfig: config.Logging, Zap: CreateZapLogger(config.Logging)},
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

		GET /config
		PATCH /config
		GET /config/reload

		GET /accounts
		POST /accounts
		PATCH /accounts?username=username (username is optional, will be required in v2)
		DELETE /accounts?username=username

		GET /servers?extrainfo=true/false

		GET /server/{id} (statistics like uptime, CPU and RAM)
		POST /server/{id} (to start and stop a server)

		WS /server/{id}/console?ticket=ticket (has console-v2 protocol)

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
		Clients: xsync.NewMapOf[chan interface{}](),
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
					process.Clients.Range(func(key string, connection chan interface{}) bool {
						connection <- m
						return true
					})
				})()
			}
			log.Println("Error in " + process.Name + " console: " + scanner.Err().Error())
		}
	})()
}

func httpError(w http.ResponseWriter, error string, code int) {
	w.Header().Set("content-type", "application/json")
	errorJson, err := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: error})
	if err == nil {
		http.Error(w, string(errorJson), code)
	} else {
		http.Error(w, "{\"error\": \"Internal Server Error!\"}", http.StatusInternalServerError)
	}
}

func writeJsonStringRes(w http.ResponseWriter, resp string) error {
	w.Header().Set("content-type", "application/json")
	_, err := fmt.Fprintln(w, resp)
	return err
}

func writeJsonStructRes(w http.ResponseWriter, resp interface{}) error {
	w.Header().Set("content-type", "application/json")
	return json.NewEncoder(w).Encode(resp)
}

// skipcq GO-R1005
func (connector *Connector) registerMiscRoutes() {
	// GET /
	connector.Router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJsonStringRes(w, "{\"version\": \""+OctyneVersion+"\"}")
	})

	// GET /config
	// PATCH /config
	connector.Router.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		user := connector.Validate(w, r)
		if user == "" {
			return
		} else if r.Method != "GET" && r.Method != "PATCH" {
			httpError(w, "Only GET and PATCH are allowed!", http.StatusMethodNotAllowed)
			return
		}
		if r.Method == "GET" {
			contents, err := os.ReadFile(ConfigJsonPath)
			if err != nil {
				log.Println("Error reading "+ConfigJsonPath+" when user accessed /config!", err)
				httpError(w, "Internal Server Error!", http.StatusInternalServerError)
				return
			}
			connector.Info("config.view", "ip", GetIP(r), "user", user)
			writeJsonStringRes(w, string(contents))
		} else if r.Method == "PATCH" {
			var buffer bytes.Buffer
			_, err := buffer.ReadFrom(r.Body)
			if err != nil {
				httpError(w, "Failed to read body!", http.StatusBadRequest)
				return
			}
			var origJson = buffer.String()
			var config Config
			contents, err := StripLineCommentsFromJSON(buffer.Bytes())
			if err != nil {
				httpError(w, "Invalid JSON body!", http.StatusBadRequest)
				return
			}
			err = json.Unmarshal(contents, &config)
			if err != nil {
				httpError(w, "Invalid JSON body!", http.StatusBadRequest)
				return
			}
			err = os.WriteFile(ConfigJsonPath+"~", []byte(strings.TrimRight(origJson, "\n")+"\n"), 0666)
			if err != nil {
				log.Println("Error writing to " + ConfigJsonPath + " when user modified config!")
				httpError(w, "Internal Server Error!", http.StatusInternalServerError)
				return
			}
			err = os.Rename(ConfigJsonPath+"~", ConfigJsonPath)
			if err != nil {
				log.Println("Error writing to " + ConfigJsonPath + " when user modified config!")
				httpError(w, "Internal Server Error!", http.StatusInternalServerError)
				return
			}
			connector.UpdateConfig(&config)
			connector.Info("config.edit", "ip", GetIP(r), "user", user, "newConfig", config)
			writeJsonStringRes(w, "{\"success\":true}")
			info.Println("Config updated remotely by user over HTTP API (see action logs for info)!")
		}
	})

	// GET /config/reload
	connector.Router.HandleFunc("/config/reload", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		user := connector.Validate(w, r)
		if user == "" {
			return
		}
		// Read the new config.
		var config Config
		contents, err := os.ReadFile(ConfigJsonPath)
		if err != nil {
			log.Println("An error occurred while attempting to read config! " + err.Error())
			httpError(w, "An error occurred while reading config!", http.StatusInternalServerError)
			return
		}
		contents, err = StripLineCommentsFromJSON(contents)
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
		// Reload the config.
		connector.UpdateConfig(&config)
		// Send the response.
		connector.Info("config.reload", "ip", GetIP(r), "user", user)
		writeJsonStringRes(w, "{\"success\":true}")
		info.Println("Config reloaded successfully!")
	})

	// GET /servers
	type serversResponse struct {
		Servers map[string]interface{} `json:"servers"`
	}
	connector.Router.HandleFunc("/servers", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		if connector.Validate(w, r) == "" {
			return
		}
		// Get a map of processes and their online status.
		processes := make(map[string]interface{})
		connector.Processes.Range(func(k string, v *managedProcess) bool {
			if r.URL.Query().Get("extrainfo") == "true" {
				processes[v.Name] = map[string]interface{}{
					"status":   v.Online.Load(),
					"toDelete": v.ToDelete.Load(),
				}
			} else {
				processes[v.Name] = v.Online.Load()
			}
			return true
		})
		// Send the list.
		writeJsonStructRes(w, serversResponse{Servers: processes}) // skipcq GSC-G104
	})

	// GET /server/{id}
	// POST /server/{id}
	type serverResponse struct {
		Status      int     `json:"status"`
		CPUUsage    float64 `json:"cpuUsage"`
		MemoryUsage float64 `json:"memoryUsage"`
		TotalMemory int64   `json:"totalMemory"`
		Uptime      int64   `json:"uptime"`
		ToDelete    bool    `json:"toDelete,omitempty"`
	}
	totalMemory := int64(system.GetTotalSystemMemory())
	connector.Router.HandleFunc("/server/{id}", func(w http.ResponseWriter, r *http.Request) {
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
				if process.Online.Load() != 1 {
					err = process.StartProcess(connector)
					connector.Info("server.start", "ip", GetIP(r), "user", user, "server", id)
				}
				// Send a response.
				res := make(map[string]bool)
				res["success"] = err == nil
				writeJsonStructRes(w, res) // skipcq GSC-G104
			} else if operation == "STOP" || operation == "KILL" || operation == "TERM" {
				// Stop process if required.
				if process.Online.Load() == 1 {
					// Octyne 2.x should drop STOP or move it to SIGTERM.
					if operation == "KILL" || operation == "STOP" {
						process.KillProcess()
						connector.Info("server.kill", "ip", GetIP(r), "user", user, "server", id)
					} else {
						process.StopProcess()
						connector.Info("server.stop", "ip", GetIP(r), "user", user, "server", id)
					}
				}
				// Send a response.
				res := make(map[string]bool)
				res["success"] = true
				writeJsonStructRes(w, res) // skipcq GSC-G104
			} else {
				httpError(w, "Invalid operation requested!", http.StatusBadRequest)
				return
			}
			// GET /server/{id}
		} else if r.Method == "GET" {
			// Get the PID of the process.
			var stat system.ProcessStats
			process.CommandMutex.RLock()
			defer process.CommandMutex.RUnlock()
			if process.Command != nil &&
				process.Command.Process != nil &&
				// Command.ProcessState == nil && // ProcessState isn't mutexed, the next if should suffice
				process.Online.Load() == 1 {
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
			uptime := process.Uptime.Load()
			if uptime > 0 {
				uptime = time.Now().UnixNano() - uptime
			}
			res := serverResponse{
				Status:      int(process.Online.Load()),
				Uptime:      uptime,
				CPUUsage:    stat.CPUUsage,
				MemoryUsage: stat.RSSMemory,
				TotalMemory: totalMemory,
				ToDelete:    process.ToDelete.Load(),
			}
			writeJsonStructRes(w, res) // skipcq GSC-G104
		} else {
			httpError(w, "Only GET and POST is allowed!", http.StatusMethodNotAllowed)
		}
	})

	// WS /server/{id}/console
	connector.Upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	connector.Router.HandleFunc("/server/{id}/console", func(w http.ResponseWriter, r *http.Request) {
		// Check with authenticator.
		ticket, ticketExists := connector.Tickets.LoadAndDelete(r.URL.Query().Get("ticket"))
		user := ""
		if ticketExists && ticket.IPAddr == GetIP(r) {
			user = ticket.User
		} else if user = connector.Validate(w, r); user == "" {
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
			connector.Info("server.console.access", "ip", GetIP(r), "user", user, "server", id)
			defer c.Close()
			// Setup WebSocket limits.
			timeout := 30 * time.Second
			c.SetReadLimit(1024 * 1024) // Limit WebSocket reads to 1 MB.
			// If v2, send settings and set read deadline.
			if v2 {
				c.SetReadDeadline(time.Now().Add(timeout))
				c.WriteJSON(struct { // skipcq GSC-G104
					Type string `json:"type"`
				}{"settings"})
			}
			// Use a channel to synchronise all writes to the WebSocket.
			writeChannel := make(chan interface{}, 8)
			defer close(writeChannel)
			go (func() {
				for {
					data, ok := <-writeChannel
					if !ok {
						break
					} else if data == nil {
						c.Close()
						break
					} else if _, ok := connector.Authenticator.GetUsers().Load(user); !ok && r.RemoteAddr != "@" {
						c.Close()
						break
					}
					c.SetWriteDeadline(time.Now().Add(timeout)) // Set write deadline esp for v1 connections.
					str, ok := data.(string)
					if ok && v2 {
						json, err := json.Marshal(struct {
							Type string `json:"type"`
							Data string `json:"data"`
						}{"output", str})
						if err != nil {
							log.Println("Error in "+process.Name+" console!", err)
						} else {
							c.WriteMessage(websocket.TextMessage, json) // skipcq GSC-G104
						}
					} else if ok {
						c.WriteMessage(websocket.TextMessage, []byte(str)) // skipcq GSC-G104
					} else {
						c.WriteMessage(websocket.TextMessage, data.([]byte)) // skipcq GSC-G104
					}
				}
			})()
			// Add connection to the process after sending current console output.
			(func() {
				process.ConsoleLock.RLock()
				defer process.ConsoleLock.RUnlock()
				writeChannel <- process.Console
				process.Clients.Store(token, writeChannel)
			})()
			// Read messages from the user and execute them.
			for {
				client, _ := process.Clients.Load(token)
				// Another client has connected with the same token. Terminate existing connection.
				if client != writeChannel {
					break
				}
				// Read messages from the user.
				_, message, err := c.ReadMessage()
				if err != nil {
					process.Clients.Delete(token)
					break // The WebSocket connection has terminated.
				} else if _, ok := connector.Authenticator.GetUsers().Load(user); !ok && r.RemoteAddr != "@" {
					process.Clients.Delete(token)
					c.Close()
					break
				}
				if v2 {
					c.SetReadDeadline(time.Now().Add(timeout)) // Update read deadline.
					var data map[string]string
					err := json.Unmarshal(message, &data)
					if err == nil {
						if data["type"] == "input" && data["data"] != "" {
							connector.Info("server.console.input", "ip", GetIP(r), "user", user, "server", id,
								"input", data["data"])
							process.SendCommand(data["data"])
						} else if data["type"] == "ping" {
							json, _ := json.Marshal(struct { // skipcq GSC-G104
								Type string `json:"type"`
								ID   string `json:"id"`
							}{"pong", data["id"]})
							writeChannel <- json
						} else {
							json, _ := json.Marshal(struct { // skipcq GSC-G104
								Type    string `json:"type"`
								Message string `json:"message"`
							}{"error", "Invalid message type: " + data["type"]})
							writeChannel <- json
						}
					} else {
						json, _ := json.Marshal(struct { // skipcq GSC-G104
							Type    string `json:"type"`
							Message string `json:"message"`
						}{"error", "Invalid message format"})
						writeChannel <- json
					}
				} else {
					connector.Info("server.console.input", "ip", GetIP(r), "user", user, "server", id,
						"input", string(message))
					process.SendCommand(string(message))
				}
			}
		}
	})
}

// UpdateConfig updates the connector with the new Config passed in arguments.
func (connector *Connector) UpdateConfig(config *Config) {
	// Update logged actions.
	func() {
		connector.Logger.Lock.Lock()
		defer connector.Logger.Lock.Unlock()
		connector.Logger.Zap.Sync()
		connector.Logger.Zap = CreateZapLogger(config.Logging)
		connector.Logger.LoggingConfig = config.Logging
	}()
	// Replace authenticator if changed. We are guaranteed that Authenticator is Replaceable.
	replaceableAuthenticator := connector.Authenticator.(*auth.ReplaceableAuthenticator)
	replaceableAuthenticator.EngineMutex.Lock()
	defer replaceableAuthenticator.EngineMutex.Unlock()
	redisAuthenticator, usingRedis := replaceableAuthenticator.Engine.(*auth.RedisAuthenticator)
	if usingRedis != config.Redis.Enabled ||
		(usingRedis && redisAuthenticator.URL != config.Redis.URL) {
		replaceableAuthenticator.Engine.Close() // Bypassing ReplaceableAuthenticator mutex Lock.
		if config.Redis.Enabled {
			replaceableAuthenticator.Engine = auth.NewRedisAuthenticator(UsersJsonPath, config.Redis.URL)
		} else {
			replaceableAuthenticator.Engine = auth.NewMemoryAuthenticator(UsersJsonPath)
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
			value.ToDelete.Swap(false)
		} else {
			value.ToDelete.Swap(true)
		}
		return true
	})
	// TODO: Reload HTTP/Unix socket server.
}

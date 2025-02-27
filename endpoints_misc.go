package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/retrixe/octyne/auth"
	"github.com/retrixe/octyne/system"
	"github.com/tailscale/hujson"
)

// GET /
func rootEndpoint(w http.ResponseWriter, _ *http.Request) {
	writeJsonStringRes(w, "{\"version\": \""+OctyneVersion+"\"}")
}

// GET /config
// PATCH /config
func configEndpoint(connector *Connector, w http.ResponseWriter, r *http.Request) {
	user := connector.ValidateAndReject(w, r)
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
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write(contents)
	} else if r.Method == "PATCH" {
		var buffer bytes.Buffer
		_, err := buffer.ReadFrom(r.Body)
		if err != nil {
			httpError(w, "Failed to read body!", http.StatusBadRequest)
			return
		}
		var origJson = buffer.String()
		var config Config
		contents, err := hujson.Standardize(buffer.Bytes())
		if err != nil {
			httpError(w, "Invalid JSON body!", http.StatusBadRequest)
			return
		}
		err = json.Unmarshal(contents, &config)
		if err != nil {
			httpError(w, "Invalid JSON body!", http.StatusBadRequest)
			return
		}
		err = os.WriteFile(ConfigJsonPath+"~", []byte(strings.TrimSpace(origJson)+"\n"), 0666)
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
}

// GET /config/reload
func configReloadEndpoint(connector *Connector, w http.ResponseWriter, r *http.Request) {
	// Check with authenticator.
	user := connector.ValidateAndReject(w, r)
	if user == "" {
		return
	}
	// Read the new config.
	config, err := ReadConfig()
	if err != nil {
		log.Println("An error occurred while attempting to read config! " + err.Error())
		httpError(w, "An error occurred while reading config!", http.StatusInternalServerError)
		return
	}
	// Reload the config.
	connector.UpdateConfig(&config)
	// Send the response.
	connector.Info("config.reload", "ip", GetIP(r), "user", user)
	writeJsonStringRes(w, "{\"success\":true}")
	info.Println("Config reloaded successfully!")
}

// GET /servers
type serversResponse struct {
	Servers map[string]interface{} `json:"servers"`
}

func serversEndpoint(connector *Connector, w http.ResponseWriter, r *http.Request) {
	// Check with authenticator.
	if connector.ValidateAndReject(w, r) == "" {
		return
	}
	// Get a map of processes and their online status.
	processes := make(map[string]interface{})
	connector.Processes.Range(func(_ string, v *ExposedProcess) bool {
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
}

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

var totalMemory = int64(system.GetTotalSystemMemory())

func serverEndpoint(connector *Connector, w http.ResponseWriter, r *http.Request) {
	// Check with authenticator.
	user := connector.ValidateAndReject(w, r)
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
	if r.Method == "GET" {
		serverEndpointGet(w, process)
	} else if r.Method == "POST" {
		serverEndpointPost(connector, w, r, process, id, user)
	} else {
		httpError(w, "Only GET and POST is allowed!", http.StatusMethodNotAllowed)
	}
}

func serverEndpointGet(w http.ResponseWriter, process *ExposedProcess) {
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
}

func serverEndpointPost(connector *Connector, w http.ResponseWriter, r *http.Request,
	process *ExposedProcess, id string, user string) {
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
}

// WS /server/{id}/console
type consoleError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type consoleData struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

type consolePing struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type consoleSettings struct {
	Type string `json:"type"`
}

func consoleEndpoint(connector *Connector, w http.ResponseWriter, r *http.Request) {
	// Get console protocol version.
	v2 := slices.Contains(websocket.Subprotocols(r), "console-v2")
	// Check with authenticator.
	ticket, ticketExists := connector.Tickets.LoadAndDelete(r.URL.Query().Get("ticket"))
	user := ""
	var userErr error = nil
	if ticketExists && ticket.IPAddr == GetIP(r) {
		user = ticket.User
	} else {
		user, userErr = connector.Authenticator.Validate(r)
	}
	if !v2 && userErr != nil {
		w.Header().Set("content-type", "application/json")
		http.Error(w, "{\"error\": \"Internal Server Error!\"}", http.StatusInternalServerError)
	} else if !v2 && user == "" {
		w.Header().Set("content-type", "application/json")
		http.Error(w, "{\"error\": \"You are not authenticated to access this resource!\"}",
			http.StatusUnauthorized)
	}
	// Retrieve the token.
	token := auth.GetTokenFromRequest(r)
	if ticketExists {
		token = ticket.Token
	}
	// Get the server being accessed.
	id := r.PathValue("id")
	process, exists := connector.Processes.Load(id)
	// In case the server doesn't exist.
	if !exists && !v2 {
		httpError(w, "This server does not exist!", http.StatusNotFound)
		return
	}
	// Upgrade WebSocket connection.
	c, err := connector.Upgrade(w, r, nil)
	if err == nil {
		if v2 {
			errStr, errNo := "", 0
			if !exists {
				errStr = "This server does not exist!"
				errNo = 4000 + http.StatusNotFound
			} else if userErr != nil {
				errStr = "Internal Server Error!"
				errNo = 4000 + http.StatusInternalServerError
			} else if user == "" {
				errStr = "You are not authenticated to access this resource!"
				errNo = 4000 + http.StatusUnauthorized
			}
			if errStr != "" {
				c.WriteJSON(consoleError{"error", errStr})
				c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(errNo, errStr))
				c.Close()
				return
			}
		}
		connector.Info("server.console.access", "ip", GetIP(r), "user", user, "server", id)
		defer c.Close()
		// Setup WebSocket limits.
		timeout := 30 * time.Second
		c.SetReadLimit(1024 * 1024) // Limit WebSocket reads to 1 MB.
		// If v2, send settings and set read deadline.
		if v2 {
			c.SetReadDeadline(time.Now().Add(timeout))
			c.WriteJSON(consoleSettings{"settings"})
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
					json, err := json.Marshal(consoleData{"output", str})
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
			process.Clients.Store(writeChannel, token)
		})()
		// Read messages from the user and execute them.
		for {
			_, ok := process.Clients.Load(writeChannel) // If gone, stop reading messages from client.
			if !ok {
				break
			}
			// Read messages from the user.
			_, message, err := c.ReadMessage()
			if err != nil {
				process.Clients.Delete(writeChannel)
				break // The WebSocket connection has terminated.
			} else if _, ok := connector.Authenticator.GetUsers().Load(user); !ok && r.RemoteAddr != "@" {
				process.Clients.Delete(writeChannel)
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
						json, _ := json.Marshal(consolePing{"pong", data["id"]})
						writeChannel <- json
					} else {
						json, _ := json.Marshal(consoleError{"error", "Invalid message type: " + data["type"]})
						writeChannel <- json
					}
				} else {
					json, _ := json.Marshal(consoleError{"error", "Invalid message format"})
					writeChannel <- json
				}
			} else {
				connector.Info("server.console.input", "ip", GetIP(r), "user", user, "server", id,
					"input", string(message))
				process.SendCommand(string(message))
			}
		}
	}
}

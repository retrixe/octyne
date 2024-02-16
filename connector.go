package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/retrixe/octyne/auth"
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

// ExposedProcess contains Process along with connected clients and cached output.
type ExposedProcess struct {
	*Process
	Clients     *xsync.MapOf[chan interface{}, string]
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
	*websocket.Upgrader
	*Logger
	Processes *xsync.MapOf[string, *ExposedProcess]
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

// WrapEndpointWithCtx provides Connector instances to HTTP endpoint handler functions.
func WrapEndpointWithCtx(
	connector *Connector,
	endpoint func(connector *Connector, w http.ResponseWriter, r *http.Request),
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { endpoint(connector, w, r) }
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
		Logger:        &Logger{LoggingConfig: config.Logging, Zap: CreateZapLogger(config.Logging)},
		Processes:     xsync.NewMapOf[string, *ExposedProcess](),
		Tickets:       xsync.NewMapOf[string, Ticket](),
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

	http.Handle("/login", WrapEndpointWithCtx(connector, loginEndpoint))
	http.Handle("/logout", WrapEndpointWithCtx(connector, logoutEndpoint))
	http.Handle("/ott", WrapEndpointWithCtx(connector, ottEndpoint))
	http.Handle("/accounts", WrapEndpointWithCtx(connector, accountsEndpoint))

	http.HandleFunc("/", rootEndpoint)
	http.Handle("/config", WrapEndpointWithCtx(connector, configEndpoint))
	http.Handle("/config/reload", WrapEndpointWithCtx(connector, configReloadEndpoint))
	http.Handle("/servers", WrapEndpointWithCtx(connector, serversEndpoint))
	http.Handle("/server/{id}", WrapEndpointWithCtx(connector, serverEndpoint))
	connector.Upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	http.Handle("/server/{id}/console", WrapEndpointWithCtx(connector, consoleEndpoint))

	http.Handle("/server/{id}/files", WrapEndpointWithCtx(connector, filesEndpoint))
	http.Handle("/server/{id}/file", WrapEndpointWithCtx(connector, fileEndpoint))
	http.Handle("/server/{id}/folder", WrapEndpointWithCtx(connector, folderEndpoint))
	http.Handle("/server/{id}/compress", WrapEndpointWithCtx(connector, compressionEndpoint))
	http.Handle("/server/{id}/compress/v2", WrapEndpointWithCtx(connector, compressionEndpoint))
	http.Handle("/server/{id}/decompress", WrapEndpointWithCtx(connector, decompressionEndpoint))
	return connector
}

// AddProcess adds a process to the connector to be accessed via the HTTP API.
func (connector *Connector) AddProcess(proc *Process) {
	process := &ExposedProcess{
		Process: proc,
		Clients: xsync.NewMapOf[chan interface{}, string](),
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
					process.Clients.Range(func(connection chan interface{}, token string) bool {
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
	connector.Processes.Range(func(key string, value *ExposedProcess) bool {
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

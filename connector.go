package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
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
	var err error
	if config.Redis.Enabled {
		authenticator, err =
			auth.NewRedisAuthenticator(config.Redis.Role, UsersJsonPath, config.Redis.URL)
	} else {
		authenticator, err = auth.NewMemoryAuthenticator(UsersJsonPath)
	}
	if err != nil {
		// skipcq RVV-A0003
		log.Fatalln("An error occurred while initializing the authenticator!", err)
	}
	// Create the connector.
	connector := &Connector{
		Logger:        &Logger{LoggingConfig: config.Logging, Zap: CreateZapLogger(config.Logging)},
		Processes:     xsync.NewMapOf[string, *ExposedProcess](),
		Tickets:       xsync.NewMapOf[string, Ticket](),
		Authenticator: &auth.ReplaceableAuthenticator{Engine: authenticator},
		Upgrader: &websocket.Upgrader{
			Subprotocols: []string{"console-v2"},
			CheckOrigin: func(_ *http.Request) bool {
				return true
			},
		},
	}
	return connector
}

// GetMux returns a new HTTP request multiplexer for the connector.
func (connector *Connector) GetMux(webUi bool) *http.ServeMux {
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

	prefix := ""
	mux := http.NewServeMux()

	// WebUI
	if webUi {
		prefix = "/api"
		mux.Handle("/", http.FileServer(ecthelionFileSystem{http.FS(Ecthelion)}))
	}

	mux.Handle(prefix+"/login", WrapEndpointWithCtx(connector, loginEndpoint))
	mux.Handle(prefix+"/logout", WrapEndpointWithCtx(connector, logoutEndpoint))
	mux.Handle(prefix+"/ott", WrapEndpointWithCtx(connector, ottEndpoint))
	mux.Handle(prefix+"/accounts", WrapEndpointWithCtx(connector, accountsEndpoint))

	mux.HandleFunc(prefix+"/", rootEndpoint)
	mux.Handle(prefix+"/config", WrapEndpointWithCtx(connector, configEndpoint))
	mux.Handle(prefix+"/config/reload", WrapEndpointWithCtx(connector, configReloadEndpoint))
	mux.Handle(prefix+"/servers", WrapEndpointWithCtx(connector, serversEndpoint))
	mux.Handle(prefix+"/server/{id}", WrapEndpointWithCtx(connector, serverEndpoint))
	mux.Handle(prefix+"/server/{id}/console", WrapEndpointWithCtx(connector, consoleEndpoint))

	mux.Handle(prefix+"/server/{id}/files", WrapEndpointWithCtx(connector, filesEndpoint))
	mux.Handle(prefix+"/server/{id}/file", WrapEndpointWithCtx(connector, fileEndpoint))
	mux.Handle(prefix+"/server/{id}/folder", WrapEndpointWithCtx(connector, folderEndpoint))
	mux.Handle(prefix+"/server/{id}/compress", WrapEndpointWithCtx(connector, compressionEndpoint))
	mux.Handle(prefix+"/server/{id}/compress/v2", WrapEndpointWithCtx(connector, compressionEndpoint))
	mux.Handle(prefix+"/server/{id}/decompress", WrapEndpointWithCtx(connector, decompressionEndpoint))
	return mux
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
					process.Clients.Range(func(connection chan interface{}, _ string) bool {
						connection <- m
						return true
					})
				})()
			}
			log.Println("Error in " + process.Name + " console: " + scanner.Err().Error())
		}
	})()
}

func httpError(w http.ResponseWriter, errMsg string, code int) {
	w.Header().Set("content-type", "application/json")
	errorJson, err := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: errMsg})
	if err == nil {
		http.Error(w, string(errorJson), code)
	} else {
		http.Error(w, "{\"error\": \"Internal Server Error!\"}", http.StatusInternalServerError)
	}
}

type ecthelionFileSystem struct {
	fs http.FileSystem
}

var ecthelionPathRegex = regexp.MustCompile(`(ecthelion\/out\/dashboard\/).+?([\/\.].*)`)

func (f ecthelionFileSystem) Open(name string) (http.File, error) {
	name = filepath.Join("ecthelion/out", name)
	if name != "ecthelion/out" && !strings.ContainsRune(name, '.') {
		name += ".html"
	}

	if strings.HasPrefix(name, "ecthelion/out/dashboard") {
		name = ecthelionPathRegex.ReplaceAllString(name, "$1[server]$2")
	}

	if strings.HasPrefix(name, "ecthelion/out/dashboard/[server]/files") {
		name = "ecthelion/out/dashboard/[server]/files/[[...path]].html"
	}

	return f.fs.Open(name)
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
		(usingRedis && redisAuthenticator.URL != config.Redis.URL) ||
		(usingRedis && redisAuthenticator.Role != config.Redis.Role) {
		var newAuthenticator auth.Authenticator
		var err error
		if config.Redis.Enabled {
			newAuthenticator, err =
				auth.NewRedisAuthenticator(config.Redis.Role, UsersJsonPath, config.Redis.URL)
		} else {
			newAuthenticator, err = auth.NewMemoryAuthenticator(UsersJsonPath)
		}
		if err != nil {
			// TODO: Propagate the error upwards to the user? Failed config reload should be reverted.
			log.Println("Failed to update authenticator during config reload!", err)
		} else {
			redisAuthenticator.Close()
			replaceableAuthenticator.Engine = newAuthenticator
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
	// TODO: Reload HTTP/WebUI/Unix socket server.
}

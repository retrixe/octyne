package main

import (
	"embed"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/handlers"
)

// OctyneVersion is the last version of Octyne this code is based on.
const OctyneVersion = "1.4.1"

// Embed the Web UI
//
//go:embed all:ecthelion/out/*
var Ecthelion embed.FS

func portToString(port uint16, defaultPort uint16) string {
	if port == 0 {
		return ":" + strconv.Itoa(int(defaultPort))
	}
	return ":" + strconv.Itoa(int(port))
}

var info *log.Logger
var ConfigJsonPath = "config.json"
var UsersJsonPath = "users.json"

func main() {
	for _, arg := range os.Args {
		if arg == "--help" || arg == "-h" {
			println("Usage: " + os.Args[0] + " [--version] [--config=<path>] [--users=<path>]")
			return
		} else if strings.HasPrefix(arg, "--users=") {
			UsersJsonPath = arg[8:]
		} else if strings.HasPrefix(arg, "--config=") {
			ConfigJsonPath = arg[9:]
		} else if arg == "--version" || arg == "-v" {
			println("octyne version " + OctyneVersion)
			return
		}
	}

	// Read config.
	config, err := ReadConfig()
	if err != nil {
		println("An error occurred while attempting to read config! " + err.Error())
		os.Exit(1)
	}

	// Setup logging.
	log.SetOutput(os.Stderr)
	log.SetPrefix("[Octyne] ")
	info = log.New(os.Stdout, "[Octyne] ", log.Flags())

	// Get a slice of server names.
	servers := make([]string, 0, len(config.Servers))
	for k := range config.Servers {
		servers = append(servers, k)
	}
	info.Println("Config read successfully!")

	// Setup daemon connector.
	connector := InitializeConnector(&config)
	exitCode := 1
	defer (func() {
		if err := connector.Authenticator.Close(); err != nil {
			log.Println("Error when closing the authenticator!", err)
		} else if err := connector.Logger.Zap.Sync(); err != nil {
			log.Println("Error when syncing the logger!", err)
		}
		// TODO: connector.Processes.Range(func(key string, value *ExposedProcess) bool { value.StopProcess() })
		os.Exit(exitCode)
	})()

	// Run processes, passing the daemon connector.
	for _, name := range servers {
		go CreateProcess(name, config.Servers[name], connector)
	}

	// Listen.
	apiPort := portToString(config.Port, defaultConfig.Port)
	webUiPort := portToString(config.WebUI.Port, defaultConfig.WebUI.Port)
	info.Println("Listening for API requests on port " + apiPort[1:])
	connector.Logger.Zap.Infow("started octyne", "port", config.Port)
	allowedHeaders := handlers.AllowedHeaders([]string{
		"X-Requested-With", "Content-Type", "Authorization", "Username", "Password",
	})
	allowedMethods := handlers.AllowedMethods([]string{
		"GET", "POST", "PUT", "HEAD", "OPTIONS", "PATCH", "DELETE",
	})
	allowedOrigins := handlers.AllowedOrigins([]string{"*"})
	apiHandler := handlers.CORS(allowedHeaders, allowedMethods, allowedOrigins)(connector.GetMux(false))
	webUiHandler := handlers.CORS(allowedHeaders, allowedMethods, allowedOrigins)(connector.GetMux(true))
	apiServer := &http.Server{
		Addr:              apiPort,
		Handler:           apiHandler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	webUiServer := &http.Server{
		Addr:              webUiPort,
		Handler:           webUiHandler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Begin listening on API port and Unix socket, then begin serving HTTP requests.
	apiListener, err := net.Listen("tcp", apiPort)
	if err != nil {
		log.Println("Error when listening on port "+apiPort+"!", err)
		return
	}
	if config.UnixSocket.Enabled {
		unixListener, err := listenOnUnixSocket(apiPort, config)
		if err != nil {
			return
		}
		go (func() {
			defer apiServer.Close()   // Close the API/Unix socket servers on failure.
			defer webUiServer.Close() // Close the Web UI server on failure.
			err = apiServer.Serve(unixListener)
			if err != nil && err != http.ErrServerClosed {
				log.Println("Error when serving Unix socket requests!", err)
			}
		})()
	}
	if config.WebUI.Enabled {
		webUiListener, err := net.Listen("tcp", webUiPort)
		if err != nil {
			log.Println("Error when listening on port "+webUiPort+"!", err)
			return
		}
		info.Println("Listening for Web UI requests on port " + webUiPort[1:])
		go (func() {
			defer apiServer.Close()   // Close the API/Unix socket servers on failure.
			defer webUiServer.Close() // Close the Web UI server on failure.
			err = webUiServer.Serve(webUiListener)
			if err != nil && err != http.ErrServerClosed {
				log.Println("Error when serving Web UI requests!", err)
			}
		})()
	}

	// Handle common process-killing signals so we can gracefully shut down:
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM) // os.KILL
	go func(c chan os.Signal) {
		sig := <-c
		log.Printf("Caught signal %s: shutting down.", sig)
		exitCode = 0
		apiServer.Close() // Close both servers, then call the defers in main()
	}(sigc)

	defer apiServer.Close()   // Close the API/Unix socket servers on failure.
	defer webUiServer.Close() // Close the Web UI server on failure.
	if !config.HTTPS.Enabled {
		err = apiServer.Serve(apiListener)
	} else {
		err = apiServer.ServeTLS(apiListener, config.HTTPS.Cert, config.HTTPS.Key)
	}
	if err != nil && err != http.ErrServerClosed {
		log.Println("Error when serving HTTP requests!", err) // skipcq: GO-S0904
	}
}

func listenOnUnixSocket(port string, config Config) (net.Listener, error) {
	loc := filepath.Join(os.TempDir(), "octyne.sock."+port[1:])
	if config.UnixSocket.Location != "" {
		loc = config.UnixSocket.Location
	}
	err := os.RemoveAll(loc)
	if err != nil {
		log.Println("Error when unlinking Unix socket at "+loc+"!", err)
		return nil, err
	}
	unixListener, err := net.Listen("unix", loc) // This unlinks the socket when closed by Serve().
	if err != nil {
		log.Println("Error when listening on Unix socket at "+loc+"!", err)
		return nil, err
	}
	if config.UnixSocket.Group != "" {
		if runtime.GOOS == "windows" {
			log.Println("Error: Assigning Unix sockets to groups is not supported on Windows!")
			return nil, err
		}
		group, err := user.LookupGroup(config.UnixSocket.Group)
		if err != nil {
			log.Println("Error when looking up Unix socket group owner: "+config.UnixSocket.Group, err)
			return nil, err
		}
		gid, err := strconv.Atoi(group.Gid)
		if err != nil {
			log.Println("Error when getting Unix socket group owner '"+config.UnixSocket.Group+"' GID!", err)
			return nil, err
		}
		err = os.Chown(loc, -1, gid)
		if err != nil {
			log.Println("Error when changing Unix socket group ownership to '"+config.UnixSocket.Group+"'!", err)
			return nil, err
		}
	}
	info.Println("Listening on Unix socket at " + loc)
	return unixListener, nil
}

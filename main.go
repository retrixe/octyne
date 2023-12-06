package main

import (
	"encoding/json"
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
const OctyneVersion = "1.2.0"

func getPort(config *Config) string {
	if config.Port == 0 {
		return ":42069"
	}
	return ":" + strconv.Itoa(int(config.Port))
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
	var config Config
	contents, err := os.ReadFile(ConfigJsonPath)
	if err != nil {
		panic("An error occurred while attempting to read config! " + err.Error())
	}
	contents, err = StripLineCommentsFromJSON(contents)
	if err != nil {
		panic("An error occurred while attempting to read config! " + err.Error())
	}
	err = json.Unmarshal(contents, &config)
	if err != nil {
		panic("An error occurred while attempting to read config! " + err.Error())
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
		// TODO: connector.Processes.Range(func(key string, value *managedProcess) bool { value.StopProcess() })
		os.Exit(exitCode)
	})()

	// Run processes, passing the daemon connector.
	for _, name := range servers {
		go CreateProcess(name, config.Servers[name], connector)
	}

	// Listen.
	port := getPort(&config)
	info.Println("Listening to port " + port[1:])
	connector.Logger.Zap.Infow("started octyne", "port", config.Port)
	handler := handlers.CORS(
		handlers.AllowedHeaders([]string{
			"X-Requested-With", "Content-Type", "Authorization", "Username", "Password",
		}),
		handlers.AllowedMethods([]string{"GET", "POST", "PUT", "HEAD", "OPTIONS", "PATCH", "DELETE"}),
		handlers.AllowedOrigins([]string{"*"}),
	)(connector.Router)
	server := &http.Server{
		Addr:              port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Begin listening on TCP and Unix socket, then begin serving HTTP requests.
	tcpListener, err := net.Listen("tcp", port)
	if err != nil {
		log.Println("Error when listening on port "+port+"!", err)
		return
	}
	if config.UnixSocket.Enabled {
		unixListener, err := ListenOnUnixSocket(port, config)
		if err != nil {
			return
		}
		go (func() {
			defer server.Close() // Close the TCP server if the Unix socket server fails.
			err = server.Serve(unixListener)
			if err != nil && err != http.ErrServerClosed {
				log.Println("Error when serving Unix socket requests!", err)
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
		server.Close() // Close both servers, then call the defers in main()
	}(sigc)

	defer server.Close() // Close the Unix socket server if the TCP server fails.
	if !config.HTTPS.Enabled {
		err = server.Serve(tcpListener)
	} else {
		err = server.ServeTLS(tcpListener, config.HTTPS.Cert, config.HTTPS.Key)
	}
	if err != nil && err != http.ErrServerClosed {
		log.Println("Error when serving HTTP requests!", err) // skipcq: GO-S0904
	}
}

func ListenOnUnixSocket(port string, config Config) (net.Listener, error) {
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
	return unixListener, nil
}

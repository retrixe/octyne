package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gorilla/handlers"
)

// OctyneVersion is the last version of Octyne this code is based on.
const OctyneVersion = "1.1.0"

func getPort(config *Config) string {
	if config.Port == 0 {
		return ":42069"
	}
	return ":" + strconv.Itoa(int(config.Port))
}

var info *log.Logger

func main() {
	if len(os.Args) >= 2 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		println("octyne version " + OctyneVersion)
		return
	}

	// Read config.
	var config Config
	contents, err := os.ReadFile("config.json")
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

	// Handle common process-killing signals so we can gracefully shut down:
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM) // os.KILL
	go func(c chan os.Signal) {
		sig := <-c
		log.Printf("Caught signal %s: shutting down.", sig)
		exitCode = 0
		server.Close() // Close the HTTP server, then call the defer in main()
	}(sigc)

	if !config.HTTPS.Enabled {
		err = server.ListenAndServe()
	} else {
		err = server.ListenAndServeTLS(config.HTTPS.Cert, config.HTTPS.Key)
	}
	if err != nil && err != http.ErrServerClosed {
		log.Println("Error when listening on port "+port+"!", err) // skipcq: GO-S0904
	}
}

package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gorilla/handlers"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// OctyneVersion is the last version of Octyne this code is based on.
const OctyneVersion = "1.0.2"

func getPort(config *Config) string {
	if config.Port == 0 {
		return ":42069"
	}
	return ":" + strconv.Itoa(int(config.Port))
}

var info *log.Logger
var logger *zap.Logger

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
	var w zapcore.WriteSyncer
	if config.Logging.Enabled {
		w = zapcore.AddSync(&lumberjack.Logger{
			Filename: filepath.Join(config.Logging.Path, "octyne.log"),
			MaxSize:  1, // megabytes
		})
	} else {
		w = zapcore.AddSync(io.Discard)
	}
	logger = zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), w, zap.InfoLevel))
	defer logger.Sync()
	defer os.Exit(1)

	// Get a slice of server names.
	servers := make([]string, 0, len(config.Servers))
	for k := range config.Servers {
		servers = append(servers, k)
	}
	info.Println("Config read successfully!")

	// Setup daemon connector.
	connector := InitializeConnector(&config)

	// Run processes, passing the daemon connector.
	for _, name := range servers {
		go CreateProcess(name, config.Servers[name], connector)
	}

	// Listen.
	port := getPort(&config)
	info.Println("Listening to port " + port[1:])
	logger.Sugar().Infow("started octyne", "port", config.Port)
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
	if !config.HTTPS.Enabled {
		err = server.ListenAndServe()
	} else {
		err = server.ListenAndServeTLS(config.HTTPS.Cert, config.HTTPS.Key)
	}
	// Close the authenticator.
	if authenticatorErr := connector.Authenticator.Close(); authenticatorErr != nil {
		log.Println("Error when closing the authenticator!", authenticatorErr)
	}
	log.Println(err) // skipcq: GO-S0904
}

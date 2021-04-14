package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/gorilla/handlers"
)

// OctyneVersion is the last version of Octyne this code is based on.
const OctyneVersion = "1.0.1"

func getPort(config Config) string {
	if config.Port == 0 {
		return ":42069"
	}
	return ":" + strconv.Itoa(int(config.Port))
}

func main() {
	if len(os.Args) >= 2 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		println("octyne version " + OctyneVersion)
		return
	}
	log.SetPrefix("[Octyne] ")

	// Read config.
	var config Config
	file, err := os.Open("config.json")
	if err != nil {
		panic("An error occurred while attempting to read config!\n" + err.Error())
	}
	contents, _ := ioutil.ReadAll(file)
	err = json.Unmarshal(contents, &config)
	if err != nil {
		panic("An error occurred while attempting to read config!\n" + err.Error())
	}

	// Get a slice of server names.
	servers := make([]string, 0, len(config.Servers))
	for k := range config.Servers {
		servers = append(servers, k)
	}
	log.Println("Config read successfully!")

	// Setup daemon connector.
	connector := InitializeConnector(config)
	// This defer never actually gets called, hence commented.
	// if connector.Authenticator.Redis != nil { defer connector.Authenticator.Redis.Close() }

	// Run processes, passing the daemon connector.
	for _, name := range servers {
		go CreateProcess(name, config.Servers[name], connector)
	}

	// Listen.
	port := getPort(config)
	log.Println("Listening to port " + port[1:])
	handler := handlers.CORS(
		handlers.AllowedHeaders([]string{
			"X-Requested-With", "Content-Type", "Authorization", "Username", "Password",
		}),
		handlers.AllowedMethods([]string{"GET", "POST", "PUT", "HEAD", "OPTIONS", "PATCH", "DELETE"}),
		handlers.AllowedOrigins([]string{"*"}),
	)(connector.Router)
	if !config.HTTPS.Enabled {
		err = http.ListenAndServe(port, handler)
	} else {
		err = http.ListenAndServeTLS(port, config.HTTPS.Cert, config.HTTPS.Key, handler)
	}
	redisAuthenticator, isRedisAuthenticator := connector.Authenticator.(*RedisAuthenticator)
	if isRedisAuthenticator && redisAuthenticator.Redis != nil { // Close Redis if needed.
		if redisErr := redisAuthenticator.Redis.Close(); redisErr != nil {
			println(redisErr)
		}
	}
	log.Fatalln(err)
}

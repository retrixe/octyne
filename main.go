package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/handlers"
)

// OctyneVersion ... Last version of Octyne this code is based on.
const OctyneVersion = "1.0.0-beta.1"

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "--version" {
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
	json.Unmarshal(contents, &config)

	// Get a slice of server names.
	servers := make([]string, 0, len(config.Servers))
	for k := range config.Servers {
		servers = append(servers, k)
	}
	log.Println("Config read successfully!")

	// Setup daemon connector.
	connector := InitializeConnector(config)

	// Run processes, passing the daemon connector.
	for _, name := range servers {
		go RunProcess(name, config.Servers[name], connector)
	}

	// Listen.
	log.Println("Listening to port 42069")
	log.Fatal(http.ListenAndServe(":42069", handlers.CORS(
		handlers.AllowedHeaders([]string{
			"X-Requested-With", "Content-Type", "Authorization", "Username", "Password",
		}),
		handlers.AllowedMethods([]string{"GET", "POST", "PUT", "HEAD", "OPTIONS", "PATCH", "DELETE"}),
		handlers.AllowedOrigins([]string{"*"}),
	)(connector.Router)))
	// TODO: Move above logic to connector.go
	// TODO: Add complete authentication logic with Redis support
	// TODO: Complete all routes
}

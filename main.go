package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/handlers"
)

func main() {
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
	/*
		err := http.ListenAndServe(":42069", connector.Router)
		if err != nil {
			log.Fatal(err)
		} else {
			log.Println("HTTP server listening successfully on port 42069!")
		}
	*/
	// TODO: Move above logic to connector.go
	// TODO: Add authentication logic
	// TODO: Authentication should support Redis
	// TODO: Complete process.go
	// TODO: Complete all routes
}

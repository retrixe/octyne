package auth

import (
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/puzpuzpuz/xsync/v2"
)

func CreateUserStore() *xsync.MapOf[string, string] {
	var users = xsync.NewMapOf[string]()
	initialFile, updates, err := readAndWatchFile("users.json")
	if err != nil {
		log.Println("An error occurred while attempting to read users.json! " + err.Error())
		return users
	}
	var usersJson map[string]string
	err = json.Unmarshal(initialFile, &usersJson)
	if err != nil {
		log.Println("An error occurred while attempting to parse users.json! " + err.Error())
		return users
	}
	for username, password := range usersJson {
		users.Store(username, password)
	}
	go (func() {
		for {
			newFile := <-updates
			var usersJson map[string]string
			err = json.Unmarshal(newFile, &usersJson)
			if err != nil {
				log.Println("An error occurred while attempting to parse users.json! " + err.Error())
				continue
			}
			for username, password := range usersJson {
				users.Store(username, password)
			}
			// Remove users that are no longer present.
			usersToRemove := make([]string, 0)
			users.Range(func(key string, value string) bool {
				if _, exists := usersJson[key]; !exists {
					usersToRemove = append(usersToRemove, key)
				}
				return true
			})
			for _, username := range usersToRemove {
				users.Delete(username)
			}
		}
	})()
	return users
}

func readAndWatchFile(filePath string) ([]byte, chan []byte, error) {
	file, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, err
	}
	channel := make(chan []byte, 1)
	go (func() {
		for {
			time.Sleep(1 * time.Second)
			newFile, err := os.ReadFile(filePath)
			if err != nil {
				log.Println("An error occurred while attempting to read users.json! " + err.Error())
				continue
			}
			if string(newFile) != string(file) {
				file = newFile
				channel <- newFile
			}
		}
	})()
	return file, channel, nil
}

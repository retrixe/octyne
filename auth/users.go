package auth

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/puzpuzpuz/xsync/v3"
)

func CreateUserStore(usersJsonPath string) *xsync.MapOf[string, string] {
	// Create default users.json file
	_, err := os.Stat(usersJsonPath)
	if os.IsNotExist(err) {
		passwordBytes := make([]byte, 12)
		rand.Read(passwordBytes) // Tolerate errors here, an error here is incredibly unlikely: skipcq GSC-G104
		password := base64.RawStdEncoding.EncodeToString(passwordBytes)
		hash := HashPassword(password)
		err = os.WriteFile(usersJsonPath, []byte("{\n  \"admin\": \""+hash+"\"\n}"), 0644)
		if err != nil {
			log.Println("An error occurred while creating " + usersJsonPath + "! " + err.Error())
		}
		log.Println("The file " + usersJsonPath +
			" has been generated with the default user 'admin' and password '" + password + "'.")
	}

	var users = xsync.NewMapOf[string, string]()
	initialFile, updates, err := readAndWatchFile(usersJsonPath)
	if err != nil {
		log.Println("An error occurred while reading " + usersJsonPath + "! " + err.Error())
		return users
	}
	var usersJson map[string]string
	err = json.Unmarshal(initialFile, &usersJson)
	if err != nil {
		log.Println("An error occurred while parsing " + usersJsonPath + "! " + err.Error())
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
				log.Println("An error occurred while parsing " + usersJsonPath + "! " + err.Error())
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
				log.Println("An error occurred while reading " + filePath + "! " + err.Error())
				continue
			}
			if !bytes.Equal(newFile, file) {
				file = newFile
				channel <- newFile
			}
		}
	})()
	return file, channel, nil
}

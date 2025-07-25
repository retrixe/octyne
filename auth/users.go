package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"regexp"

	"github.com/retrixe/octyne/system"
)

var validUsernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_@]+$`)

func ValidateUsername(username string) string {
	if username == "@local" {
		return "The username '@local' is reserved for local system users."
	} else if !validUsernameRegex.MatchString(username) {
		return "The username '" + username + "' is invalid." +
			" A valid username can contain only letters, numbers, _ or @."
	}
	return ""
}

func readAndWatchUsers(usersJsonPath string) (<-chan map[string]string, context.CancelFunc) {
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
		} else {
			log.Println("The file " + usersJsonPath +
				" has been generated with the default user 'admin' and password '" + password + "'.")
		}
	}

	fileUpdates, cancel, err := system.ReadAndWatchFile(usersJsonPath)
	if err != nil {
		// skipcq RVV-A0003
		// panic here, as this is critical for authenticator and we don't want to continue without it
		log.Panicln("An error occurred while reading " + usersJsonPath + "! " + err.Error())
	}
	userChannel := make(chan map[string]string, 1)
	go (func() {
		for {
			newFile, ok := <-fileUpdates
			if !ok {
				return
			}
			var usersJson map[string]string
			err = json.Unmarshal(newFile, &usersJson)
			if err != nil {
				log.Println("An error occurred while parsing " + usersJsonPath + "! " + err.Error())
				continue
			}
			userChannel <- usersJson
		}
	})()
	return userChannel, cancel
}

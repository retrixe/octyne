package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"regexp"

	"github.com/puzpuzpuz/xsync/v3"
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
		} else {
			log.Println("The file " + usersJsonPath +
				" has been generated with the default user 'admin' and password '" + password + "'.")
		}
	}

	users := xsync.NewMapOf[string, string]()
	initialFile, updates, err := system.ReadAndWatchFile(usersJsonPath)
	if err != nil {
		log.Panicln("An error occurred while reading " + usersJsonPath + "! " + err.Error())
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
			UpdateUserStoreFromMap(users, usersJson)
		}
	})()
	var usersJson map[string]string
	err = json.Unmarshal(initialFile, &usersJson)
	if err != nil {
		log.Println("An error occurred while parsing " + usersJsonPath + "! " + err.Error())
		return users
	}
	UpdateUserStoreFromMap(users, usersJson)
	return users
}

func UpdateUserStoreFromMap(users *xsync.MapOf[string, string], userMap map[string]string) error {
	users.Clear() // Clear all pre-existing users
	for username, password := range userMap {
		if msg := ValidateUsername(username); msg == "" {
			users.Store(username, password)
		} else {
			log.Println(msg + " This account will be ignored and eventually removed!")
		}
	}
	return nil
}

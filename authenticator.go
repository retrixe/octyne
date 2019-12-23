package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
)

// Authenticator ... Used by Octyne's Connector to provide authentication to the HTTP API.
type Authenticator struct {
	Config
	Tokens []string
}

// InitializeAuthenticator ... Initialize an authenticator.
func InitializeAuthenticator(config Config) *Authenticator {
	// Create the connector.
	connector := &Authenticator{
		Config: config,
		Tokens: make([]string, 0),
	}
	return connector
}

// Validate ... Called on an HTTP API execution and checks whether the user is authenticated or not.
// Stores tokens in Redis if required.
func (a *Authenticator) Validate(w http.ResponseWriter, r *http.Request) bool {
	token := r.Header.Get("Authorization")
	// For WebSockets, special case.
	if r.Header.Get("Cookie") != "" && token == "" {
		cookie, exists := r.Cookie("X-Authentication")
		if exists == nil {
			token = cookie.Value
		}
	}
	// If valid, return true.
	for _, value := range a.Tokens {
		if value == token && value != "" {
			return true
		}
	}
	http.Error(w, "{\"error\": \"You are not authenticated to access this resource!\"}", 401)
	return false
}

// Login ... Allows logging in a user and returning the token.
func (a *Authenticator) Login(username string, password string) string {
	// Hash the password.
	sha256sum := fmt.Sprintf("%x", sha256.Sum256([]byte(password)))
	// Read users.json and check whether a user with such a username and password exists.
	var users map[string]string
	file, err := os.Open("users.json")
	if err != nil {
		panic("An error occurred while attempting to read config!\n" + err.Error())
	}
	contents, _ := ioutil.ReadAll(file)
	json.Unmarshal(contents, &users)
	// Check whether this user exists.
	hashedPassword, exists := users[username]
	if !exists || hashedPassword != sha256sum {
		return ""
	}
	// Generate a token and return it.
	token := make([]byte, 96)
	rand.Read(token)
	a.Tokens = append(a.Tokens, base64.StdEncoding.EncodeToString(token))
	return base64.StdEncoding.EncodeToString(token)
}

// Logout ... Allows logging out of a user and deleting the token from the server.
func (a *Authenticator) Logout(token string) bool {
	for i, t := range a.Tokens {
		if t == token {
			a.Tokens = append(a.Tokens[:i], a.Tokens[i+1:]...)
			return true
		}
	}
	return false
}

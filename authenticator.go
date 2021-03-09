package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gomodule/redigo/redis"
)

// TODO: An interface would be better, implemented by two authenticators.

// Authenticator is used by Octyne's Connector to provide HTTP API authentication.
type Authenticator struct {
	Config
	Redis  *redis.Pool
	Tokens []string
}

// InitializeAuthenticator initializes an authenticator.
func InitializeAuthenticator(config Config) *Authenticator {
	// If Redis, create a Redis connection.
	if config.Redis.Enabled {
		return InitializeRedisAuthenticator(config)
	}
	// Create the connector.
	connector := &Authenticator{
		Config: config,
		Tokens: make([]string, 0),
	}
	return connector
}

// InitializeRedisAuthenticator initializes an authenticator using Redis.
func InitializeRedisAuthenticator(config Config) *Authenticator {
	pool := &redis.Pool{
		MaxIdle:   5,
		MaxActive: 5,
		Dial: func() (redis.Conn, error) {
			conn, err := redis.DialURL(config.Redis.URL, redis.DialConnectTimeout(time.Second*60))
			if err != nil {
				log.Println("An error occurred when trying to connect to Redis!", err)
			}
			return conn, err
		},
	}
	authenticator := &Authenticator{Config: config, Redis: pool}
	return authenticator
}

// Validate is called on an HTTP API request and checks whether or not the user is authenticated.
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
	// If Redis, make request to Redis database.
	if a.Redis != nil {
		conn := a.Redis.Get()
		defer conn.Close()
		res, err := redis.Int(conn.Do("EXISTS", token))
		if err != nil {
			log.Println("An error occurred while making a request to Redis!", err)
			return false
		}
		return res == 1
	}
	// If valid, return true.
	for _, value := range a.Tokens {
		if value == token && value != "" {
			return true
		}
	}
	http.Error(w, "{\"error\": \"You are not authenticated to access this resource!\"}",
		http.StatusUnauthorized)
	return false
}

// Login allows logging in a user and returning the token.
func (a *Authenticator) Login(username string, password string) string {
	// Hash the password.
	sha256sum := fmt.Sprintf("%x", sha256.Sum256([]byte(password)))
	// Read users.json and check whether a user with such a username and password exists.
	var users map[string]string
	file, err := os.Open("users.json")
	if err != nil {
		log.Println("An error occurred while attempting to read users.json!\n" + err.Error())
	}
	contents, _ := ioutil.ReadAll(file)
	json.Unmarshal(contents, &users) // Tolerate errors here: skipcq GSC-G104
	// Check whether this user exists.
	hashedPassword, exists := users[username]
	if !exists || hashedPassword != sha256sum {
		return ""
	}
	// Generate a token and return it.
	token := make([]byte, 96)
	rand.Read(token) // Tolerate errors here, an error here is incredibly unlikely: skipcq GSC-G104
	if a.Redis != nil {
		conn := a.Redis.Get()
		defer conn.Close()
		_, err := conn.Do("SET", base64.StdEncoding.EncodeToString(token), username)
		if err != nil {
			log.Println("An error occurred while making a request to Redis for login!", err)
		}
	} else {
		a.Tokens = append(a.Tokens, base64.StdEncoding.EncodeToString(token))
	}
	return base64.StdEncoding.EncodeToString(token)
}

// Logout allows logging out of a user and deleting the token from the server.
func (a *Authenticator) Logout(token string) bool {
	if a.Redis != nil {
		conn := a.Redis.Get()
		defer conn.Close()
		res, err := redis.Int(conn.Do("DEL", token))
		if err != nil {
			log.Println("An error occurred while making a request to Redis for logout!", err)
			return false
		}
		return res == 1
	}
	for i, t := range a.Tokens {
		if t == token {
			a.Tokens = append(a.Tokens[:i], a.Tokens[i+1:]...)
			return true
		}
	}
	return false
}

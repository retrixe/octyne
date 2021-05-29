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
	"sync"
	"time"

	"github.com/gomodule/redigo/redis"
)

// Authenticator is used by Octyne's Connector to provide HTTP API authentication.
type Authenticator interface {
	// Validate is called on an HTTP API request and checks whether or not the user is authenticated.
	Validate(w http.ResponseWriter, r *http.Request) bool
	// Login allows logging in a user and returning the token.
	Login(username string, password string) string
	// Logout allows logging out of a user and deleting the token from the server.
	Logout(token string) bool
}

// RedisAuthenticator is an Authenticator implementation using Redis to store tokens.
type RedisAuthenticator struct {
	Config
	Redis *redis.Pool
}

// MemoryAuthenticator is an Authenticator implementation using an array to store tokens.
type MemoryAuthenticator struct {
	Config
	TokenMutex sync.RWMutex
	Tokens     []string
}

// InitializeAuthenticator initializes an authenticator.
func InitializeAuthenticator(config Config) Authenticator {
	// If Redis, create a Redis connection.
	if config.Redis.Enabled {
		return InitializeRedisAuthenticator(config)
	}
	// Create the authenticator.
	authenticator := &MemoryAuthenticator{
		Config: config,
		Tokens: make([]string, 0),
	}
	return authenticator
}

// InitializeRedisAuthenticator initializes an authenticator using Redis.
func InitializeRedisAuthenticator(config Config) Authenticator {
	pool := &redis.Pool{
		Wait:      true,
		MaxIdle:   5,
		MaxActive: 5,
		Dial: func() (redis.Conn, error) {
			conn, err := redis.DialURL(config.Redis.URL, redis.DialConnectTimeout(time.Second*60))
			if err != nil {
				log.Println("An error occurred when trying to connect to Redis!", err) // skipcq: GO-S0904
			}
			return conn, err
		},
	}
	authenticator := &RedisAuthenticator{Config: config, Redis: pool}
	return authenticator
}

func getTokenFromRequest(r *http.Request) string {
	token := r.Header.Get("Authorization")
	// For WebSockets, special case.
	if r.Header.Get("Cookie") != "" && token == "" {
		cookie, exists := r.Cookie("X-Authentication")
		if exists == nil {
			token = cookie.Value
		}
	}
	return token
}

func isValidToken(token string) bool {
	_, err := base64.StdEncoding.DecodeString(token)
	return err == nil && len(token) == 128
}

// Validate is called on an HTTP API request and checks whether or not the user is authenticated.
func (a *MemoryAuthenticator) Validate(w http.ResponseWriter, r *http.Request) bool {
	token := getTokenFromRequest(r)
	if !isValidToken(token) {
		http.Error(w, "{\"error\": \"You are not authenticated to access this resource!\"}",
			http.StatusUnauthorized)
	}
	// If valid, return true.
	a.TokenMutex.RLock()
	defer a.TokenMutex.RUnlock()
	for _, value := range a.Tokens {
		if value == token && value != "" {
			return true
		}
	}
	http.Error(w, "{\"error\": \"You are not authenticated to access this resource!\"}",
		http.StatusUnauthorized)
	return false
}

// Validate is called on an HTTP API request and checks whether or not the user is authenticated.
func (a *RedisAuthenticator) Validate(w http.ResponseWriter, r *http.Request) bool {
	token := getTokenFromRequest(r)
	if !isValidToken(token) {
		http.Error(w, "{\"error\": \"You are not authenticated to access this resource!\"}",
			http.StatusUnauthorized)
	}
	// Make request to Redis database.
	conn := a.Redis.Get()
	defer conn.Close()
	res, err := redis.Int(conn.Do("EXISTS", "octyne-token:"+token))
	if err != nil {
		log.Println("An error occurred while making a request to Redis!", err) // skipcq: GO-S0904
		http.Error(w, "{\"error\": \"Internal Server Error!\"}", http.StatusInternalServerError)
		return false
	}
	if res != 1 {
		http.Error(w, "{\"error\": \"You are not authenticated to access this resource!\"}",
			http.StatusUnauthorized)
	}
	return res == 1
}

func checkValidLoginAndGenerateToken(username string, password string) string {
	// Hash the password.
	sha256sum := fmt.Sprintf("%x", sha256.Sum256([]byte(password)))
	// Read users.json and check whether a user with such a username and password exists.
	var users map[string]string
	contents, err := ioutil.ReadFile("users.json")
	if err != nil {
		log.Println("An error occurred while attempting to read users.json!\n" + err.Error())
		return ""
	}
	err = json.Unmarshal(contents, &users)
	if err != nil {
		log.Println("An error occurred while attempting to read users.json!\n" + err.Error())
		return ""
	}
	// Check whether this user exists.
	hashedPassword, exists := users[username]
	if !exists || hashedPassword != sha256sum {
		return ""
	}
	// Generate a token and return it.
	token := make([]byte, 96)
	rand.Read(token) // Tolerate errors here, an error here is incredibly unlikely: skipcq GSC-G104
	return base64.StdEncoding.EncodeToString(token)
}

// Login allows logging in a user and returning the token.
func (a *MemoryAuthenticator) Login(username string, password string) string {
	token := checkValidLoginAndGenerateToken(username, password)
	if token == "" {
		return ""
	}
	a.TokenMutex.Lock()
	defer a.TokenMutex.Unlock()
	a.Tokens = append(a.Tokens, token)
	return token
}

// Login allows logging in a user and returning the token.
func (a *RedisAuthenticator) Login(username string, password string) string {
	token := checkValidLoginAndGenerateToken(username, password)
	if token == "" {
		return ""
	}
	conn := a.Redis.Get()
	defer conn.Close()
	_, err := conn.Do("SET", "octyne-token:"+token, username)
	if err != nil {
		log.Println("An error occurred while making a request to Redis for login!", err) // skipcq: GO-S0904
	}
	return token
}

// Logout allows logging out of a user and deleting the token from the server.
func (a *MemoryAuthenticator) Logout(token string) bool {
	if !isValidToken(token) {
		return false
	}
	a.TokenMutex.RLock()
	defer a.TokenMutex.RUnlock()
	for i, t := range a.Tokens {
		if t == token {
			a.TokenMutex.Lock()
			defer a.TokenMutex.Unlock()
			a.Tokens = append(a.Tokens[:i], a.Tokens[i+1:]...)
			return true
		}
	}
	return false
}

// Logout allows logging out of a user and deleting the token from the server.
func (a *RedisAuthenticator) Logout(token string) bool {
	if !isValidToken(token) {
		return false
	}
	conn := a.Redis.Get()
	defer conn.Close()
	res, err := redis.Int(conn.Do("DEL", "octyne-token:"+token))
	if err != nil {
		log.Println("An error occurred while making a request to Redis for logout!", err) // skipcq: GO-S0904
		return false
	}
	return res == 1
}

package auth

import (
	"log"
	"net/http"
	"time"

	"github.com/gomodule/redigo/redis"
)

// RedisAuthenticator is an Authenticator implementation using Redis to store tokens.
type RedisAuthenticator struct {
	Redis *redis.Pool
	URL   string
}

// NewRedisAuthenticator initializes an authenticator using Redis for token storage.
func NewRedisAuthenticator(url string) *RedisAuthenticator {
	pool := &redis.Pool{
		Wait:      true,
		MaxIdle:   5,
		MaxActive: 5,
		Dial: func() (redis.Conn, error) {
			conn, err := redis.DialURL(url, redis.DialConnectTimeout(time.Second*60))
			if err != nil {
				log.Println("An error occurred when trying to connect to Redis!", err) // skipcq: GO-S0904
			}
			return conn, err
		},
	}
	return &RedisAuthenticator{Redis: pool, URL: url}
}

// Validate is called on an HTTP API request and checks whether or not the user is authenticated.
func (a *RedisAuthenticator) Validate(w http.ResponseWriter, r *http.Request) bool {
	token := getTokenFromRequest(r)
	if !isValidToken(token) {
		http.Error(w, "{\"error\": \"You are not authenticated to access this resource!\"}",
			http.StatusUnauthorized)
		return false
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

// Close closes the authenticator. Once closed, the authenticator should not be used.
func (a *RedisAuthenticator) Close() error {
	return a.Redis.Close()
}

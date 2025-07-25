package auth

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gomodule/redigo/redis"
)

// RedisAuthenticator is an Authenticator implementation using Redis to store tokens.
type RedisAuthenticator struct {
	stopUserUpdates context.CancelFunc
	Redis           *redis.Pool
	URL             string
}

// NewRedisAuthenticator initializes an authenticator using Redis for token storage.
func NewRedisAuthenticator(usersJsonPath string, url string) *RedisAuthenticator {
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
	userUpdates, stopUserUpdates := readAndWatchUsers(usersJsonPath)
	go (func() {
		for {
			newUsers, ok := <-userUpdates
			if !ok {
				return
			}
			(func() {
				conn := pool.Get()
				defer conn.Close()
				// Get current users from Redis and remove them if they are not in the newUsers map
				currentUsers, err := redis.Strings(conn.Do("KEYS", "octyne-user:*"))
				if err != nil {
					log.Println("An error occurred while fetching current users from Redis!", err)
				}
				for _, userKey := range currentUsers {
					username := userKey[len("octyne-user:"):]
					if _, exists := newUsers[username]; !exists {
						if _, err := conn.Do("DEL", userKey); err != nil {
							log.Println("An error occurred while deleting user '"+username+"' from Redis!", err)
						}
					}
				}
				// Upsert new users into Redis
				for username, password := range newUsers {
					if msg := ValidateUsername(username); msg == "" {
						_, err := conn.Do("SET", "octyne-user:"+username, password)
						if err != nil {
							log.Println("An error occurred while updating user '"+username+"' in Redis!", err)
						}
					} else {
						log.Println(msg + " This account will be ignored and eventually removed!")
					}
				}
			})()
		}
	})()
	return &RedisAuthenticator{Redis: pool, URL: url, stopUserUpdates: stopUserUpdates}
}

// GetUser returns info about the user with the given username.
// Currently, it returns the password hash of the user.
//
// If the user does not exist, it returns ErrUserNotFound.
func (a *RedisAuthenticator) GetUser(username string) (string, error) {
	conn := a.Redis.Get()
	defer conn.Close()
	user, err := redis.String(conn.Do("GET", "octyne-user:"+username))
	if err != nil {
		if errors.Is(err, redis.ErrNil) {
			return "", ErrUserNotFound
		}
		return "", err
	}
	return user, nil
}

// Validate is called on an HTTP API request and returns the username if request is authenticated,
// else returns an empty string.
func (a *RedisAuthenticator) Validate(r *http.Request) (string, error) {
	if r.RemoteAddr == "@" {
		return "@local", nil
	}

	token := GetTokenFromRequest(r)
	if !isValidToken(token) {
		return "", nil
	}
	// Make request to Redis database.
	conn := a.Redis.Get()
	defer conn.Close()
	username, err := redis.String(conn.Do("GET", "octyne-token:"+token))
	if err == nil {
		if _, err := a.GetUser(username); err == nil {
			return username, nil
		} else if !errors.Is(err, ErrUserNotFound) {
			return "", err
		}
		a.Logout(token)
	} else if !errors.Is(err, redis.ErrNil) {
		return "", err
	}
	return "", nil
}

// ValidateAndReject is called on an HTTP API request and returns the username if request
// is authenticated, else the request is rejected.
func (a *RedisAuthenticator) ValidateAndReject(w http.ResponseWriter, r *http.Request) string {
	username, err := a.Validate(r)
	if err != nil {
		w.Header().Set("content-type", "application/json")
		log.Println("An error occurred while validating authorization for an HTTP request!", err)
		http.Error(w, "{\"error\": \"Internal Server Error!\"}", http.StatusInternalServerError)
		return ""
	} else if username == "" {
		w.Header().Set("content-type", "application/json")
		http.Error(w, "{\"error\": \"You are not authenticated to access this resource!\"}",
			http.StatusUnauthorized)
		return ""
	}
	return username
}

// Login allows logging in a user and returning the token.
// It returns an empty string if the username or password are invalid.
func (a *RedisAuthenticator) Login(username string, password string) (string, error) {
	token, err := checkValidLoginAndGenerateToken(a, username, password)
	if err != nil {
		return "", err
	} else if token == "" {
		return "", nil
	}
	conn := a.Redis.Get()
	defer conn.Close()
	_, err = conn.Do("SET", "octyne-token:"+token, username)
	if err != nil {
		return "", err
	}
	return token, nil
}

// Logout allows logging out of a user and deleting the token from the server.
func (a *RedisAuthenticator) Logout(token string) (bool, error) {
	if !isValidToken(token) {
		return false, nil
	}
	conn := a.Redis.Get()
	defer conn.Close()
	res, err := redis.Int(conn.Do("DEL", "octyne-token:"+token))
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

// Close closes the authenticator. Once closed, the authenticator should not be used.
func (a *RedisAuthenticator) Close() error {
	a.stopUserUpdates()
	return a.Redis.Close()
}

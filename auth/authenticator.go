package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"sync"
)

// Authenticator is used by Octyne's Connector to provide HTTP API authentication.
type Authenticator interface {
	// GetUser returns info about the user with the given username.
	// Currently, it returns the password hash of the user.
	//
	// If the user does not exist, it returns ErrUserNotFound.
	GetUser(username string) (string, error)
	// Validate is called on an HTTP API request and returns the username if request is authenticated,
	// else returns an empty string.
	Validate(r *http.Request) (string, error)
	// ValidateAndReject is called on an HTTP API request and returns the username if request
	// is authenticated, else the request is rejected.
	ValidateAndReject(w http.ResponseWriter, r *http.Request) string
	// Login allows logging in a user and returning the token.
	// It returns an empty string if the username or password are invalid.
	Login(username string, password string) (string, error)
	// Logout allows logging out of a user and deleting the token from the server.
	Logout(token string) (bool, error)
	// Close closes the authenticator. Once closed, the authenticator should not be used.
	Close() error
}

// ErrUserNotFound is returned when a user was not found by the authenticator.
var ErrUserNotFound = errors.New("no user with this username was found")

// ReplaceableAuthenticator is an Authenticator implementation that allows replacing
// the underlying Authenticator in a thread-safe manner on the fly.
type ReplaceableAuthenticator struct {
	Engine      Authenticator
	EngineMutex sync.RWMutex
}

// GetUser returns info about the user with the given username.
// Currently, it returns the password hash of the user.
//
// If the user does not exist, it returns ErrUserNotFound.
func (a *ReplaceableAuthenticator) GetUser(username string) (string, error) {
	a.EngineMutex.RLock()
	defer a.EngineMutex.RUnlock()
	return a.Engine.GetUser(username)
}

// Validate is called on an HTTP API request and returns the username if request is authenticated,
// else returns an empty string.
func (a *ReplaceableAuthenticator) Validate(r *http.Request) (string, error) {
	a.EngineMutex.RLock()
	defer a.EngineMutex.RUnlock()
	return a.Engine.Validate(r)
}

// ValidateAndReject is called on an HTTP API request and returns the username if request
// is authenticated, else the request is rejected.
func (a *ReplaceableAuthenticator) ValidateAndReject(w http.ResponseWriter, r *http.Request) string {
	a.EngineMutex.RLock()
	defer a.EngineMutex.RUnlock()
	return a.Engine.ValidateAndReject(w, r)
}

// Login allows logging in a user and returning the token.
// It returns an empty string if the username or password are invalid.
func (a *ReplaceableAuthenticator) Login(username string, password string) (string, error) {
	a.EngineMutex.RLock()
	defer a.EngineMutex.RUnlock()
	return a.Engine.Login(username, password)
}

// Logout allows logging out of a user and deleting the token from the server.
func (a *ReplaceableAuthenticator) Logout(token string) (bool, error) {
	a.EngineMutex.RLock()
	defer a.EngineMutex.RUnlock()
	return a.Engine.Logout(token)
}

// Close closes the authenticator. Once closed, the authenticator should not be used.
func (a *ReplaceableAuthenticator) Close() error {
	a.EngineMutex.Lock()
	defer a.EngineMutex.Unlock()
	return a.Engine.Close()
}

// GetTokenFromRequest gets the token from the request header or cookie.
func GetTokenFromRequest(r *http.Request) string {
	token := r.Header.Get("Authorization")
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

func checkValidLoginAndGenerateToken(
	auth Authenticator, username string, password string,
) (string, error) {
	// Check whether this user exists and if the password matches the saved hash.
	hash, err := auth.GetUser(username)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return "", err
	} else if err != nil || !VerifyPasswordMatchesHash(password, hash) {
		return "", nil
	}
	// Generate a token and return it.
	token := make([]byte, 96)
	rand.Read(token) // Tolerate errors here, an error here is incredibly unlikely: skipcq GSC-G104
	return base64.StdEncoding.EncodeToString(token), nil
}

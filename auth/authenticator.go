package auth

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"

	"github.com/puzpuzpuz/xsync/v3"
)

// Authenticator is used by Octyne's Connector to provide HTTP API authentication.
type Authenticator interface {
	// GetUsers returns a Map with all the users and their corresponding passwords.
	GetUsers() *xsync.MapOf[string, string]
	// Validate is called on an HTTP API request and returns the username if request is authenticated.
	Validate(r *http.Request) (string, error)
	// ValidateAndReject is called on an HTTP API request and returns the username if request
	// is authenticated, else the request is rejected.
	ValidateAndReject(w http.ResponseWriter, r *http.Request) string
	// Login allows logging in a user and returning the token.
	Login(username string, password string) (string, error)
	// Logout allows logging out of a user and deleting the token from the server.
	Logout(token string) (bool, error)
	// Close closes the authenticator. Once closed, the authenticator should not be used.
	Close() error
}

// ReplaceableAuthenticator is an Authenticator implementation that allows replacing
// the underlying Authenticator in a thread-safe manner on the fly.
type ReplaceableAuthenticator struct {
	Engine      Authenticator
	EngineMutex sync.RWMutex
}

// GetUsers returns a Map with all the users and their corresponding passwords.
func (a *ReplaceableAuthenticator) GetUsers() *xsync.MapOf[string, string] {
	a.EngineMutex.RLock()
	defer a.EngineMutex.RUnlock()
	return a.Engine.GetUsers()
}

// Validate is called on an HTTP API request and returns the username if request is authenticated.
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

func checkValidLoginAndGenerateToken(auth Authenticator, username string, password string) string {
	// Check whether this user exists and if the password matches the saved hash.
	hash, exists := auth.GetUsers().Load(username)
	if !exists || !VerifyPasswordMatchesHash(password, hash) {
		return ""
	}
	// Generate a token and return it.
	token := make([]byte, 96)
	rand.Read(token) // Tolerate errors here, an error here is incredibly unlikely: skipcq GSC-G104
	return base64.StdEncoding.EncodeToString(token)
}

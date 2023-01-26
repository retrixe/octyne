package auth

import (
	"net/http"
	"sync"
)

// MemoryAuthenticator is an Authenticator implementation using an array to store tokens.
type MemoryAuthenticator struct {
	TokenMutex sync.RWMutex
	Tokens     map[string]string
}

// NewMemoryAuthenticator initializes an authenticator using memory for token storage.
func NewMemoryAuthenticator() Authenticator {
	// Create the authenticator.
	return &MemoryAuthenticator{
		Tokens: make(map[string]string),
	}
}

// Validate is called on an HTTP API request and checks whether or not the user is authenticated.
func (a *MemoryAuthenticator) Validate(w http.ResponseWriter, r *http.Request) bool {
	token := getTokenFromRequest(r)
	if !isValidToken(token) {
		http.Error(w, "{\"error\": \"You are not authenticated to access this resource!\"}",
			http.StatusUnauthorized)
		return false
	}
	// If valid, return true.
	a.TokenMutex.RLock()
	defer a.TokenMutex.RUnlock()
	_, ok := a.Tokens[token]
	if ok {
		return true
	}
	http.Error(w, "{\"error\": \"You are not authenticated to access this resource!\"}",
		http.StatusUnauthorized)
	return false
}

// Login allows logging in a user and returning the token.
func (a *MemoryAuthenticator) Login(username string, password string) string {
	token := checkValidLoginAndGenerateToken(username, password)
	if token == "" {
		return ""
	}
	a.TokenMutex.Lock()
	defer a.TokenMutex.Unlock()
	a.Tokens[token] = username
	return token
}

// Logout allows logging out of a user and deleting the token from the server.
func (a *MemoryAuthenticator) Logout(token string) bool {
	if !isValidToken(token) {
		return false
	}
	a.TokenMutex.Lock()
	defer a.TokenMutex.Unlock()
	tokenExisted := a.Tokens[token] != ""
	delete(a.Tokens, token)
	return tokenExisted
}

// Close closes the authenticator. This is no-op for MemoryAuthenticator.
func (*MemoryAuthenticator) Close() error {
	return nil
}

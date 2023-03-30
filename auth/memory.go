package auth

import (
	"net/http"

	"github.com/puzpuzpuz/xsync/v2"
)

// MemoryAuthenticator is an Authenticator implementation using an array to store tokens.
type MemoryAuthenticator struct {
	Tokens *xsync.MapOf[string, string]
}

// NewMemoryAuthenticator initializes an authenticator using memory for token storage.
func NewMemoryAuthenticator() Authenticator {
	return &MemoryAuthenticator{Tokens: xsync.NewMapOf[string]()}
}

// Validate is called on an HTTP API request and checks whether or not the user is authenticated.
func (a *MemoryAuthenticator) Validate(w http.ResponseWriter, r *http.Request) string {
	token := GetTokenFromRequest(r)
	if !isValidToken(token) {
		http.Error(w, "{\"error\": \"You are not authenticated to access this resource!\"}",
			http.StatusUnauthorized)
		return ""
	}
	// If valid, return true.
	username, ok := a.Tokens.Load(token)
	if ok {
		return username
	}
	http.Error(w, "{\"error\": \"You are not authenticated to access this resource!\"}",
		http.StatusUnauthorized)
	return ""
}

// Login allows logging in a user and returning the token.
func (a *MemoryAuthenticator) Login(username string, password string) (string, error) {
	token := checkValidLoginAndGenerateToken(username, password)
	if token == "" {
		return "", nil
	}
	a.Tokens.Store(token, username)
	return token, nil
}

// Logout allows logging out of a user and deleting the token from the server.
func (a *MemoryAuthenticator) Logout(token string) (bool, error) {
	if !isValidToken(token) {
		return false, nil
	}
	_, tokenExisted := a.Tokens.LoadAndDelete(token)
	return tokenExisted, nil
}

// Close closes the authenticator. This is no-op for MemoryAuthenticator.
func (*MemoryAuthenticator) Close() error {
	return nil
}

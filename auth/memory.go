package auth

import (
	"net/http"

	"github.com/puzpuzpuz/xsync/v3"
)

// MemoryAuthenticator is an Authenticator implementation using an array to store tokens.
type MemoryAuthenticator struct {
	Users  *xsync.MapOf[string, string]
	Tokens *xsync.MapOf[string, string]
}

// NewMemoryAuthenticator initializes an authenticator using memory for token storage.
func NewMemoryAuthenticator(usersJsonPath string) *MemoryAuthenticator {
	users := CreateUserStore(usersJsonPath)
	return &MemoryAuthenticator{Tokens: xsync.NewMapOf[string, string](), Users: users}
}

// GetUsers returns a Map with all the users and their corresponding passwords.
func (a *MemoryAuthenticator) GetUsers() *xsync.MapOf[string, string] {
	return a.Users
}

// Validate is called on an HTTP API request and returns the username if request is authenticated.
func (a *MemoryAuthenticator) Validate(w http.ResponseWriter, r *http.Request) (string, error) {
	if r.RemoteAddr == "@" {
		return "@local", nil
	}

	token := GetTokenFromRequest(r)
	if !isValidToken(token) {
		return "", nil
	}
	username, ok := a.Tokens.Load(token)
	if ok {
		if _, exists := a.Users.Load(username); exists {
			return username, nil
		}
		a.Logout(token)
	}
	return "", nil
}

// ValidateAndReject is called on an HTTP API request and returns the username if request
// is authenticated, else the request is rejected.
func (a *MemoryAuthenticator) ValidateAndReject(w http.ResponseWriter, r *http.Request) string {
	username, err := a.Validate(w, r)
	if err != nil {
		w.Header().Set("content-type", "application/json")
		http.Error(w, "{\"error\": \"Internal Server Error!\"}", http.StatusInternalServerError)
	} else if username == "" {
		w.Header().Set("content-type", "application/json")
		http.Error(w, "{\"error\": \"You are not authenticated to access this resource!\"}",
			http.StatusUnauthorized)
	}
	return username
}

// Login allows logging in a user and returning the token.
func (a *MemoryAuthenticator) Login(username string, password string) (string, error) {
	token := checkValidLoginAndGenerateToken(a, username, password)
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

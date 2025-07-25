package auth

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/puzpuzpuz/xsync/v3"
)

// MemoryAuthenticator is an Authenticator implementation using an array to store tokens.
type MemoryAuthenticator struct {
	Users           *xsync.MapOf[string, string]
	stopUserUpdates context.CancelFunc
	Tokens          *xsync.MapOf[string, string]
}

// NewMemoryAuthenticator initializes an authenticator using memory for token storage.
func NewMemoryAuthenticator(usersJsonPath string) *MemoryAuthenticator {
	userUpdates, stopUserUpdates := readAndWatchUsers(usersJsonPath)
	users := xsync.NewMapOf[string, string]()
	go (func() {
		for {
			newUsers, ok := <-userUpdates
			if !ok {
				return
			}
			users.Clear() // Clear all pre-existing users
			for username, password := range newUsers {
				if msg := ValidateUsername(username); msg == "" {
					users.Store(username, password)
				} else {
					log.Println(msg + " This account will be ignored and eventually removed!")
				}
			}
		}
	})()
	return &MemoryAuthenticator{
		Users:           users,
		stopUserUpdates: stopUserUpdates,
		Tokens:          xsync.NewMapOf[string, string](),
	}
}

// GetUser returns info about the user with the given username.
// Currently, it returns the password hash of the user.
//
// If the user does not exist, it returns ErrUserNotFound.
func (a *MemoryAuthenticator) GetUser(username string) (string, error) {
	user, ok := a.Users.Load(username)
	if !ok {
		return "", ErrUserNotFound
	}
	return user, nil
}

// Validate is called on an HTTP API request and returns the username if request is authenticated,
// else returns an empty string.
func (a *MemoryAuthenticator) Validate(r *http.Request) (string, error) {
	if r.RemoteAddr == "@" {
		return "@local", nil
	}

	token := GetTokenFromRequest(r)
	if !isValidToken(token) {
		return "", nil
	}
	username, ok := a.Tokens.Load(token)
	if ok {
		if _, err := a.GetUser(username); err == nil {
			return username, nil
		} else if !errors.Is(err, ErrUserNotFound) {
			return "", err
		}
		a.Logout(token)
	}
	return "", nil
}

// ValidateAndReject is called on an HTTP API request and returns the username if request
// is authenticated, else the request is rejected.
func (a *MemoryAuthenticator) ValidateAndReject(w http.ResponseWriter, r *http.Request) string {
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

// CanManageAuth returns whether or not this authenticator can manage auth, i.e. users and tokens.
func (*MemoryAuthenticator) CanManageAuth() bool {
	return true
}

// Login allows logging in a user and returning the token.
// It returns an empty string if the username or password are invalid.
func (a *MemoryAuthenticator) Login(username string, password string) (string, error) {
	token, err := checkValidLoginAndGenerateToken(a, username, password)
	if err != nil {
		return "", err
	} else if token == "" {
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

// Close closes the authenticator. Once closed, the authenticator should not be used.
func (a *MemoryAuthenticator) Close() error {
	a.stopUserUpdates()
	return nil
}

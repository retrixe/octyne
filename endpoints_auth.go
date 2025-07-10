package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/retrixe/octyne/auth"
)

// GET /login
type loginEndpointResponse struct {
	Token string `json:"token"`
}

func loginEndpoint(connector *Connector, w http.ResponseWriter, r *http.Request) {
	if r.RemoteAddr == "@" {
		httpError(w, "Auth endpoints cannot be called over Unix socket!", http.StatusBadRequest)
		return
	}
	// In case the username and password headers don't exist.
	username := r.Header.Get("Username")
	password := r.Header.Get("Password")
	if username == "" || password == "" {
		httpError(w, "Username or password not provided!", http.StatusBadRequest)
		return
	}
	// Authorize the user.
	token, err := connector.Login(username, password)
	if err != nil {
		log.Println("An error occurred when logging user in!", err)
		httpError(w, "Internal Server Error!", http.StatusInternalServerError)
		return
	} else if token == "" {
		httpError(w, "Invalid username or password!", http.StatusUnauthorized)
		return
	}
	connector.Info("auth.login", "ip", GetIP(r), "user", username)
	// Set the authentication cookie, if requested.
	if r.URL.Query().Get("cookie") == "true" {
		http.SetCookie(w, &http.Cookie{
			Name:   "X-Authentication",
			Value:  token,
			MaxAge: 60 * 60 * 24 * 31 * 3, // 3 months
			// Allows HTTP usage. Strict SameSite will block sending cookie over HTTP when using HTTPS:
			// https://web.dev/same-site-same-origin/
			Secure:   false,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
		writeJsonStringRes(w, "{\"success\":true}")
		return
	}
	// Send the response.
	writeJsonStructRes(w, loginEndpointResponse{Token: token}) // skipcq GSC-G104
}

// GET /logout
func logoutEndpoint(connector *Connector, w http.ResponseWriter, r *http.Request) {
	if r.RemoteAddr == "@" {
		httpError(w, "Auth endpoints cannot be called over Unix socket!", http.StatusBadRequest)
		return
	}
	// Check with authenticator.
	user := connector.ValidateAndReject(w, r)
	if user == "" {
		return
	}
	token := auth.GetTokenFromRequest(r)
	// Authorize the user.
	success, err := connector.Logout(token)
	if err != nil {
		log.Println("An error occurred when logging out user!", err)
		httpError(w, "Internal Server Error!", http.StatusInternalServerError)
		return
	} else if !success {
		httpError(w, "Invalid token, failed to logout!", http.StatusUnauthorized)
		return
	}
	// Unset the authentication cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     "X-Authentication",
		Value:    "",
		MaxAge:   -1,
		Secure:   false,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	// Send the response.
	connector.Info("auth.logout", "ip", GetIP(r), "user", user)
	writeJsonStringRes(w, "{\"success\":true}")
}

// GET /ott
func ottEndpoint(connector *Connector, w http.ResponseWriter, r *http.Request) {
	if r.RemoteAddr == "@" {
		httpError(w, "Auth endpoints cannot be called over Unix socket!", http.StatusBadRequest)
		return
	}
	// Check with authenticator.
	user := connector.ValidateAndReject(w, r)
	if user == "" {
		return
	}
	token := auth.GetTokenFromRequest(r)
	// Add a ticket.
	ticket := make([]byte, 4)
	rand.Read(ticket) // Tolerate errors here, an error here is incredibly unlikely: skipcq GSC-G104
	ticketString := base64.StdEncoding.EncodeToString(ticket)
	connector.Tickets.Store(ticketString, Ticket{
		Time:   time.Now().Unix(),
		User:   user,
		Token:  token,
		IPAddr: GetIP(r),
	})
	// Schedule deletion (cancellable).
	go (func() {
		<-time.After(2 * time.Minute)
		connector.Tickets.Delete(ticketString)
	})()
	// Send the response.
	writeJsonStringRes(w, "{\"ticket\": \""+ticketString+"\"}")
}

// GET /accounts
// POST /accounts
// PATCH /accounts?username=username
// DELETE /accounts?username=username
type accountsRequestBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func accountsEndpoint(connector *Connector, w http.ResponseWriter, r *http.Request) {
	user := connector.ValidateAndReject(w, r)
	if user == "" {
		return
	} else if r.Method != "GET" && r.Method != "POST" && r.Method != "PATCH" && r.Method != "DELETE" {
		httpError(w, "Only GET, POST, PATCH and DELETE are allowed!", http.StatusMethodNotAllowed)
		return
	}
	/* TODO: This breaks if users.json is updated multiple times before the user store updates every 1s.
	users := make(map[string]string)
	connector.GetUsers().Range(func(username string, password string) bool {
		users[username] = password
		return true
	}) */
	var users map[string]string
	contents, err := os.ReadFile(UsersJsonPath)
	if err != nil {
		log.Println("Error reading "+UsersJsonPath+" when modifying accounts!", err)
		httpError(w, "Internal Server Error!", http.StatusInternalServerError)
		return
	}
	err = json.Unmarshal(contents, &users)
	if err != nil {
		log.Println("Error parsing "+UsersJsonPath+" when modifying accounts!", err)
		httpError(w, "Internal Server Error!", http.StatusInternalServerError)
		return
	}
	if r.Method == "GET" {
		accountsEndpointGet(w, users)
		return
	} else if r.Method == "POST" && !accountsEndpointPost(connector, w, r, users, user) {
		return
	} else if r.Method == "PATCH" && !accountsEndpointPatch(connector, w, r, users, user) {
		return
	} else if r.Method == "DELETE" && !accountsEndpointDelete(connector, w, r, users, user) {
		return
	}
	usersJson, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		log.Println("Error serialising " + UsersJsonPath + " when modifying accounts!")
		httpError(w, "Internal Server Error!", http.StatusInternalServerError)
		return
	}
	err = os.WriteFile(UsersJsonPath+"~", []byte(string(usersJson)+"\n"), 0666)
	if err != nil {
		log.Println("Error writing to " + UsersJsonPath + " when modifying accounts!")
		httpError(w, "Internal Server Error!", http.StatusInternalServerError)
		return
	}
	err = os.Rename(UsersJsonPath+"~", UsersJsonPath)
	if err != nil {
		log.Println("Error writing to " + UsersJsonPath + " when modifying accounts!")
		httpError(w, "Internal Server Error!", http.StatusInternalServerError)
		return
	}
	writeJsonStringRes(w, "{\"success\":true}")
}

func accountsEndpointGet(w http.ResponseWriter, users map[string]string) {
	var usernames []string
	for username := range users {
		usernames = append(usernames, username)
	}
	usernamesJson, err := json.Marshal(usernames)
	if err != nil {
		log.Println("Error serialising usernames when listing accounts!")
		httpError(w, "Internal Server Error!", http.StatusInternalServerError)
		return
	}
	writeJsonStringRes(w, string(usernamesJson))
}

func accountsEndpointPost(connector *Connector, w http.ResponseWriter, r *http.Request,
	users map[string]string, user string) bool {
	var buffer bytes.Buffer
	_, err := buffer.ReadFrom(r.Body)
	if err != nil {
		httpError(w, "Failed to read body!", http.StatusBadRequest)
		return false
	}
	var body accountsRequestBody
	err = json.Unmarshal(buffer.Bytes(), &body)
	if err != nil {
		httpError(w, "Invalid JSON body!", http.StatusBadRequest)
		return false
	} else if body.Username == "" || body.Password == "" {
		httpError(w, "Username or password not provided!", http.StatusBadRequest)
		return false
	} else if users[body.Username] != "" {
		httpError(w, "User already exists!", http.StatusConflict)
		return false
	} else if msg := auth.ValidateUsername(body.Username); msg != "" {
		httpError(w, msg, http.StatusBadRequest)
		return false
	}
	hash := auth.HashPassword(body.Password)
	connector.Info("accounts.create", "ip", GetIP(r), "user", user, "newUser", body.Username)
	users[body.Username] = hash
	return true
}

func accountsEndpointPatch(connector *Connector, w http.ResponseWriter, r *http.Request,
	users map[string]string, user string) bool {
	username := r.URL.Query().Get("username")
	var buffer bytes.Buffer
	_, err := buffer.ReadFrom(r.Body)
	if err != nil {
		httpError(w, "Failed to read body!", http.StatusBadRequest)
		return false
	}
	var body accountsRequestBody
	err = json.Unmarshal(buffer.Bytes(), &body)
	if username == "" { // Legacy compat with older API, assume body.Username, fix in 2.0
		username = body.Username
	}
	toUpdateUsername := r.URL.Query().Get("username") != "" && body.Username != ""
	if err != nil {
		httpError(w, "Invalid JSON body!", http.StatusBadRequest)
		return false
	} else if username == "" || (body.Password == "" && !toUpdateUsername) {
		httpError(w, "Username or password not provided!", http.StatusBadRequest)
		return false
	} else if users[username] == "" {
		httpError(w, "User does not exist!", http.StatusNotFound)
		return false
	} else if toUpdateUsername && users[body.Username] != "" {
		httpError(w, "User already exists!", http.StatusConflict)
		return false
	} else if msg := auth.ValidateUsername(body.Username); msg != "" {
		httpError(w, msg, http.StatusBadRequest)
		return false
	}
	hash := users[username]
	if body.Password != "" {
		hash = auth.HashPassword(body.Password)
	}
	if toUpdateUsername {
		connector.Info("accounts.update", "ip", GetIP(r), "user", user,
			"updatedUser", body.Username, "oldUsername", username, "changedPassword", body.Password != "")
		delete(users, username)
		users[body.Username] = hash
	} else {
		connector.Info("accounts.update", "ip", GetIP(r), "user", user,
			"updatedUser", username, "changedPassword", true)
		users[username] = hash
	}
	return true
}

func accountsEndpointDelete(connector *Connector, w http.ResponseWriter, r *http.Request,
	users map[string]string, user string) bool {
	username := r.URL.Query().Get("username")
	if username == "" {
		httpError(w, "Username not provided!", http.StatusBadRequest)
		return false
	} else if users[username] == "" {
		httpError(w, "User does not exist!", http.StatusNotFound)
		return false
	}
	connector.Info("accounts.delete", "ip", GetIP(r), "user", user, "deletedUser", username)
	delete(users, username)
	return true
}

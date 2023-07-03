package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/retrixe/octyne/auth"
)

func (connector *Connector) registerAuthRoutes() {
	// GET /login
	type loginResponse struct {
		Token string `json:"token"`
	}
	connector.Router.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
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
			fmt.Fprintln(w, "{\"success\":true}")
			return
		}
		// Send the response.
		json.NewEncoder(w).Encode(loginResponse{Token: token}) // skipcq GSC-G104
	})

	// GET /logout
	connector.Router.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.RemoteAddr == "@" {
			httpError(w, "Auth endpoints cannot be called over Unix socket!", http.StatusBadRequest)
			return
		}
		// Check with authenticator.
		user := connector.Validate(w, r)
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
		fmt.Fprintln(w, "{\"success\":true}")
	})

	// GET /ott
	connector.Router.HandleFunc("/ott", func(w http.ResponseWriter, r *http.Request) {
		if r.RemoteAddr == "@" {
			httpError(w, "Auth endpoints cannot be called over Unix socket!", http.StatusBadRequest)
			return
		}
		// Check with authenticator.
		user := connector.Validate(w, r)
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
		fmt.Fprintln(w, "{\"ticket\": \""+ticketString+"\"}")
	})

	// GET /accounts
	// POST /accounts
	// PATCH /accounts
	// DELETE /accounts?username=username
	type accountsRequestBody struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	connector.Router.HandleFunc("/accounts", func(w http.ResponseWriter, r *http.Request) {
		user := connector.Validate(w, r)
		if user == "" {
			return
		} else if r.Method != "GET" && r.Method != "POST" && r.Method != "PATCH" && r.Method != "DELETE" {
			httpError(w, "Only GET, POST, PATCH and DELETE are allowed!", http.StatusMethodNotAllowed)
			return
		}
		var users map[string]string
		contents, err := os.ReadFile("users.json")
		if err != nil {
			log.Println("Error reading users.json when modifying accounts!", err)
			httpError(w, "Internal Server Error!", http.StatusInternalServerError)
			return
		}
		err = json.Unmarshal(contents, &users)
		if err != nil {
			log.Println("Error parsing users.json when modifying accounts!", err)
			httpError(w, "Internal Server Error!", http.StatusInternalServerError)
			return
		}
		if r.Method == "GET" {
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
			fmt.Fprintln(w, string(usernamesJson))
			return
		} else if r.Method == "POST" || r.Method == "PATCH" {
			var buffer bytes.Buffer
			_, err := buffer.ReadFrom(r.Body)
			if err != nil {
				httpError(w, "Failed to read body!", http.StatusBadRequest)
				return
			}
			var body accountsRequestBody
			err = json.Unmarshal(buffer.Bytes(), &body)
			if err != nil {
				httpError(w, "Invalid JSON body!", http.StatusBadRequest)
				return
			} else if body.Username == "" || body.Password == "" {
				httpError(w, "Username or password not provided!", http.StatusBadRequest)
				return
			} else if r.Method == "POST" && users[body.Username] != "" {
				httpError(w, "User already exists!", http.StatusConflict)
				return
			} else if r.Method == "PATCH" && users[body.Username] == "" {
				httpError(w, "User does not exist!", http.StatusNotFound)
				return
			}
			sha256sum := fmt.Sprintf("%x", sha256.Sum256([]byte(body.Password)))
			if r.Method == "POST" {
				connector.Info("accounts.create", "ip", GetIP(r), "user", user, "newUser", body.Username)
			} else {
				connector.Info("accounts.update", "ip", GetIP(r), "user", user, "updatedUser", body.Username)
			}
			users[body.Username] = sha256sum
		} else if r.Method == "DELETE" {
			username := r.URL.Query().Get("username")
			if username == "" {
				httpError(w, "Username not provided!", http.StatusBadRequest)
				return
			} else if users[username] == "" {
				httpError(w, "User does not exist!", http.StatusNotFound)
				return
			}
			connector.Info("accounts.delete", "ip", GetIP(r), "user", user, "deletedUser", username)
			delete(users, username)
		}
		usersJson, err := json.MarshalIndent(users, "", "  ")
		if err != nil {
			log.Println("Error serialising users.json when modifying accounts!")
			httpError(w, "Internal Server Error!", http.StatusInternalServerError)
			return
		}
		err = os.WriteFile("users.json~", []byte(string(usersJson)+"\n"), 0666)
		if err != nil {
			log.Println("Error writing to users.json when modifying accounts!")
			httpError(w, "Internal Server Error!", http.StatusInternalServerError)
			return
		}
		err = os.Rename("users.json~", "users.json")
		if err != nil {
			log.Println("Error writing to users.json when modifying accounts!")
			httpError(w, "Internal Server Error!", http.StatusInternalServerError)
			return
		}
		fmt.Fprintln(w, "{\"success\":true}")
	})
}

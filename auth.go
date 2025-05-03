package main
/*
copyright 2025 by Moshix
this is an authentication front end to racingsmar's proxy3270 library.
It will be used to authenticate users to the proxy3270 library,
and then pass the connection to remote mainframes as listed in the
hosts lists pointd to by the secure3270.cnf file.
check out github.com/racingmars/go3270 for the proxy3270 library.

v 0.1 build the authentication screen
v 0.2 add support for TLS
v 0.3 renegotiate telnet after connection is closed
v 0.4 provide a user and password list
v 0.5 per user host lists!
:wq 
*/

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/racingmars/go3270"
)

// Field names for auth screens
const (
	fieldUsername = "username"
	fieldPassword = "password"
	fieldErrorMsg = "errorMsg"
)

type User struct {
	Username string
	Password string
	HostFile string // Path to user-specific host file
}

type authSession struct {
	authenticated bool
	username      string
	hostFile      string // Store the host file for this user's session
}

var (
	authUsers     []User
	authUsersLock sync.RWMutex
)

// LoadAuthConfig loads the authentication configuration from users.cnf file
func LoadAuthConfig(configFile string) error {
	// The users file is in the same directory as the config file
	usersFile := "users.cnf"

	file, err := os.Open(usersFile)
	if err != nil {
		return fmt.Errorf("failed to open users file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var users []User

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "/", 3)
		if len(parts) < 2 {
			continue
		}

		username := strings.TrimSpace(parts[0])
		password := strings.TrimSpace(parts[1])

		// Get the host file if it exists, otherwise use the default
		hostFile := ""
		if len(parts) >= 3 {
			hostFile = strings.TrimSpace(parts[2])
		}

		if username != "" && password != "" {
			users = append(users, User{
				Username: username,
				Password: password,
				HostFile: hostFile,
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading users file: %v", err)
	}

	if len(users) == 0 {
		return fmt.Errorf("no valid users found in %s", usersFile)
	}

	// Update the global users list
	authUsersLock.Lock()
	authUsers = users
	authUsersLock.Unlock()

	return nil
}

// authenticateUser checks if the provided credentials are valid and returns the user's host file
func authenticateUser(username, password string) (bool, string) {
	authUsersLock.RLock()
	defer authUsersLock.RUnlock()

	for _, user := range authUsers {
		if username == user.Username && password == user.Password {
			return true, user.HostFile
		}
	}

	return false, ""
}

// HandleAuth manages the authentication flow using 3270 screens
func HandleAuth(conn net.Conn) (*authSession, error) {
	// Create field values map
	fieldValues := make(map[string]string)

	// Create login screen
	loginScreen := go3270.Screen{
		// Title bar with dashes
		{Row: 0, Col: 0, Content: strings.Repeat("-", 15) + " SECURE3270PROXY - TSO/E  LOGON " + strings.Repeat("-", 15), Color: go3270.White},

		// Function key help line
		{Row: 2, Col: 0, Content: "PF1/PF13 ==> Help   PF9 ==> Logoff", Color: go3270.White},

		// Main section headers
		{Row: 4, Col: 3, Content: "ENTER LOGON PARAMETERS BELOW:", Color: go3270.White},
		{Row: 4, Col: 39, Content: "RACF LOGON PARAMETERS:", Color: go3270.White},

		// Left column fields
		{Row: 6, Col: 3, Content: "USERID    ", Color: go3270.Turquoise},
		{Row: 6, Col: 13, Content: "===>", Color: go3270.White},
		{Row: 6, Col: 19, Name: fieldUsername, Write: true, Color: go3270.Red},
		{Row: 6, Col: 27, Autoskip: true},

		{Row: 8, Col: 3, Content: "PASSWORD  ", Color: go3270.Turquoise},
		{Row: 8, Col: 13, Content: "===>", Color: go3270.White},
		{Row: 8, Col: 19, Name: fieldPassword, Write: true, Hidden: true, Color: go3270.Red},
		{Row: 8, Col: 36, Autoskip: true},

		{Row: 10, Col: 3, Content: "PROCEDURE ", Color: go3270.Turquoise},
		{Row: 10, Col: 13, Content: "===>", Color: go3270.White},
		{Row: 10, Col: 19, Content: "TSOISPF", Color: go3270.Pink},

		{Row: 12, Col: 3, Content: "ACCT NMBR ", Color: go3270.Turquoise},
		{Row: 12, Col: 13, Content: "===>", Color: go3270.White},

		{Row: 14, Col: 3, Content: "SIZE      ", Color: go3270.Turquoise},
		{Row: 14, Col: 13, Content: "===>", Color: go3270.White},
		{Row: 14, Col: 19, Content: "6144", Color: go3270.Pink},

		{Row: 16, Col: 3, Content: "PERFORM   ", Color: go3270.Turquoise},
		{Row: 16, Col: 13, Content: "===>", Color: go3270.White},

		{Row: 18, Col: 3, Content: "COMMAND   ", Color: go3270.Turquoise},
		{Row: 18, Col: 13, Content: "===>", Color: go3270.White},

		// Right column fields
		{Row: 10, Col: 39, Content: "GROUP IDENT  ", Color: go3270.Turquoise},
		{Row: 10, Col: 51, Content: "===>", Color: go3270.White},

		// Options section
		{Row: 21, Col: 3, Content: "ENTER AN 'S' BEFORE EACH OPTION DESIRED BELOW:", Color: go3270.White},

		{Row: 23, Col: 11, Content: "-NOMAIL         -NONOTICE        -RECONNECT        -OIDCARD", Color: go3270.Turquoise},

		// Error message field (hidden at bottom)
		{Row: 24, Col: 0, Name: fieldErrorMsg, Color: go3270.Red, Intense: true},
	}

	// Define rules
	rules := go3270.Rules{
		fieldUsername: {Validator: go3270.NonBlank},
		fieldPassword: {Validator: go3270.NonBlank},
	}

	session := &authSession{}

	for {
		// Display the screen and get user input
		resp, err := go3270.HandleScreen(
			loginScreen,
			rules,
			fieldValues,
			[]go3270.AID{go3270.AIDEnter},
			[]go3270.AID{go3270.AIDPF9},
			fieldErrorMsg,
			6, 20, // Position cursor at username field
			conn,
		)

		if err != nil {
			return nil, fmt.Errorf("screen show error: %v", err)
		}

		// Check if user pressed PF9 (logoff)
		if resp.AID == go3270.AIDPF9 {
			return nil, fmt.Errorf("user requested logoff with PF9")
		}

		if resp.AID == go3270.AIDEnter {
			username := resp.Values[fieldUsername]
			password := resp.Values[fieldPassword]

			authenticated, hostFile := authenticateUser(username, password)
			if authenticated {
				session.authenticated = true
				session.username = username
				session.hostFile = hostFile
				return session, nil
			}

			// Show invalid credentials message in the error field
			fieldValues[fieldErrorMsg] = "Invalid userid or password. Please try again."
		}
	}
}

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
v 0.5 per user hosts lists!
v 0.6 selecing X or 99 from hosts view will disconnect session
:wq
*/

import (
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/racingmars/go3270"
)

func handleProxyConnection(conn net.Conn, config *Config, authSession *authSession) {
	for {
		// Create field values map
		fieldValues := make(map[string]string)

		// Show host selection menu with centered title
		welcomeMsg := fmt.Sprintf("Welcome %s - Available Hosts", authSession.username)
		// Calculate center position (assuming 80 column screen)
		centerPos := (80 - len(welcomeMsg)) / 2
		if centerPos < 1 {
			centerPos = 1
		}

		screen := go3270.Screen{
			{Row: 0, Col: centerPos, Content: welcomeMsg, Color: go3270.White},
		}

		// Add host entries - start from row 2
		for i, host := range config.Hosts {
			// Add the host number in white
			screen = append(screen, go3270.Field{
				Row:     i + 2, // Start from row 2
				Col:     1,
				Content: fmt.Sprintf("%2d.", i+1),
				Color:   go3270.White,
			})

			// Split the host details: name in blue, address in green
			hostName := fmt.Sprintf("%-30s", host.Name)
			hostAddr := fmt.Sprintf("(%s:%d)", host.Host, host.Port)

			// Add host name in blue
			screen = append(screen, go3270.Field{
				Row:     i + 2,
				Col:     5,
				Content: hostName,
				Color:   go3270.Blue,
			})

			// Add host address in green
			screen = append(screen, go3270.Field{
				Row:     i + 2,
				Col:     5 + len(hostName),
				Content: hostAddr,
				Color:   go3270.Green,
			})
		}

		// Add disconnect option on row 21
		screen = append(screen, go3270.Field{
			Row:     21,
			Col:     4,
			Content: "Enter 99 or X to disconnect",
			Color:   go3270.White,
		})

		// Add selectoin feeld on row 23
		screen = append(screen,
			go3270.Field{
				Row:     23,
				Col:     4,
				Content: "Enter selection (1-" + strconv.Itoa(len(config.Hosts)) + ", 99, or X): ",
				Color:   go3270.Red,
			},
			go3270.Field{
				Row:          23,
				Col:          36,
				Name:         "selection",
				Write:        true,
				Color:        go3270.Green,
				Highlighting: go3270.Underscore,
			},
			go3270.Field{
				Row:      23,
				Col:      39,
				Autoskip: true,
			},
		)

		// Define rules
		rules := go3270.Rules{
			"selection": {Validator: go3270.NonBlank},
		}

		// Display the screen and wait for user input
		resp, err := go3270.HandleScreen(
			screen,
			rules,
			fieldValues,
			[]go3270.AID{go3270.AIDEnter},
			[]go3270.AID{},
			"",
			23, 37, // Position cursor at selection field on row 23
			conn,
		)

		if err != nil {
			log.Printf("Screen show error: %v", err)
			return
		}

		if resp.AID == go3270.AIDEnter {
			selection := resp.Values["selection"]

			// Check for disconnect commands (99 or X/x)
			if selection == "99" || strings.ToUpper(selection) == "X" {
				log.Printf("User %s requested disconnect with selection: %s", authSession.username, selection)
				return // Exit the function to close the connection
			}

			// Otherwise, try to parse as a host number
			num, err := strconv.Atoi(selection)
			if err != nil || num < 1 || num > len(config.Hosts) {
				continue
			}

			// Connect to selected host
			selectedHost := config.Hosts[num-1]
			if err := connectToHost(conn, selectedHost); err != nil {
				log.Printf("Connection to host failed: %v", err)

				// Show eror screan
				errorScreen := go3270.Screen{
					{Row: 1, Col: 1, Content: "Connection Error", Color: go3270.White},
					{Row: 3, Col: 1, Content: fmt.Sprintf("Failed to connect to %s: %v", selectedHost.Name, err), Color: go3270.White},
					{Row: 5, Col: 1, Content: "Press Enter to continue", Color: go3270.White},
				}

				go3270.HandleScreen(
					errorScreen,
					nil,
					nil,
					[]go3270.AID{go3270.AIDEnter},
					[]go3270.AID{},
					"",
					5, 1,
					conn,
				)
				continue
			}

			// After disconnecting from the host, re-display the host selection menu
			// by continuing the loop instead of returning
			continue
		}
	}
}

func connectToHost(clientConn net.Conn, host Host) error {
	// Un-negotiate telnet protocol before connecting to host
	if err := go3270.UnNegotiateTelnet(clientConn, 2*time.Second); err != nil {
		return fmt.Errorf("telnet un-negotiation failed: %v", err)
	}

	// Connect to the target host
	targetConn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", host.Host, host.Port))
	if err != nil {
		// If we failed to connect, re-negotiate telnet before returning the error
		// so the client can display the error properly
		_ = go3270.NegotiateTelnet(clientConn)
		return fmt.Errorf("failed to connect to target: %v", err)
	}
	defer targetConn.Close()

	// Create channels for error handling
	errChan := make(chan error, 2)
	doneChan := make(chan struct{})
	stopProxyChan := make(chan struct{})

	// Forward data in both directions with cancellation
	go forwardWithCancel(clientConn, targetConn, errChan, stopProxyChan, "client->target")
	go forwardWithCancel(targetConn, clientConn, errChan, stopProxyChan, "target->client")

	// Wait for either connection to close
	var forwardErr error
	select {
	case forwardErr = <-errChan:
		// Connection closed, signal both goroutines to stop
		close(stopProxyChan)
		close(doneChan)

		// Let the goroutines finish cleanup (discard their errors)
		for i := 0; i < 2; i++ {
			select {
			case <-errChan:
			case <-time.After(100 * time.Millisecond):
			}
		}
	}

	// Reset the client connection to ensure clean state
	if tcpConn, ok := clientConn.(*net.TCPConn); ok {
		tcpConn.SetLinger(0) // Set SO_LINGER to 0 to discard any pending data
	}

	// Wait for the connection to settle
	time.Sleep(500 * time.Millisecond)

	// Re-negotiate telnet protocol with increased timeout for better reliability
	var negotiateErr error
	for attempts := 0; attempts < 5; attempts++ {
		// Use a fresh connection deadline for each attempt
		clientConn.SetDeadline(time.Now().Add(30 * time.Second))

		negotiateErr = go3270.NegotiateTelnet(clientConn)
		if negotiateErr == nil {
			// Successfully re-negotiated telnet
			clientConn.SetDeadline(time.Now().Add(5 * time.Minute)) // Reset to normal timeout
			log.Printf("Successfully re-negotiated telnet after %d attempts", attempts+1)
			break
		}

		// Log the error and retry
		log.Printf("Telnet re-negotiation attempt %d failed: %v", attempts+1, negotiateErr)
		time.Sleep(1 * time.Second)
	}

	if negotiateErr != nil {
		log.Printf("All telnet re-negotiation attempts failed, last error: %v", negotiateErr)
		// Even though negotiation failed, try to continue anyway
		// This allows the user to at least see an error message in the host menu
	}

	// Log any original error for debugging purposes
	if forwardErr != nil && forwardErr != io.EOF {
		log.Printf("DEBUG: Original connection error: %v", forwardErr)
	}

	// Always return nil to get back to host menu, even if there were errors
	// The key insight is we need to continue even after errors
	return nil
}

func forwardWithCancel(dst net.Conn, src net.Conn, errChan chan error, stopChan chan struct{}, direction string) {
	buf := make([]byte, 32*1024)

	for {
		select {
		case <-stopChan:
			// Stopping was requested
			errChan <- fmt.Errorf("forwarding %s canceled", direction)
			return
		default:
			// Set a short read deadline to allow checking the stop channel frequently
			src.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, err := src.Read(buf)

			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// This is just our short timeout for checking stopChan, keep looping
					continue
				}
				// Real error, report it
				errChan <- err
				return
			}

			if n > 0 {
				_, writeErr := dst.Write(buf[:n])
				if writeErr != nil {
					errChan <- writeErr
					return
				}
			}
		}
	}
}

func forward(dst net.Conn, src net.Conn, errChan chan error) {
	_, err := io.Copy(dst, src)
	errChan <- err
}

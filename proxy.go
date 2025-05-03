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
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
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
	// Set a timeout for the un-negotiation
	clientConn.SetDeadline(time.Now().Add(10 * time.Second))

	// Un-negotiate telnet protocol before connecting to host
	if err := go3270.UnNegotiateTelnet(clientConn, 2*time.Second); err != nil {
		log.Printf("Warning: telnet un-negotiation failed: %v", err)
		// Continue anyway - some clients may not require proper un-negotiation
	}

	// Connect to the target host with a timeout
	dialer := net.Dialer{Timeout: 15 * time.Second}
	targetConn, err := dialer.Dial("tcp", fmt.Sprintf("%s:%d", host.Host, host.Port))
	if err != nil {
		// If connection failed, re-negotiate telnet to show error message
		clientConn.SetDeadline(time.Now().Add(10 * time.Second))
		_ = go3270.NegotiateTelnet(clientConn)
		clientConn.SetDeadline(time.Time{}) // Remove deadline
		return fmt.Errorf("failed to connect to target: %v", err)
	}

	// Create buffers for error handling and data transfer
	clientBuffer := make([]byte, 32*1024)
	targetBuffer := make([]byte, 32*1024)

	// Create a cancel context for proper cleanup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use WaitGroup to ensure both goroutines finish
	var wg sync.WaitGroup
	wg.Add(2)

	// Create error channel
	errChan := make(chan error, 2)

	// Forward data client -> target
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				// Set short timeout to check context regularly
				clientConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
				n, err := clientConn.Read(clientBuffer)

				if err != nil {
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						continue // Just a timeout, try again
					}
					// Real error
					errChan <- err
					cancel() // Cancel other goroutine
					return
				}

				if n > 0 {
					// Try sending data with timeout
					targetConn.SetWriteDeadline(time.Now().Add(5 * time.Second))
					_, err := targetConn.Write(clientBuffer[:n])
					if err != nil {
						errChan <- err
						cancel()
						return
					}
				}
			}
		}
	}()

	// Forward data target -> client
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				// Set short timeout to check context regularly
				targetConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
				n, err := targetConn.Read(targetBuffer)

				if err != nil {
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						continue // Just a timeout, try again
					}
					// Real error
					errChan <- err
					cancel() // Cancel other goroutine
					return
				}

				if n > 0 {
					// Try sending data with timeout
					clientConn.SetWriteDeadline(time.Now().Add(5 * time.Second))
					_, err := clientConn.Write(targetBuffer[:n])
					if err != nil {
						errChan <- err
						cancel()
						return
					}
				}
			}
		}
	}()

	// Wait for an error or EOF
	var finalErr error
	select {
	case finalErr = <-errChan:
		// An error occurred, cancel both goroutines
		cancel()
	}

	// Close the target connection
	targetConn.Close()

	// Wait for both goroutines to finish
	wg.Wait()

	// Reset the client connection to ensure clean state
	if tcpConn, ok := clientConn.(*net.TCPConn); ok {
		tcpConn.SetLinger(0) // Discard any pending data
	}

	// Give connections time to settle
	time.Sleep(500 * time.Millisecond)

	// Re-negotiate telnet protocol with increased timeout and retry
	var negotiateErr error
	for attempts := 0; attempts < 3; attempts++ {
		// Use a fresh deadline for each attempt
		clientConn.SetDeadline(time.Now().Add(10 * time.Second))

		// Try to renegotiate telnet
		negotiateErr = go3270.NegotiateTelnet(clientConn)
		if negotiateErr == nil {
			// Success!
			clientConn.SetDeadline(time.Time{}) // Remove deadline
			log.Printf("Successfully re-negotiated telnet after %d attempts", attempts+1)
			break
		}

		log.Printf("Telnet re-negotiation attempt %d failed: %v", attempts+1, negotiateErr)
		time.Sleep(1 * time.Second) // Wait before retry
	}

	// Log errors for debugging (only log non-EOF errors)
	if finalErr != nil && finalErr != io.EOF {
		log.Printf("DEBUG: Connection error: %v", finalErr)
	}

	// Remove any deadlines
	clientConn.SetDeadline(time.Time{})

	// Always return nil to get back to the host menu
	return nil
}

package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/racingmars/go3270"
)

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
v 0.7 more permissive TLS settings
v 0.8 add F11 key to display clock from proxy menu
:wq
*/
type Host struct {
	Name string `json:"name"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

type Config struct {
	Hosts         []Host
	Port          int
	TLSPort       int
	TLSCert       string
	TLSKey        string
	HostFile      string // Path to the hosts configuration file
	TLSEnabled    bool   // Flag to enable/disable TLS
	TLSMinVersion string // Minimum TLS version (TLS1.0, TLS1.1, TLS1.2, TLS1.3)
	TLSMaxVersion string // Maximum TLS version (TLS1.0, TLS1.1, TLS1.2, TLS1.3)
	TLSTimeout    int    // Timeout in seconds for TLS connection negotiation
}

func loadConfig(filename string) (*Config, error) {
	var config Config

	// Default host file if not specified in secure3270.cnf
	config.HostFile = "proxy3270.ovh"

	// First read the secure3270.cnf file for configuration
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch strings.ToLower(key) {
		case "port":
			if port, err := strconv.Atoi(value); err == nil && port > 0 {
				config.Port = port
			}
		case "tlsport":
			if port, err := strconv.Atoi(value); err == nil && port > 0 {
				config.TLSPort = port
			}
		case "tlscert":
			config.TLSCert = value
		case "tlskey":
			config.TLSKey = value
		case "hostfile":
			config.HostFile = value
		case "tls":
			// Make sure to handle any whitespace or comments in the value
			trimmedValue := strings.TrimSpace(strings.Split(value, "#")[0])
			config.TLSEnabled = strings.ToLower(trimmedValue) == "enabled"
		case "tlsminversion":
			config.TLSMinVersion = value
		case "tlsmaxversion":
			config.TLSMaxVersion = value
		case "tlstimeout":
			if timeout, err := strconv.Atoi(value); err == nil && timeout > 0 {
				config.TLSTimeout = timeout
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading config: %v", err)
	}

	// Now load the proxy hosts configuraton from the speficied file
	proxyData, err := os.ReadFile(config.HostFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read proxy config from %s: %v", config.HostFile, err)
	}

	if err := json.Unmarshal(proxyData, &config.Hosts); err != nil {
		return nil, fmt.Errorf("failed to parse proxy config: %v", err)
	}

	// Set default port if not specified
	if config.Port == 0 {
		config.Port = 3270
	}

	// Display configuration summary
	log.Printf("Configuration loaded successfully from %s:", filename)
	log.Printf("  - Standard listener port: %d", config.Port)
	if config.TLSEnabled {
		if config.TLSPort > 0 && config.TLSCert != "" && config.TLSKey != "" {
			log.Printf("  - TLS listener enabled on port: %d", config.TLSPort)
			log.Printf("  - TLS certificate: %s", config.TLSCert)
			log.Printf("  - TLS key: %s", config.TLSKey)

			// Display TLS version settings
			if config.TLSMinVersion != "" {
				log.Printf("  - TLS minimum version: %s", config.TLSMinVersion)
			} else {
				log.Printf("  - TLS minimum version: TLS1.0 (default)")
			}

			if config.TLSMaxVersion != "" {
				log.Printf("  - TLS maximum version: %s", config.TLSMaxVersion)
			} else {
				log.Printf("  - TLS maximum version: TLS1.3 (default)")
			}

			if config.TLSTimeout > 0 {
				log.Printf("  - TLS connection timeout: %d seconds", config.TLSTimeout)
			} else {
				log.Printf("  - TLS connection timeout: 60 seconds (default)")
			}
		} else {
			log.Printf("  - WARNING: TLS is enabled but configuration is incomplete")
			if config.TLSPort == 0 {
				log.Printf("    - TLS port not specified")
			}
			if config.TLSCert == "" {
				log.Printf("    - TLS certificate not specified")
			}
			if config.TLSKey == "" {
				log.Printf("    - TLS key not specified")
			}
		}
	} else {
		log.Printf("  - TLS listener disabled")
	}
	log.Printf("  - Host list file: %s (%d hosts)", config.HostFile, len(config.Hosts))

	return &config, nil
}

func startTLSServer(config *Config, debug, debug3270, trace bool) {
	if config.TLSPort == 0 {
		log.Printf("TLS enabled but port not specified, can't start TLS server")
		return
	}

	// Check if certificate files exist
	if _, err := os.Stat(config.TLSCert); os.IsNotExist(err) {
		log.Printf("TLS certificate file %s not found, can't start TLS server", config.TLSCert)
		return
	}

	if _, err := os.Stat(config.TLSKey); os.IsNotExist(err) {
		log.Printf("TLS key file %s not found, can't start TLS server", config.TLSKey)
		return
	}

	// TLS server auto-recovery loop
	for {
		startTime := time.Now()
		if err := runTLSServer(config, debug, debug3270, trace); err != nil {
			log.Printf("TLS server error: %v", err)

			// If the server ran for a reasonable amount of time before failing,
			// it's likely a temporary issue, so we can restart immediately
			if time.Since(startTime) > 5*time.Minute {
				log.Printf("TLS server restarting immediately...")
			} else {
				// If it failed quickly, there might be a more serious issue
				// Wait before retrying to avoid rapid restart loops
				log.Printf("TLS server will restart in 30 seconds...")
				time.Sleep(30 * time.Second)
			}
		} else {
			// Normal shutdown - wait before restarting
			log.Printf("TLS server shut down, restarting in 10 seconds...")
			time.Sleep(10 * time.Second)
		}
	}
}

func runTLSServer(config *Config, debug, debug3270, trace bool) error {
	cert, err := tls.LoadX509KeyPair(config.TLSCert, config.TLSKey)
	if err != nil {
		return fmt.Errorf("failed to load TLS certificates: %v", err)
	}

	// Set TLS version based on configuration or use defaults
	var minVersion uint16 = tls.VersionTLS10 // Default minimum
	var maxVersion uint16 = tls.VersionTLS13 // Default maximum

	// Parse minimum TLS version from config
	if config.TLSMinVersion != "" {
		switch strings.ToLower(config.TLSMinVersion) {
		case "tls1.0", "tls1", "tlsv1.0", "tlsv1":
			minVersion = tls.VersionTLS10
		case "tls1.1", "tlsv1.1":
			minVersion = tls.VersionTLS11
		case "tls1.2", "tlsv1.2":
			minVersion = tls.VersionTLS12
		case "tls1.3", "tlsv1.3":
			minVersion = tls.VersionTLS13
		default:
			log.Printf("Warning: Unrecognized TLS minimum version '%s', using TLS 1.0", config.TLSMinVersion)
		}
	}

	// Parse maximum TLS version from config
	if config.TLSMaxVersion != "" {
		switch strings.ToLower(config.TLSMaxVersion) {
		case "tls1.0", "tls1", "tlsv1.0", "tlsv1":
			maxVersion = tls.VersionTLS10
		case "tls1.1", "tlsv1.1":
			maxVersion = tls.VersionTLS11
		case "tls1.2", "tlsv1.2":
			maxVersion = tls.VersionTLS12
		case "tls1.3", "tlsv1.3":
			maxVersion = tls.VersionTLS13
		default:
			log.Printf("Warning: Unrecognized TLS maximum version '%s', using TLS 1.3", config.TLSMaxVersion)
		}
	}

	// Log TLS version configuration
	log.Printf("Using TLS version range: %s to %s",
		tlsVersionToString(minVersion),
		tlsVersionToString(maxVersion))

	tlsConfig := &tls.Config{
		Certificates:             []tls.Certificate{cert},
		MinVersion:               minVersion,
		MaxVersion:               maxVersion,
		PreferServerCipherSuites: true,
		InsecureSkipVerify:       true,
		ClientAuth:               tls.NoClientCert,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_128_CBC_SHA,
			tls.TLS_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA,
		},
	}

	listener, err := tls.Listen("tcp", fmt.Sprintf(":%d", config.TLSPort), tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to start TLS listener: %v", err)
	}
	defer listener.Close()

	log.Printf("TLS Proxy3270 listening on port %d", config.TLSPort)

	for {
		// Accept connections without a timeout - TLS listeners don't support SetDeadline
		// like TCP listeners do. We'll handle timeouts at the connection level instead.
		conn, err := listener.Accept()

		if err != nil {
			// Check if we should continue or return the error
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				log.Printf("Temporary TLS accept error: %v, continuing...", err)
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return fmt.Errorf("TLS accept error: %v", err)
		}

		// Handle each connection in a separate goroutine
		go handleTLSConnection(conn, config, debug, debug3270, trace)
	}
}

func handleTLSConnection(conn net.Conn, config *Config, debug, debug3270, trace bool) {
	// Ensure connection is always closed when we're done
	defer conn.Close()

	// For TLS connections, add a small delay to ensure handshake completes
	time.Sleep(500 * time.Millisecond)

	// Set initial timeout for telnet negotiation - use configured timeout or default to 60 seconds
	timeoutSeconds := 60
	if config.TLSTimeout > 0 {
		timeoutSeconds = config.TLSTimeout
	}
	conn.SetDeadline(time.Now().Add(time.Duration(timeoutSeconds) * time.Second))

	// Log TLS connection details if debugging is enabled
	if debug {
		if tlsConn, ok := conn.(*tls.Conn); ok {
			tlsState := tlsConn.ConnectionState()
			log.Printf("TLS Connection: Version=%v, CipherSuite=%v, HandshakeComplete=%v",
				tlsVersionToString(tlsState.Version),
				tls.CipherSuiteName(tlsState.CipherSuite),
				tlsState.HandshakeComplete)
		}
	}

	// Negotiate telnet protocol with direct error handling
	if err := go3270.NegotiateTelnet(conn); err != nil {
		log.Printf("TLS telnet negotiation failed: %v", err)
		return
	}

	// After successful negotiation, remove the deadline for regular operation
	conn.SetDeadline(time.Time{})

	// Handle authentication first
	authSession, err := HandleAuth(conn)
	if err != nil {
		log.Printf("TLS authentication failed: %v", err)
		if err.Error() == "user requested logoff with PF9" {
			log.Printf("TLS user terminated connection with PF9")
		}
		return
	}

	if !authSession.authenticated {
		log.Printf("TLS user authentication failed")
		return
	}

	log.Printf("TLS user %s authenticated successfully", authSession.username)

	// Create a copy of the config to override with user-specific settings if needed
	userConfig := *config

	// If user has a specific host file, use it
	if authSession.hostFile != "" {
		log.Printf("Using user-specific host file: %s", authSession.hostFile)
		userConfig.HostFile = authSession.hostFile

		// Load hosts from the user-specific file
		proxyData, err := os.ReadFile(userConfig.HostFile)
		if err != nil {
			log.Printf("Failed to read user host file %s: %v, falling back to default",
				userConfig.HostFile, err)
		} else {
			// Parse the hosts from the user's host file
			var hosts []Host
			if err := json.Unmarshal(proxyData, &hosts); err != nil {
				log.Printf("Failed to parse user host file %s: %v, falling back to default",
					userConfig.HostFile, err)
			} else {
				// Successfully loaded user's hosts
				userConfig.Hosts = hosts
			}
		}
	}

	// Now proceed with the normal proxy3270 host selection and connection handling
	handleProxyConnection(conn, &userConfig, authSession)
}

// tlsVersionToString converts a TLS version constant to a human-readable string
func tlsVersionToString(version uint16) string {
	switch version {
	case tls.VersionSSL30:
		return "SSLv3"
	case tls.VersionTLS10:
		return "TLSv1.0"
	case tls.VersionTLS11:
		return "TLSv1.1"
	case tls.VersionTLS12:
		return "TLSv1.2"
	case tls.VersionTLS13:
		return "TLSv1.3"
	default:
		return fmt.Sprintf("Unknown(0x%04X)", version)
	}
}

func main() {
	var (
		configFile = flag.String("config", "secure3270.cnf", "Configuration file")
		debug      = flag.Bool("debug", false, "Enable debug logging")
		debug3270  = flag.Bool("debug3270", false, "Enable debug output in go3270 library")
		trace      = flag.Bool("trace", false, "Enable trace logging")
	)
	flag.Parse()

	log.Printf("Secure3270Proxy starting...")
	log.Printf("Loading configuration from %s", *configFile)

	// Load configuration (includes both proxy hosts and authentication settings)
	config, err := loadConfig(*configFile)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Load authentcation configuraton from users.cnf
	if err := LoadAuthConfig(*configFile); err != nil {
		log.Fatalf("Failed to load authentication config: %v", err)
	}
	log.Printf("Authentication configuration loaded successfully from users.cnf")

	// Start TLS server in a goroutine if configured and enabled
	if config.TLSEnabled && config.TLSPort > 0 {
		go startTLSServer(config, *debug, *debug3270, *trace)
	}

	// Start non-TLS listener with auto-recovery
	go startStandardServer(config, *debug, *debug3270, *trace)

	// Keep the main goroutine running
	select {}
}

func startStandardServer(config *Config, debug, debug3270, trace bool) {
	for {
		startTime := time.Now()
		if err := runStandardServer(config, debug, debug3270, trace); err != nil {
			log.Printf("Standard server error: %v", err)

			// If the server ran for a reasonable amount of time before failing,
			// it's likely a temporary issue, so we can restart immediately
			if time.Since(startTime) > 5*time.Minute {
				log.Printf("Standard server restarting immediately...")
			} else {
				// If it failed quickly, there might be a more serious issue
				// Wait before retrying to avoid rapid restart loops
				log.Printf("Standard server will restart in 30 seconds...")
				time.Sleep(30 * time.Second)
			}
		} else {
			// Normal shutdown - wait before restarting
			log.Printf("Standard server shut down, restarting in 10 seconds...")
			time.Sleep(10 * time.Second)
		}
	}
}

func runStandardServer(config *Config, debug, debug3270, trace bool) error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", config.Port))
	if err != nil {
		return fmt.Errorf("failed to start standard listener: %v", err)
	}
	defer listener.Close()

	log.Printf("Proxy3270 listening on port %d", config.Port)
	log.Printf("Secure3270Proxy startup complete")

	// Safely access the underlying TCP listener to set deadlines
	tcpListener, ok := listener.(*net.TCPListener)
	if !ok {
		return fmt.Errorf("expected TCP listener but got %T", listener)
	}

	for {
		// Accept connections with a timeout to allow for periodic health checks
		tcpListener.SetDeadline(time.Now().Add(1 * time.Minute))
		conn, err := tcpListener.Accept()

		if err != nil {
			// Check if this is just a timeout (which we use for health checking)
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue // This is just our periodic timeout, not a real error
			}
			return fmt.Errorf("Standard accept error: %v", err)
		}

		// Handle each connection in a separate goroutine
		go handleStandardConnection(conn, config, debug, debug3270, trace)
	}
}

func handleStandardConnection(conn net.Conn, config *Config, debug, debug3270, trace bool) {
	// Ensure connection is always closed when we're done
	defer conn.Close()

	// Set initial timeout for telnet negotiation
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	// Negotiate telnet protocol with direct error handling
	if err := go3270.NegotiateTelnet(conn); err != nil {
		log.Printf("Standard telnet negotiation failed: %v", err)
		return
	}

	// After successful negotiation, remove the deadline for regular operation
	conn.SetDeadline(time.Time{})

	// Handle authentication first
	authSession, err := HandleAuth(conn)
	if err != nil {
		log.Printf("Standard authentication failed: %v", err)
		if err.Error() == "user requested logoff with PF9" {
			log.Printf("Standard user terminated connection with PF9")
		}
		return
	}

	if !authSession.authenticated {
		log.Printf("Standard user authentication failed")
		return
	}

	log.Printf("Standard user %s authenticated successfully", authSession.username)

	// Create a copy of the config to override with user-specific settings if needed
	userConfig := *config

	// If user has a specific host file, use it
	if authSession.hostFile != "" {
		log.Printf("Using user-specific host file: %s", authSession.hostFile)
		userConfig.HostFile = authSession.hostFile

		// Load hosts from the user-specific file
		proxyData, err := os.ReadFile(userConfig.HostFile)
		if err != nil {
			log.Printf("Failed to read user host file %s: %v, falling back to default",
				userConfig.HostFile, err)
		} else {
			// Parse the hosts from the user's host file
			var hosts []Host
			if err := json.Unmarshal(proxyData, &hosts); err != nil {
				log.Printf("Failed to parse user host file %s: %v, falling back to default",
					userConfig.HostFile, err)
			} else {
				// Successfully loaded user's hosts
				userConfig.Hosts = hosts
			}
		}
	}

	// Now proceed with the normal proxy3270 host selection and connection handling
	handleProxyConnection(conn, &userConfig, authSession)
}

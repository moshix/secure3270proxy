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
:wq
*/
type Host struct {
	Name string `json:"name"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

type Config struct {
	Hosts      []Host
	Port       int
	TLSPort    int
	TLSCert    string
	TLSKey     string
	HostFile   string // Path to the hosts configuration file
	TLSEnabled bool   // Flag to enable/disable TLS
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

	cert, err := tls.LoadX509KeyPair(config.TLSCert, config.TLSKey)
	if err != nil {
		log.Printf("Failed to load TLS certificates: %v", err)
		return
	}

	tlsConfig := &tls.Config{
		Certificates:             []tls.Certificate{cert},
		MinVersion:               tls.VersionTLS10, // Very permissive - TLS 1.0
		MaxVersion:               tls.VersionTLS13,
		PreferServerCipherSuites: true,
		InsecureSkipVerify:       true, // Very permisive
	}

	listener, err := tls.Listen("tcp", fmt.Sprintf(":%d", config.TLSPort), tlsConfig)
	if err != nil {
		log.Printf("Failed to start TLS listener: %v", err)
		return
	}
	defer listener.Close()

	log.Printf("TLS Proxy3270 listening on port %d", config.TLSPort)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("TLS accept error: %v", err)
			continue
		}

		go handleConnection(conn, config, debug, debug3270, trace)
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

	// Start non-TLS listener
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", config.Port))
	if err != nil {
		log.Fatalf("Failed to start listener: %v", err)
	}
	defer listener.Close()

	log.Printf("Proxy3270 listening on port %d", config.Port)
	log.Printf("Secure3270Proxy startup complete")

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		go handleConnection(conn, config, *debug, *debug3270, *trace)
	}
}

func handleConnection(conn net.Conn, config *Config, debug, debug3270, trace bool) {
	defer conn.Close()

	// Set connection timeouts
	conn.SetDeadline(time.Now().Add(5 * time.Minute))

	// Negotiate telnet protocol for 3270 emulation
	if err := go3270.NegotiateTelnet(conn); err != nil {
		log.Printf("Telnet negotiation failed: %v", err)
		return
	}

	// Handle authentication first
	authSession, err := HandleAuth(conn)
	if err != nil {
		log.Printf("Authentication failed: %v", err)
		// If user pressed PF9 to logoff, it's an expected termination
		if err.Error() == "user requested logoff with PF9" {
			log.Printf("User terminated connection with PF9")
		}
		return
	}

	if !authSession.authenticated {
		log.Printf("User authentication failed")
		return
	}

	log.Printf("User %s authenticated successfully", authSession.username)

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

	// Now procede with the normaal proxy3270 host selection and connection handlin
	handleProxyConnection(conn, &userConfig, authSession)
}

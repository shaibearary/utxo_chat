// Copyright (c) 2025 UTXOchat developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"runtime/trace"
	"syscall"

	"github.com/shaibearary/utxo_chat/bitcoin"
	"github.com/shaibearary/utxo_chat/blockchain"
	"github.com/shaibearary/utxo_chat/database"
	"github.com/shaibearary/utxo_chat/message"
	"github.com/shaibearary/utxo_chat/network"
	"github.com/shaibearary/utxo_chat/utils"
)

const (
	// dbNamePrefix is the prefix for the UTXOchat database name.
	dbNamePrefix = "utxochat"
)

var (
	cfg *config
)

// utxoChatMain is the real main function for UTXOchat. It is necessary to work around
// the fact that deferred functions do not run when os.Exit() is called.
func utxoChatMain() error {
	// Load configuration and parse command line.
	tcfg, err := loadConfig()
	if err != nil {
		return err
	}
	cfg = tcfg
	defer func() {
		if logRotator != nil {
			logRotator.Close()
		}
	}()

	// Show version at startup.
	log.Printf("UTXOchat Version %s", version())

	// Get a channel that will be closed when a shutdown signal has been
	// triggered either from an OS signal such as SIGINT (Ctrl+C) or from
	// another subsystem such as the RPC server.
	interrupt := interruptListener()
	defer log.Println("Shutdown complete")

	// Enable http profiling server if requested.
	if cfg.Debug.Profile != "" {
		go func() {
			listenAddr := net.JoinHostPort("", cfg.Debug.Profile)
			log.Printf("Profile server listening on %s", listenAddr)
			profileRedirect := http.RedirectHandler("/debug/pprof",
				http.StatusSeeOther)
			http.Handle("/", profileRedirect)
			log.Printf("%v", http.ListenAndServe(listenAddr, nil))
		}()
	}

	// Write cpu profile if requested.
	if cfg.Debug.CPUProfile != "" {
		f, err := os.Create(cfg.Debug.CPUProfile)
		if err != nil {
			log.Printf("Unable to create cpu profile: %v", err)
			return err
		}
		pprof.StartCPUProfile(f)
		defer f.Close()
		defer pprof.StopCPUProfile()
	}

	// Write mem profile if requested.
	if cfg.Debug.MemoryProfile != "" {
		f, err := os.Create(cfg.Debug.MemoryProfile)
		if err != nil {
			log.Printf("Unable to create memory profile: %v", err)
			return err
		}
		defer f.Close()
		defer pprof.WriteHeapProfile(f)
		defer runtime.GC()
	}

	// Write execution trace if requested.
	if cfg.Debug.TraceProfile != "" {
		f, err := os.Create(cfg.Debug.TraceProfile)
		if err != nil {
			log.Printf("Unable to create execution trace: %v", err)
			return err
		}
		defer f.Close()
		defer trace.Stop()
	}

	// Perform upgrades to UTXOchat as new versions require it.
	if err := doUpgrades(); err != nil {
		log.Printf("%v", err)
		return err
	}

	// Return now if an interrupt signal was triggered.
	if interruptRequested(interrupt) {
		return nil
	}

	// Ensure data directory exists.
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		log.Printf("Failed to create data directory: %v", err)
		return err
	}

	// Create context that can be canceled on shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize Bitcoin client.
	bitcoinClient, err := newBitcoinClient(cfg.Bitcoin)
	if err != nil {
		log.Printf("Failed to initialize Bitcoin client: %v", err)
		return err
	}

	// Check Bitcoin connection.
	info, err := bitcoinClient.GetBlockchainInfo(ctx)
	if err != nil {
		log.Printf("Failed to connect to Bitcoin node: %v", err)
		return err
	}
	log.Printf("Connected to Bitcoin node, chain: %s, blocks: %d", info.Chain, info.Blocks)

	// Initialize database.
	db, err := database.New(database.Config{
		Type: database.Type(cfg.Database.Type),
		Path: cfg.Database.Path,
	})
	if err != nil {
		log.Printf("Failed to initialize database: %v", err)
		return err
	}
	defer func() {
		// Ensure the database is sync'd and closed on shutdown.
		log.Printf("Gracefully shutting down the database...")
		db.Close()
	}()

	// Return now if an interrupt signal was triggered.
	if interruptRequested(interrupt) {
		return nil
	}

	// Initialize message validator.
	validator := message.NewValidator(bitcoinClient, db)

	// Initialize P2P network.
	networkCfg := network.Config{
		ListenAddr:       cfg.Network.ListenAddr,
		KnownPeers:       cfg.Network.KnownPeers,
		HandshakeTimeout: cfg.Network.HandshakeTimeout,
	}
	networkManager, err := network.NewManager(networkCfg, validator, db)
	if err != nil {
		log.Printf("Failed to initialize network: %v", err)
		return err
	}
	// Start services.
	if err := networkManager.Start(ctx); err != nil {
		log.Printf("Failed to start network: %v", err)
		return err
	}

	// Start block notification handler for cleaning up spent outpoints.
	blockHandler := blockchain.NewHandlerWithConfig(bitcoinClient, db, blockchain.Config{
		NotificationsEnabled: cfg.Blockchain.NotificationsEnabled,
		MaxReorgDepth:        cfg.Blockchain.MaxReorgDepth,
		ScanFullBlocks:       cfg.Blockchain.ScanFullBlocks,
		PollInterval:         cfg.Blockchain.PollInterval,
	})
	if err := blockHandler.Start(ctx); err != nil {
		log.Printf("Failed to start block handler: %v", err)
		return err
	}

	// Print startup information.
	log.Printf("UTXOchat is running on %s", cfg.Network.ListenAddr)
	log.Printf("Data directory: %s", cfg.DataDir)

	// Wait until the interrupt signal is received from an OS signal or
	// shutdown is requested through one of the subsystems.
	<-interrupt

	// Cancel context to signal all services to shut down.
	cancel()

	// Shutdown network.
	log.Printf("Gracefully shutting down network...")
	if err := networkManager.Stop(); err != nil {
		log.Printf("Error stopping network: %v", err)
	}

	// Shutdown block handler.
	log.Printf("Gracefully shutting down block handler...")
	if err := blockHandler.Stop(); err != nil {
		log.Printf("Error stopping block handler: %v", err)
	}

	return nil
}

// interruptListener returns a channel that will be closed when an interrupt
// signal is received.
func interruptListener() chan struct{} {
	c := make(chan struct{})
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-interruptChan
		signal.Stop(interruptChan)
		close(c)
	}()
	return c
}

// interruptRequested returns true if the channel returned by interruptListener
// was closed indicating an interrupt signal was received.
func interruptRequested(interrupted <-chan struct{}) bool {
	select {
	case <-interrupted:
		return true
	default:
	}
	return false
}

// doUpgrades performs any necessary upgrades to the UTXOchat data directory.
func doUpgrades() error {
	// Perform any upgrades to the UTXOchat data directory here.
	return nil
}

// version returns the version of the UTXOchat software.
func version() string {
	return "0.1.0"
}

// logRotator is the log file rotator used by UTXOchat.
var logRotator rotator

// rotator is an interface that is satisfied by log file rotators.
type rotator interface {
	Close() error
}

// loadConfig initializes and parses the config using command line options.
func loadConfig() (*config, error) {
	// Get the default data directory for the specified operating system
	defaultDataDir := utils.AppDataDir("utxochat", false)
	// Parse command line flags
	configPath := flag.String("config", "config.json", "Path to configuration file")
	dataDir := flag.String("datadir", defaultDataDir, "Data directory")
	profile := flag.String("profile", "", "Enable HTTP profiling on given port")
	cpuProfile := flag.String("cpuprofile", "", "Write CPU profile to the specified file")
	memProfile := flag.String("memprofile", "", "Write memory profile to the specified file")
	traceProfile := flag.String("traceprofile", "", "Write execution trace to the specified file")
	debugFlag := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	// Set up logging
	if *debugFlag {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	// Try to load config from file
	var cfg config
	file, err := os.Open(*configPath)
	if err != nil {
		// If config file doesn't exist, use defaults
		if os.IsNotExist(err) {
			log.Printf("Config file not found at %s, using defaults and command line options", *configPath)
			return &config{
				DataDir: *dataDir,
				Network: networkConfig{
					ListenAddr:       "0.0.0.0:8335",
					KnownPeers:       []string{},
					HandshakeTimeout: 60,
				},
				Bitcoin: bitcoinConfig{
					RPCURL:     "http://localhost:8332",
					RPCUser:    "",
					RPCPass:    "",
					DisableTLS: true,
				},
				Database: databaseConfig{
					Type: string(database.TypeMemory),
					Path: filepath.Join(*dataDir, dbNamePrefix+".db"),
				},
				Blockchain: blockchainConfig{
					NotificationsEnabled: true,
					MaxReorgDepth:        6,
					ScanFullBlocks:       true,
					PollInterval:         30,
				},
				Message: messageConfig{
					MaxPayloadSize: 65434,
					MaxMessageSize: 65536,
				},
				Debug: debugConfig{
					Profile:       *profile,
					CPUProfile:    *cpuProfile,
					MemoryProfile: *memProfile,
					TraceProfile:  *traceProfile,
					LogLevel:      "info",
				},
			}, nil
		}
		return nil, fmt.Errorf("error opening config file: %v", err)
	}
	defer file.Close()

	// Decode the config file
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("error decoding config file: %v", err)
	}

	// Override with command line flags if specified
	if *dataDir != defaultDataDir {
		cfg.DataDir = *dataDir
	}
	if *profile != "" {
		cfg.Debug.Profile = *profile
	}
	if *cpuProfile != "" {
		cfg.Debug.CPUProfile = *cpuProfile
	}
	if *memProfile != "" {
		cfg.Debug.MemoryProfile = *memProfile
	}
	if *traceProfile != "" {
		cfg.Debug.TraceProfile = *traceProfile
	}

	// Validate required fields
	if cfg.DataDir == "" {
		cfg.DataDir = *dataDir
	}
	if cfg.Network.ListenAddr == "" {
		cfg.Network.ListenAddr = "0.0.0.0:8335"
	}
	if cfg.Network.HandshakeTimeout == 0 {
		cfg.Network.HandshakeTimeout = 60
	}
	if cfg.Bitcoin.RPCURL == "" {
		cfg.Bitcoin.RPCURL = "http://localhost:8332"
	}
	if cfg.Database.Type == "" {
		cfg.Database.Type = string(database.TypeMemory)
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = filepath.Join(cfg.DataDir, dbNamePrefix+".db")
	}
	if cfg.Blockchain.MaxReorgDepth == 0 {
		cfg.Blockchain.MaxReorgDepth = 6
	}
	if cfg.Blockchain.PollInterval == 0 {
		cfg.Blockchain.PollInterval = 30
	}
	if cfg.Message.MaxPayloadSize == 0 {
		cfg.Message.MaxPayloadSize = 65434
	}
	if cfg.Message.MaxMessageSize == 0 {
		cfg.Message.MaxMessageSize = 65536
	}
	if cfg.Debug.LogLevel == "" {
		cfg.Debug.LogLevel = "info"
	}

	return &cfg, nil
}

// config defines the configuration options for UTXOchat.
type config struct {
	DataDir    string
	Network    networkConfig
	Bitcoin    bitcoinConfig
	Database   databaseConfig
	Blockchain blockchainConfig
	Message    messageConfig
	Debug      debugConfig
}

// networkConfig defines the network configuration for UTXOchat.
type networkConfig struct {
	ListenAddr       string
	KnownPeers       []string
	HandshakeTimeout int
}

// bitcoinConfig defines the Bitcoin node configuration for UTXOchat.
type bitcoinConfig struct {
	RPCURL     string
	RPCUser    string
	RPCPass    string
	DisableTLS bool
}

// databaseConfig defines the database configuration for UTXOchat.
type databaseConfig struct {
	Type string
	Path string
}

// blockchainConfig defines the blockchain configuration for UTXOchat.
type blockchainConfig struct {
	NotificationsEnabled bool
	MaxReorgDepth        int32
	ScanFullBlocks       bool
	PollInterval         int
}

// messageConfig defines the message configuration for UTXOchat.
type messageConfig struct {
	MaxPayloadSize int
	MaxMessageSize int
}

// debugConfig defines the debug configuration for UTXOchat.
type debugConfig struct {
	Profile       string
	CPUProfile    string
	MemoryProfile string
	TraceProfile  string
	LogLevel      string
}

// Update newBitcoinClient to use the new package
func newBitcoinClient(cfg bitcoinConfig) (*bitcoin.Client, error) {
	return bitcoin.NewClient(bitcoin.Config{
		RPCURL:  cfg.RPCURL,
		RPCUser: cfg.RPCUser,
		RPCPass: cfg.RPCPass,
	})
}

func main() {
	// If GOGC is not explicitly set, override GC percent.
	if os.Getenv("GOGC") == "" {
		// Block and transaction processing can cause bursty allocations.
		debug.SetGCPercent(10)
	}

	// Work around defer not working after os.Exit()
	if err := utxoChatMain(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// Copyright (c) 2023 UTXOchat developers
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
	if cfg.Profile != "" {
		go func() {
			listenAddr := net.JoinHostPort("", cfg.Profile)
			log.Printf("Profile server listening on %s", listenAddr)
			profileRedirect := http.RedirectHandler("/debug/pprof",
				http.StatusSeeOther)
			http.Handle("/", profileRedirect)
			log.Printf("%v", http.ListenAndServe(listenAddr, nil))
		}()
	}

	// Write cpu profile if requested.
	if cfg.CPUProfile != "" {
		f, err := os.Create(cfg.CPUProfile)
		if err != nil {
			log.Printf("Unable to create cpu profile: %v", err)
			return err
		}
		pprof.StartCPUProfile(f)
		defer f.Close()
		defer pprof.StopCPUProfile()
	}

	// Write mem profile if requested.
	if cfg.MemoryProfile != "" {
		f, err := os.Create(cfg.MemoryProfile)
		if err != nil {
			log.Printf("Unable to create memory profile: %v", err)
			return err
		}
		defer f.Close()
		defer pprof.WriteHeapProfile(f)
		defer runtime.GC()
	}

	// Write execution trace if requested.
	if cfg.TraceProfile != "" {
		f, err := os.Create(cfg.TraceProfile)
		if err != nil {
			log.Printf("Unable to create execution trace: %v", err)
			return err
		}
		trace.Start(f)
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
	info, err := bitcoinClient.getBlockchainInfo(ctx)
	if err != nil {
		log.Printf("Failed to connect to Bitcoin node: %v", err)
		return err
	}
	log.Printf("Connected to Bitcoin node, chain: %s, blocks: %d", info.Chain, info.Blocks)

	// Initialize database.
	db, err := newDB(filepath.Join(cfg.DataDir, dbNamePrefix+".db"))
	if err != nil {
		log.Printf("Failed to initialize database: %v", err)
		return err
	}
	defer func() {
		// Ensure the database is sync'd and closed on shutdown.
		log.Printf("Gracefully shutting down the database...")
		db.close()
	}()

	// Return now if an interrupt signal was triggered.
	if interruptRequested(interrupt) {
		return nil
	}

	// Initialize message validator.
	validator := newValidator(bitcoinClient, db)

	// Initialize P2P network.
	networkManager, err := newNetworkManager(cfg.Network, validator, db)
	if err != nil {
		log.Printf("Failed to initialize network: %v", err)
		return err
	}
	// Start services.
	if err := networkManager.start(ctx); err != nil {
		log.Printf("Failed to start network: %v", err)
		return err
	}

	// Start block notification handler for cleaning up spent outpoints.
	blockHandler := newBlockHandler(bitcoinClient, db)
	if err := blockHandler.start(ctx); err != nil {
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
	if err := networkManager.stop(); err != nil {
		log.Printf("Error stopping network: %v", err)
	}

	// Shutdown block handler.
	log.Printf("Gracefully shutting down block handler...")
	if err := blockHandler.stop(); err != nil {
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
	print(defaultDataDir)

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
					ListenAddr: "0.0.0.0:8334",
				},
				Bitcoin: bitcoinConfig{
					RPCURL:  "http://localhost:8332",
					RPCUser: "",
					RPCPass: "",
				},
				Profile:       *profile,
				CPUProfile:    *cpuProfile,
				MemoryProfile: *memProfile,
				TraceProfile:  *traceProfile,
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
		cfg.Profile = *profile
	}
	if *cpuProfile != "" {
		cfg.CPUProfile = *cpuProfile
	}
	if *memProfile != "" {
		cfg.MemoryProfile = *memProfile
	}
	if *traceProfile != "" {
		cfg.TraceProfile = *traceProfile
	}

	// Validate required fields
	if cfg.DataDir == "" {
		cfg.DataDir = *dataDir
	}
	if cfg.Network.ListenAddr == "" {
		cfg.Network.ListenAddr = "0.0.0.0:8334"
	}
	if cfg.Bitcoin.RPCURL == "" {
		cfg.Bitcoin.RPCURL = "http://localhost:8332"
	}

	return &cfg, nil
}

// config defines the configuration options for UTXOchat.
type config struct {
	DataDir       string
	Network       networkConfig
	Bitcoin       bitcoinConfig
	Profile       string
	CPUProfile    string
	MemoryProfile string
	TraceProfile  string
}

// networkConfig defines the network configuration for UTXOchat.
type networkConfig struct {
	ListenAddr string
}

// bitcoinConfig defines the Bitcoin node configuration for UTXOchat.
type bitcoinConfig struct {
	RPCURL  string
	RPCUser string
	RPCPass string
}

// These types and functions are placeholders for the actual implementations.
// They will be implemented in separate files.

// Bitcoin client related types and functions
type bitcoinClient struct {
	config bitcoinConfig
}

type blockchainInfo struct {
	Chain  string
	Blocks int64
}

func newBitcoinClient(cfg bitcoinConfig) (*bitcoinClient, error) {
	// TODO: Implement Bitcoin client
	return &bitcoinClient{
		config: cfg,
	}, nil
}

func (c *bitcoinClient) getBlockchainInfo(ctx context.Context) (*blockchainInfo, error) {
	// TODO: Implement GetBlockchainInfo
	return &blockchainInfo{
		Chain:  "main",
		Blocks: 123456,
	}, nil
}

// Block handler related types and functions
type blockHandler struct {
	client *bitcoinClient
	db     *db
}

func newBlockHandler(client *bitcoinClient, db *db) *blockHandler {
	// TODO: Implement block handler
	return &blockHandler{
		client: client,
		db:     db,
	}
}

func (h *blockHandler) start(ctx context.Context) error {
	// TODO: Implement Start
	return nil
}

func (h *blockHandler) stop() error {
	// TODO: Implement Stop
	return nil
}

// Database related types and functions
type db struct {
	path string
}

func newDB(path string) (*db, error) {
	// TODO: Implement NewDB
	return &db{
		path: path,
	}, nil
}

func (d *db) close() error {
	// TODO: Implement Close
	return nil
}

// Message validation related types and functions
type validator struct {
	client *bitcoinClient
	db     *db
}

func newValidator(client *bitcoinClient, db *db) *validator {
	// TODO: Implement NewValidator
	return &validator{
		client: client,
		db:     db,
	}
}

// Network related types and functions
type networkManager struct {
	config    networkConfig
	validator *validator
	db        *db
}

func newNetworkManager(cfg networkConfig, v *validator, db *db) (*networkManager, error) {
	// TODO: Implement NewManager
	return &networkManager{
		config:    cfg,
		validator: v,
		db:        db,
	}, nil
}

func (m *networkManager) start(ctx context.Context) error {
	// TODO: Implement Start
	return nil
}

func (m *networkManager) stop() error {
	// TODO: Implement Stop
	return nil
}

// Application layer related types and functions
type appManager struct {
	network *networkManager
	db      *db
	client  *bitcoinClient
}

func newAppManager(nm *networkManager, db *db, client *bitcoinClient) *appManager {
	// TODO: Implement NewManager
	return &appManager{
		network: nm,
		db:      db,
		client:  client,
	}
}

func (m *appManager) start(ctx context.Context) error {
	// TODO: Implement Start
	return nil
}

func (m *appManager) stop() error {
	// TODO: Implement Stop
	return nil
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

// Package config loads runtime configuration from the environment. Every value
// has a default; secrets default to insecure placeholders that trigger a
// startup warning (and block production) if left in place.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the fully-resolved application configuration.
type Config struct {
	// HTTP
	Port string
	Env  string // "development" | "production"

	// Ethereum
	EthRPCURL string // JSON-RPC endpoint (defaults to a public Sepolia node)
	ChainID   int64  // 11155111 = Sepolia

	// Keystore (private keys, encrypted at rest via Web3 Secret Storage)
	KeystoreDir        string
	KeystorePassphrase string

	// Auth
	JWTSecret         string
	JWTIssuer         string
	JWTExpiry         time.Duration
	OAuthClientID     string
	OAuthClientSecret string

	// Observability
	LogLevel string // "debug" | "info" | "warn" | "error"
}

// Sentinel defaults that are insecure for production. Load() warns if they survive.
const (
	defaultJWTSecret          = "dev-insecure-jwt-secret-change-me"
	defaultKeystorePassphrase = "dev-insecure-passphrase-change-me"
	defaultOAuthClientSecret  = "dev-insecure-client-secret-change-me"
)

// Load reads configuration from the environment, applying defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Port:               getEnv("PORT", "8080"),
		Env:                getEnv("APP_ENV", "development"),
		EthRPCURL:          getEnv("ETH_RPC_URL", "https://ethereum-sepolia-rpc.publicnode.com"),
		KeystoreDir:        getEnv("KEYSTORE_DIR", "./data/keystore"),
		KeystorePassphrase: getEnv("KEYSTORE_PASSPHRASE", defaultKeystorePassphrase),
		JWTSecret:          getEnv("JWT_SECRET", defaultJWTSecret),
		JWTIssuer:          getEnv("JWT_ISSUER", "web3-wallet-api"),
		OAuthClientID:      getEnv("OAUTH_CLIENT_ID", "demo-client"),
		OAuthClientSecret:  getEnv("OAUTH_CLIENT_SECRET", defaultOAuthClientSecret),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
	}

	chainID, err := strconv.ParseInt(getEnv("CHAIN_ID", "11155111"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid CHAIN_ID: %w", err)
	}
	cfg.ChainID = chainID

	expiry, err := time.ParseDuration(getEnv("JWT_EXPIRY", "1h"))
	if err != nil {
		return nil, fmt.Errorf("invalid JWT_EXPIRY: %w", err)
	}
	cfg.JWTExpiry = expiry

	return cfg, nil
}

// UsesInsecureDefaults reports whether any production-sensitive secret is still
// at its built-in default. main() uses this to emit a startup warning.
func (c *Config) UsesInsecureDefaults() bool {
	return c.JWTSecret == defaultJWTSecret ||
		c.KeystorePassphrase == defaultKeystorePassphrase ||
		c.OAuthClientSecret == defaultOAuthClientSecret
}

// IsProduction reports whether the service is running in production mode.
func (c *Config) IsProduction() bool { return c.Env == "production" }

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

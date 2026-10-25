// Package config loads runtime configuration from environment variables.
//
// Every option has a sensible default so that `docker run` with a single
// volume works out of the box. See docs/configuration.md for the full list.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime settings.
type Config struct {
	Host           string
	Port           int
	BaseURL        string
	DataDir        string
	DBPath         string
	LogLevel       string
	LogFormat      string // text | json
	TrustProxy     bool
	MetricsEnabled bool

	// MaxBodyBytes caps a JSON request (an export being imported).
	MaxBodyBytes int64

	AuthMode             string // local | proxy | none
	AuthProxyHeader      string
	AuthProxyEmailHeader string
	SessionTTL           time.Duration
	SecureCookies        bool
	AdminEmail           string
	AdminPassword        string
	AdminName            string

	// Sign in with an OpenID Connect provider. On a box with Pocket Core none
	// of these are needed: the app uses the client Core issues on registration.
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCName         string // button label
	OIDCAutoCreate   bool   // unknown emails get a member account

	// The owner's integrations, copied once into the owner's own settings;
	// the values saved under Settings win.
	NotifyURL       string
	NotifyFormat    string // apprise | ntfy | json
	InventoryURL    string
	InventoryToken  string
	MonitoringURL   string
	MonitoringToken string
	OpenAIKey       string
	OpenAIModel     string
	OpenAIBaseURL   string

	// Scanning from this box. LocalScan lets admins sweep the networks this
	// machine is on (it needs host networking and CAP_NET_RAW in a container);
	// the owner starts with one scan of ScanInterface/ScanSubnet (default: the
	// interface of the default route).
	LocalScan     bool
	ScanInterface string
	ScanSubnet    string
	ScanInterval  time.Duration
	// The owner's other first sources: the local tailscaled (when its socket
	// is there) and AdGuard Home.
	TailscaleSocket string
	AdGuardURL      string
	AdGuardUser     string
	AdGuardPassword string
	// Where this server's public address is asked (text with "ip=" or just the address).
	WANCheckURL string

	// Outbound lookups identify as this.
	UserAgent string

	// Pocket Core: when CORE_URL is set the app registers itself on start,
	// publishes events and accepts the tokens Core hands to paired phones.
	CoreURL             string
	CoreRegistrationKey string
	CoreAppURL          string // how Core reaches this app; defaults to BASE_URL

	DemoData bool
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func envBool(key string, def bool) bool {
	switch strings.ToLower(env(key, "")) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func envInt(key string, def int) int {
	if v := env(key, ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envInt64(key string, def int64) int64 {
	if v := env(key, ""); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := env(key, ""); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// Load reads configuration from the environment.
func Load() (*Config, error) {
	dataDir := env("DATA_DIR", "./data")
	c := &Config{
		Host:           env("HOST", "0.0.0.0"),
		Port:           envInt("PORT", 8080),
		BaseURL:        strings.TrimRight(env("BASE_URL", ""), "/"),
		DataDir:        dataDir,
		DBPath:         env("DB_PATH", dataDir+"/pocket-network.db"),
		LogLevel:       strings.ToLower(env("LOG_LEVEL", "info")),
		LogFormat:      strings.ToLower(env("LOG_FORMAT", "text")),
		TrustProxy:     envBool("TRUST_PROXY", true),
		MetricsEnabled: envBool("METRICS_ENABLED", true),

		MaxBodyBytes: envInt64("MAX_BODY_BYTES", 64<<20),

		AuthMode:             strings.ToLower(env("AUTH_MODE", "local")),
		AuthProxyHeader:      env("AUTH_PROXY_HEADER", "Remote-User"),
		AuthProxyEmailHeader: env("AUTH_PROXY_EMAIL_HEADER", "Remote-Email"),
		SessionTTL:           envDuration("SESSION_TTL", 720*time.Hour),
		SecureCookies:        envBool("SECURE_COOKIES", false),
		AdminEmail:           strings.ToLower(env("ADMIN_EMAIL", "")),
		AdminPassword:        env("ADMIN_PASSWORD", ""),
		AdminName:            env("ADMIN_NAME", "Admin"),
		OIDCIssuer:           strings.TrimRight(env("OIDC_ISSUER", ""), "/"),
		OIDCClientID:         env("OIDC_CLIENT_ID", ""),
		OIDCClientSecret:     env("OIDC_CLIENT_SECRET", ""),
		OIDCName:             env("OIDC_NAME", "single sign-on"),
		OIDCAutoCreate:       envBool("OIDC_AUTO_CREATE", true),

		NotifyURL:       env("NOTIFY_URL", ""),
		NotifyFormat:    strings.ToLower(env("NOTIFY_FORMAT", "")),
		InventoryURL:    strings.TrimRight(env("INVENTORY_URL", ""), "/"),
		InventoryToken:  env("INVENTORY_TOKEN", ""),
		MonitoringURL:   strings.TrimRight(env("MONITORING_URL", ""), "/"),
		MonitoringToken: env("MONITORING_TOKEN", ""),
		OpenAIKey:       env("OPENAI_API_KEY", ""),
		OpenAIModel:     env("OPENAI_MODEL", ""),
		OpenAIBaseURL:   strings.TrimRight(env("OPENAI_BASE_URL", ""), "/"),

		LocalScan:       envBool("LOCAL_SCAN", true),
		ScanInterface:   env("SCAN_INTERFACE", ""),
		ScanSubnet:      env("SCAN_SUBNET", ""),
		ScanInterval:    envDuration("SCAN_INTERVAL", 5*time.Minute),
		TailscaleSocket: env("TAILSCALE_SOCKET", "/var/run/tailscale/tailscaled.sock"),
		AdGuardURL:      strings.TrimRight(env("ADGUARD_URL", ""), "/"),
		AdGuardUser:     env("ADGUARD_USER", ""),
		AdGuardPassword: env("ADGUARD_PASSWORD", ""),
		WANCheckURL:     env("WAN_CHECK_URL", "https://1.1.1.1/cdn-cgi/trace"),

		UserAgent: env("USER_AGENT", "PocketNetwork/1.0 (+https://github.com/abjelosevic88/pocket-homelab-network)"),

		CoreURL:             strings.TrimRight(env("CORE_URL", ""), "/"),
		CoreRegistrationKey: env("CORE_REGISTRATION_KEY", ""),
		CoreAppURL:          strings.TrimRight(env("CORE_APP_URL", ""), "/"),

		DemoData: envBool("DEMO_DATA", false),
	}
	if c.CoreAppURL == "" {
		c.CoreAppURL = c.BaseURL
	}
	switch c.AuthMode {
	case "local", "proxy", "none":
	default:
		return nil, fmt.Errorf("AUTH_MODE must be one of local, proxy, none (got %q)", c.AuthMode)
	}
	if c.Port <= 0 || c.Port > 65535 {
		return nil, fmt.Errorf("PORT out of range: %d", c.Port)
	}
	if c.OIDCIssuer != "" && (c.OIDCClientID == "" || c.BaseURL == "") {
		return nil, fmt.Errorf("OIDC_ISSUER needs OIDC_CLIENT_ID and BASE_URL (the callback is BASE_URL/api/v1/auth/oidc/callback)")
	}
	return c, nil
}

// Addr returns host:port.
func (c *Config) Addr() string { return fmt.Sprintf("%s:%d", c.Host, c.Port) }

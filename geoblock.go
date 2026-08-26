// Package traefik_geoblock_plugin implements a Traefik middleware plugin for geoblocking
package traefik_geoblock_plugin

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// CountryUnknown represents an unknown country code
	CountryUnknown = "UNKNOWN"
	// DefaultActionAllow represents the default allow action
	DefaultActionAllow = "allow"
	// ActionAllow represents the allow action
	ActionAllow = "allow"
	// ActionBlock represents the block action
	ActionBlock = "block"

	LevelInfo    = "info"
	LevelWarning = "warning"
)

// Config holds the plugin configuration
type Config struct {
	BlockPageTitle        string   `json:"blockPageTitle,omitempty"`
	MetricsLogPath        string   `json:"metricsLogPath,omitempty"`
	QueryURL              string   `json:"queryURL,omitempty"`
	DatabaseURL           string   `json:"databaseURL,omitempty"`
	DatabasePath          string   `json:"databasePath,omitempty"`
	PrometheusMetricsPath string   `json:"prometheusMetricsPath,omitempty"`
	DefaultAction         string   `json:"defaultAction,omitempty"`
	BlockMessage          string   `json:"blockMessage,omitempty"`
	LogLevel              string   `json:"logLevel,omitempty"`
	RedirectURL           string   `json:"redirectURL,omitempty"`
	BlockPageBody         string   `json:"blockPageBody,omitempty"`
	BlockedCountries      []string `json:"blockedCountries,omitempty"`
	TrustedProxies        []string `json:"trustedProxies,omitempty"`
	AllowedCountries      []string `json:"allowedCountries,omitempty"`
	MetricsFlushSeconds   int      `json:"metricsFlushSeconds,omitempty"`
	LogRetentionDays      int      `json:"logRetentionDays,omitempty"`
	CacheDuration         int      `json:"cacheDuration,omitempty"`
	EnableMetricsLog      bool     `json:"enableMetricsLog,omitempty"`
}

// CreateConfig creates the default plugin configuration
func CreateConfig() *Config {
	return &Config{
		AllowedCountries:    []string{},
		BlockedCountries:    []string{},
		QueryURL:            "https://ipapi.co/{ip}/json/",
		DatabaseURL:         "",
		DatabasePath:        "/tmp/ipinfo_lite.json",
		CacheDuration:       60,
		DefaultAction:       DefaultActionAllow,
		BlockMessage:        "Access denied from your country",
		BlockPageTitle:      "Access Denied",
		BlockPageBody:       "",
		RedirectURL:         "",
		LogLevel:            LevelInfo,
		TrustedProxies:      []string{},
		MetricsLogPath:      "/var/log/traefik-geoblock/metrics.log",
		MetricsFlushSeconds: 60,
		LogRetentionDays:    14,
		EnableMetricsLog:    false,
	}
}

// GeoBlock holds the plugin state
type GeoBlock struct {
	next              http.Handler
	config            *Config
	cache             *geoCache
	localDB           *localDatabase
	allowedCountries  map[string]bool
	blockedCountries  map[string]bool
	trustedProxies    map[string]bool
	metricsAggregator *metricsAggregator
	promMetrics       *prometheusMetrics
	name              string
}

func (g *GeoBlock) log(level string, format string, v ...interface{}) {
	configLevel := strings.ToLower(g.config.LogLevel)

	// Determine if we should log based on level
	shouldLog := false
	switch configLevel {
	case "debug":
		// Debug logs everything
		shouldLog = true
	case LevelInfo:
		// Info logs info and warning
		if level == LevelInfo || level == LevelWarning {
			shouldLog = true
		}
	case LevelWarning:
		// Warning logs only warning
		if level == LevelWarning {
			shouldLog = true
		}
	default:
		// Default to info behavior
		if level == LevelInfo || level == LevelWarning {
			shouldLog = true
		}
	}

	if shouldLog {
		timestamp := time.Now().UTC().Format(time.RFC3339)
		msg := fmt.Sprintf(format, v...)
		fmt.Printf("[%s] [GeoBlock] [%s] %s\n", timestamp, strings.ToUpper(level), msg)
	}
}

type geoCache struct {
	entries map[string]*cacheEntry
	mu      sync.RWMutex
}

type cacheEntry struct {
	expiresAt    time.Time
	country      string
	organization string
}

type localDatabase struct {
	lastUpdate  time.Time
	downloadURL string
	filePath    string
	ranges      []ipRange
	mu          sync.RWMutex
}

type ipRange struct {
	country string
	startIP net.IP
	endIP   net.IP
}

type ipInfoLiteEntry struct {
	StartIP string `json:"start_ip"`
	EndIP   string `json:"end_ip"`
	Country string `json:"country"`
}

type ipAPIResponse struct {
	IP           string `json:"ip"`
	Country      string `json:"country_code"` // ipapi.co format
	CountryCode  string `json:"countryCode"`  // ip-api.com format
	CountryISO   string `json:"country"`      // ipinfo.io format
	CountryName  string `json:"country_name"`
	Organization string `json:"org"`    // ipapi.co/ipinfo.io format
	ISP          string `json:"isp"`    // ip-api.com format
	AS           string `json:"as"`     // Alternative org format
	ASName       string `json:"asname"` // Alternative org format
}

// Metrics structures for Grafana-compatible logging

type metricsAggregator struct {
	metrics       map[string]*metricEntry
	logger        *log.Logger
	logFile       *os.File
	logPath       string
	flushSeconds  int
	retentionDays int
	mu            sync.RWMutex
}

type metricEntry struct {
	Country      string
	Organization string
	Action       string // "allowed" or "blocked"
	Count        int64
}

type metricLogEntry struct {
	Timestamp    string `json:"timestamp"`
	Country      string `json:"country"`
	Organization string `json:"organization,omitempty"`
	Action       string `json:"action"`
	Count        int64  `json:"count"`
}

type geoInfo struct {
	Country      string
	Organization string
}

// Prometheus metrics structures for native Prometheus integration

type prometheusMetrics struct {
	counters map[string]int64
	mu       sync.RWMutex
}

// New creates a new GeoBlock plugin
func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	setConfigDefaults(config)

	allowedCountries := parseCountryList(config.AllowedCountries)
	blockedCountries := parseCountryList(config.BlockedCountries)

	gb := &GeoBlock{
		next:             next,
		config:           config,
		name:             name,
		cache:            &geoCache{entries: make(map[string]*cacheEntry)},
		allowedCountries: allowedCountries,
		blockedCountries: blockedCountries,
	}

	trustedProxies := gb.parseTrustedProxies(config.TrustedProxies)
	gb.trustedProxies = trustedProxies

	if err := initializeMetrics(ctx, config, gb); err != nil {
		return nil, err
	}

	initializeLocalDatabase(ctx, config, gb)

	return gb, nil
}

// setConfigDefaults sets default values for configuration options
func setConfigDefaults(config *Config) {
	if config.QueryURL == "" {
		config.QueryURL = "https://ipapi.co/{ip}/json/"
	}
	if config.DatabasePath == "" {
		config.DatabasePath = "/tmp/ipinfo_lite.json"
	}
	if config.CacheDuration <= 0 {
		config.CacheDuration = 60
	}
	if config.DefaultAction != DefaultActionAllow && config.DefaultAction != "block" {
		config.DefaultAction = DefaultActionAllow
	}
	if config.BlockMessage == "" {
		config.BlockMessage = "Access denied from your country"
	}
	if config.BlockPageTitle == "" {
		config.BlockPageTitle = "Access Denied"
	}
	if config.LogLevel == "" {
		config.LogLevel = LevelInfo
	}
}

// parseCountryList parses a list of country codes, supporting comma-separated values
func parseCountryList(countries []string) map[string]bool {
	countryMap := make(map[string]bool)
	for _, country := range countries {
		// Support comma-separated country codes in a single string
		if strings.Contains(country, ",") {
			for _, c := range strings.Split(country, ",") {
				c = strings.TrimSpace(c)
				if c != "" {
					countryMap[strings.ToUpper(c)] = true
				}
			}
		} else {
			countryMap[strings.ToUpper(country)] = true
		}
	}
	return countryMap
}

// parseTrustedProxies parses trusted proxy configuration, including Cloudflare support
func (g *GeoBlock) parseTrustedProxies(proxies []string) map[string]bool {
	trustedProxies := make(map[string]bool)
	cloudflareEnabled := false

	for _, proxy := range proxies {
		if strings.ToLower(strings.TrimSpace(proxy)) == "cloudflare" {
			cloudflareEnabled = true
		} else {
			trustedProxies[proxy] = true
		}
	}

	// Fetch Cloudflare IPs if requested
	if cloudflareEnabled {
		cloudflareIPs, err := fetchCloudflareIPs()
		if err != nil {
			g.log(LevelWarning, "Failed to fetch Cloudflare IPs: %v. Continuing without them.", err)
		} else {
			for _, ip := range cloudflareIPs {
				trustedProxies[ip] = true
			}
			g.log(LevelInfo, "Loaded %d Cloudflare IP ranges as trusted proxies", len(cloudflareIPs))
		}
	}

	return trustedProxies
}

// initializeMetrics initializes Prometheus and legacy JSON metrics
func initializeMetrics(ctx context.Context, config *Config, gb *GeoBlock) error {
	// Initialize Prometheus metrics if path is configured
	if config.PrometheusMetricsPath != "" {
		gb.promMetrics = &prometheusMetrics{
			counters: make(map[string]int64),
		}
		gb.log(LevelInfo, "Prometheus metrics enabled at path: %s", config.PrometheusMetricsPath)
	}

	// Initialize metrics aggregator if enabled (legacy JSON logging)
	if config.EnableMetricsLog {
		if config.MetricsFlushSeconds <= 0 {
			config.MetricsFlushSeconds = 60
		}
		if config.LogRetentionDays <= 0 {
			config.LogRetentionDays = 14
		}

		aggregator, err := newMetricsAggregator(config.MetricsLogPath, config.MetricsFlushSeconds, config.LogRetentionDays)
		if err != nil {
			return fmt.Errorf("failed to initialize metrics aggregator: %w", err)
		}
		gb.metricsAggregator = aggregator

		// Start background flusher
		go gb.metricsAggregator.startFlusher(ctx)
	}

	return nil
}

// initializeLocalDatabase initializes the local GeoIP database if configured
func initializeLocalDatabase(ctx context.Context, config *Config, gb *GeoBlock) {
	if config.DatabaseURL == "" {
		return
	}

	gb.localDB = &localDatabase{
		downloadURL: config.DatabaseURL,
		filePath:    config.DatabasePath,
		ranges:      make([]ipRange, 0),
	}

	// Initial database load
	if err := gb.loadLocalDatabase(); err != nil {
		gb.log(LevelWarning, "Failed to load local database: %v. Will use query API as fallback.", err)
	} else {
		gb.log(LevelInfo, "Local database loaded successfully with %d IP ranges", len(gb.localDB.ranges))
	}

	// Start background updater
	go gb.databaseUpdater(ctx)
}

func (g *GeoBlock) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// Check if this is a Prometheus metrics request
	if g.config.PrometheusMetricsPath != "" && req.URL.Path == g.config.PrometheusMetricsPath {
		g.servePrometheusMetrics(rw)
		return
	}

	ip := g.getClientIP(req)
	if ip == "" {
		g.next.ServeHTTP(rw, req)
		return
	}

	// Fast path: bypass if the resolved client IP is explicitly in a trusted proxy/allowed list
	if g.isTrustedProxy(ip) {
		g.log("debug", "Allowed request from IP %s (bypass via allowed/trusted proxy list)", ip)
		g.next.ServeHTTP(rw, req)
		return
	}

	geoInfo, isCached, err := g.getGeoInfo(ip)
	if err != nil {
		g.log(LevelWarning, "Error getting country for IP %s: %v", ip, err)
		// On error, apply default action
		if g.config.DefaultAction == "block" {
			g.log("debug", "Request from IP %s - Source: %s - Country: %s - Action: blocked (default action on error)", ip, sourceString(isCached), CountryUnknown)
			g.blockRequest(rw, CountryUnknown)
			g.recordMetrics(CountryUnknown, "", "blocked")
			return
		}
		g.log("debug", "Request from IP %s - Source: %s - Country: %s - Action: allowed (default action on error)", ip, sourceString(isCached), CountryUnknown)
		g.next.ServeHTTP(rw, req)
		return
	}

	if g.shouldBlock(geoInfo.Country) {
		g.log("debug", "Request from IP %s - Source: %s - Country: %s - Action: blocked", ip, sourceString(isCached), geoInfo.Country)
		g.blockRequest(rw, geoInfo.Country)
		g.recordMetrics(geoInfo.Country, geoInfo.Organization, "blocked")
		return
	}

	g.log("debug", "Request from IP %s - Source: %s - Country: %s - Action: allowed", ip, sourceString(isCached), geoInfo.Country)

	// Record allowed metric
	g.recordMetrics(geoInfo.Country, geoInfo.Organization, "allowed")

	// Add country header for downstream services
	req.Header.Set("X-Country-Code", geoInfo.Country)
	if geoInfo.Organization != "" {
		req.Header.Set("X-Organization", geoInfo.Organization)
	}
	g.next.ServeHTTP(rw, req)
}

func sourceString(isCached bool) string {
	if isCached {
		return "Cache"
	}
	return "API"
}

func (g *GeoBlock) getClientIP(req *http.Request) string {
	// Check X-Forwarded-For header
	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		// Get the first non-trusted proxy IP
		for _, ip := range ips {
			ip = strings.TrimSpace(ip)
			if !g.isTrustedProxy(ip) && net.ParseIP(ip) != nil {
				return ip
			}
		}
	}

	// Check X-Real-IP header
	if xri := req.Header.Get("X-Real-IP"); xri != "" {
		ip := strings.TrimSpace(xri)
		if net.ParseIP(ip) != nil {
			return ip
		}
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}

func (g *GeoBlock) isTrustedProxy(ip string) bool {
	// Check exact match first (for backward compatibility and performance)
	if g.trustedProxies[ip] {
		return true
	}

	// Parse the IP
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	// Check if IP is within any trusted CIDR range
	for proxyRange := range g.trustedProxies {
		// Check if it's a CIDR range
		if strings.Contains(proxyRange, "/") {
			_, network, err := net.ParseCIDR(proxyRange)
			if err != nil {
				continue
			}
			if network.Contains(parsedIP) {
				return true
			}
		}
	}

	return false
}

func (g *GeoBlock) getGeoInfo(ip string) (*geoInfo, bool, error) { // Returns info, isCached, err
	// Check if it's a private/local IP
	if g.isPrivateIP(ip) {
		return &geoInfo{Country: "PRIVATE", Organization: ""}, false, nil
	}

	// Check cache first
	if info := g.cache.get(ip); info != nil {
		return info, true, nil
	}

	var info *geoInfo
	var err error

	// Use local database if available
	if g.localDB != nil && len(g.localDB.ranges) > 0 {
		country := g.lookupLocalDatabase(ip)
		if country != "" && country != CountryUnknown {
			info = &geoInfo{Country: country, Organization: ""}
			// Try to get organization from API
			if apiInfo, apiErr := g.queryGeoIP(ip); apiErr == nil {
				info.Organization = apiInfo.Organization
			}
			g.cache.set(ip, info, time.Duration(g.config.CacheDuration)*time.Minute)
			return info, false, nil
		}
	}

	// Fallback to API query
	info, err = g.queryGeoIP(ip)
	if err != nil {
		return nil, false, err
	}

	// Cache the result
	g.cache.set(ip, info, time.Duration(g.config.CacheDuration)*time.Minute)

	return info, false, nil
}

func (g *GeoBlock) queryGeoIP(ip string) (*geoInfo, error) {
	url := strings.Replace(g.config.QueryURL, "{ip}", ip, 1)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to query geo IP: %w", err)
	}
	g.log(LevelInfo, "Queried API endpoint: %s", url)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geo IP API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var data ipAPIResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Try different field names used by various GeoIP APIs
	country := data.Country // ipapi.co: country_code
	if country == "" {
		country = data.CountryCode // ip-api.com: countryCode
	}
	if country == "" {
		country = data.CountryISO // ipinfo.io: country
	}

	if country == "" {
		// Log the raw response for debugging
		g.log(LevelWarning, "Could not extract country from API response. Raw response: %s", string(body))
		return &geoInfo{Country: CountryUnknown, Organization: ""}, nil
	}

	// Extract organization information
	organization := data.Organization
	if organization == "" {
		organization = data.ISP
	}
	if organization == "" {
		organization = data.ASName
	}
	if organization == "" {
		organization = data.AS
	}

	return &geoInfo{
		Country:      strings.ToUpper(country),
		Organization: organization,
	}, nil
}

func (g *GeoBlock) isPrivateIP(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	// Check for private IP ranges
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}

	for _, cidr := range privateRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(parsedIP) {
			return true
		}
	}

	return false
}

func (g *GeoBlock) shouldBlock(country string) bool {
	country = strings.ToUpper(country)

	// If allowed countries list is specified, only allow those
	if len(g.allowedCountries) > 0 {
		return !g.allowedCountries[country]
	}

	// If blocked countries list is specified, block those
	if len(g.blockedCountries) > 0 {
		return g.blockedCountries[country]
	}

	// Default action
	return g.config.DefaultAction == ActionBlock
}

func (g *GeoBlock) blockRequest(rw http.ResponseWriter, country string) {
	// We removed logBlocked, these details are now handled in ServeHTTP with debug level logs.
	// However, we can log to debug level here as well if we want, but ServeHTTP already does it.
	// No output needed here unless it's info level if you want, but user said debug logs every request.

	// If redirect URL is configured, redirect instead of showing block page
	if g.config.RedirectURL != "" {
		http.Redirect(rw, &http.Request{}, g.config.RedirectURL, http.StatusFound)
		return
	}

	// Generate HTML block page
	blockPage := g.generateBlockPage(country)

	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.WriteHeader(http.StatusForbidden)
	fmt.Fprint(rw, blockPage)
}

func (g *GeoBlock) generateBlockPage(country string) string {
	title := g.config.BlockPageTitle
	message := g.config.BlockMessage
	body := g.config.BlockPageBody

	// If custom body is provided, use it
	if body != "" {
		return g.generateCustomBlockPage(title, message, body, country)
	}

	// Default block page
	return g.generateDefaultBlockPage(title, message, country)
}

func (g *GeoBlock) generateCustomBlockPage(title, message, body, country string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s</title>
    <style>%s</style>
</head>
<body>
    <div class="container">
        <div class="icon">🚫</div>
        <h1>%s</h1>
        <div class="message">%s</div>
        <div class="custom-body">%s</div>
        <div class="country-info">
            Detected Country: <span class="country-code">%s</span>
        </div>
    </div>
</body>
</html>`, title, getCustomBlockPageStyles(), title, message, body, country)
}

func (g *GeoBlock) generateDefaultBlockPage(title, message, country string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s</title>
    <style>%s</style>
</head>
<body>
    <div class="container">
        <div class="icon">🚫</div>
        <h1>%s</h1>
        <div class="message">%s</div>
        <div class="country-info">
            Detected Country: <span class="country-code">%s</span>
        </div>
        <div class="footer">
            If you believe this is an error, please contact the website administrator.
        </div>
    </div>
</body>
</html>`, title, getDefaultBlockPageStyles(), title, message, country)
}

func getCustomBlockPageStyles() string {
	return `* { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 20px;
        }
        .container {
            background: white;
            border-radius: 16px;
            box-shadow: 0 20px 60px rgba(0, 0, 0, 0.3);
            max-width: 600px;
            width: 100%;
            padding: 40px;
            text-align: center;
        }
        .icon { font-size: 64px; margin-bottom: 20px; }
        h1 { color: #2d3748; font-size: 28px; margin-bottom: 16px; font-weight: 600; }
        .message { color: #4a5568; font-size: 16px; line-height: 1.6; margin-bottom: 24px; }
        .custom-body { color: #718096; font-size: 14px; line-height: 1.8; margin-top: 20px; padding-top: 20px; border-top: 1px solid #e2e8f0; }
        .country-info { background: #f7fafc; border-radius: 8px; padding: 12px 16px; display: inline-block; color: #4a5568; font-size: 14px; margin-top: 20px; }
        .country-code { font-weight: 600; color: #667eea; }`
}

func getDefaultBlockPageStyles() string {
	return `* { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 20px;
        }
        .container {
            background: white;
            border-radius: 16px;
            box-shadow: 0 20px 60px rgba(0, 0, 0, 0.3);
            max-width: 500px;
            width: 100%;
            padding: 40px;
            text-align: center;
            animation: slideIn 0.3s ease-out;
        }
        @keyframes slideIn {
            from { opacity: 0; transform: translateY(-20px); }
            to { opacity: 1; transform: translateY(0); }
        }
        .icon { font-size: 64px; margin-bottom: 20px; animation: pulse 2s infinite; }
        @keyframes pulse {
            0%, 100% { transform: scale(1); }
            50% { transform: scale(1.05); }
        }
        h1 { color: #2d3748; font-size: 28px; margin-bottom: 16px; font-weight: 600; }
        .message { color: #4a5568; font-size: 16px; line-height: 1.6; margin-bottom: 24px; }
        .country-info { background: #f7fafc; border-radius: 8px; padding: 12px 16px; display: inline-block; color: #4a5568; font-size: 14px; }
        .country-code { font-weight: 600; color: #667eea; }
        .footer { margin-top: 30px; padding-top: 20px; border-top: 1px solid #e2e8f0; color: #a0aec0; font-size: 12px; }`
}

// Local database functions

func (g *GeoBlock) loadLocalDatabase() error {
	// Try to load from existing file first
	if err := g.loadDatabaseFromFile(); err == nil {
		// Check if database is recent (less than 24 hours old)
		if time.Since(g.localDB.lastUpdate) < 24*time.Hour {
			return nil
		}
	}

	// Download fresh database
	return g.downloadDatabase()
}

func (g *GeoBlock) loadDatabaseFromFile() error {
	g.localDB.mu.Lock()
	defer g.localDB.mu.Unlock()

	file, err := os.Open(g.localDB.filePath)
	if err != nil {
		return fmt.Errorf("failed to open database file: %w", err)
	}
	defer file.Close()

	// Get file modification time
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat database file: %w", err)
	}
	g.localDB.lastUpdate = stat.ModTime()

	decoder := json.NewDecoder(file)
	var entries []ipInfoLiteEntry

	if err := decoder.Decode(&entries); err != nil {
		return fmt.Errorf("failed to decode database: %w", err)
	}

	// Convert to IP ranges
	g.localDB.ranges = make([]ipRange, 0, len(entries))
	for _, entry := range entries {
		startIP := net.ParseIP(entry.StartIP)
		endIP := net.ParseIP(entry.EndIP)
		if startIP != nil && endIP != nil {
			g.localDB.ranges = append(g.localDB.ranges, ipRange{
				startIP: startIP,
				endIP:   endIP,
				country: strings.ToUpper(entry.Country),
			})
		}
	}

	return nil
}

func (g *GeoBlock) downloadDatabase() error {

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(g.localDB.downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download database: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("database download returned status %d", resp.StatusCode)
	}

	// Create temporary file
	tmpFile, err := os.CreateTemp("", "ipinfo_lite_*.json.gz")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Download to temp file
	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return fmt.Errorf("failed to save database: %w", err)
	}

	// Reopen for reading
	if _, err := tmpFile.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek temp file: %w", err)
	}

	// Decompress gzip
	gzReader, err := gzip.NewReader(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	// Parse JSON
	var entries []ipInfoLiteEntry
	decoder := json.NewDecoder(gzReader)
	if err := decoder.Decode(&entries); err != nil {
		return fmt.Errorf("failed to decode database: %w", err)
	}

	// Convert to IP ranges and update database
	g.localDB.mu.Lock()
	g.localDB.ranges = make([]ipRange, 0, len(entries))
	for _, entry := range entries {
		startIP := net.ParseIP(entry.StartIP)
		endIP := net.ParseIP(entry.EndIP)
		if startIP != nil && endIP != nil {
			g.localDB.ranges = append(g.localDB.ranges, ipRange{
				startIP: startIP,
				endIP:   endIP,
				country: strings.ToUpper(entry.Country),
			})
		}
	}
	g.localDB.lastUpdate = time.Now()
	g.localDB.mu.Unlock()

	// Save to persistent file (uncompressed JSON for faster loading)
	outFile, err := os.Create(g.localDB.filePath)
	if err != nil {
		return fmt.Errorf("failed to create database file: %w", err)
	}
	defer outFile.Close()

	encoder := json.NewEncoder(outFile)
	if err := encoder.Encode(entries); err != nil {
		return fmt.Errorf("failed to save database: %w", err)
	}

	return nil
}

func (g *GeoBlock) databaseUpdater(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			g.log(LevelInfo, "Starting daily database update...")
			if err := g.downloadDatabase(); err != nil {
				g.log(LevelWarning, "Failed to update database: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (g *GeoBlock) lookupLocalDatabase(ip string) string {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return CountryUnknown
	}

	g.localDB.mu.RLock()
	defer g.localDB.mu.RUnlock()

	// Binary search would be faster, but for simplicity using linear search
	// In production, consider sorting ranges and using binary search
	for _, r := range g.localDB.ranges {
		if ipInRange(parsedIP, r.startIP, r.endIP) {
			return r.country
		}
	}

	return CountryUnknown
}

func ipInRange(ip, start, end net.IP) bool {
	// Convert to 16-byte format for comparison
	ip = ip.To16()
	start = start.To16()
	end = end.To16()

	if ip == nil || start == nil || end == nil {
		return false
	}

	// Compare bytes
	return bytes.Compare(ip, start) >= 0 && bytes.Compare(ip, end) <= 0
}

func (c *geoCache) get(ip string) *geoInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[ip]
	if !exists {
		return nil
	}

	if time.Now().After(entry.expiresAt) {
		return nil
	}

	return &geoInfo{
		Country:      entry.country,
		Organization: entry.organization,
	}
}

func (c *geoCache) set(ip string, info *geoInfo, duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[ip] = &cacheEntry{
		country:      info.Country,
		organization: info.Organization,
		expiresAt:    time.Now().Add(duration),
	}

	// Simple cleanup: remove expired entries periodically
	if len(c.entries) > 10000 {
		now := time.Now()
		for key, entry := range c.entries {
			if now.After(entry.expiresAt) {
				delete(c.entries, key)
			}
		}
	}
}

// Metrics aggregator implementation for Grafana-compatible logging

func newMetricsAggregator(logPath string, flushSeconds, retentionDays int) (*metricsAggregator, error) {
	// Create log directory if it doesn't exist
	logDir := filepath.Dir(logPath)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open log file in append mode
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	logger := log.New(logFile, "", 0) // No prefix or flags, we'll use JSON

	ma := &metricsAggregator{
		metrics:       make(map[string]*metricEntry),
		logPath:       logPath,
		flushSeconds:  flushSeconds,
		retentionDays: retentionDays,
		logger:        logger,
		logFile:       logFile,
	}

	return ma, nil
}

func (ma *metricsAggregator) recordMetric(country, organization, action string) {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	// Create a unique key for this country+organization+action combination
	key := fmt.Sprintf("%s|%s|%s", country, organization, action)

	if entry, exists := ma.metrics[key]; exists {
		entry.Count++
	} else {
		ma.metrics[key] = &metricEntry{
			Country:      country,
			Organization: organization,
			Action:       action,
			Count:        1,
		}
	}
}

func (ma *metricsAggregator) startFlusher(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(ma.flushSeconds) * time.Second)
	defer ticker.Stop()

	// Also run cleanup daily
	cleanupTicker := time.NewTicker(24 * time.Hour)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ticker.C:
			ma.flush()
		case <-cleanupTicker.C:
			ma.cleanupOldLogs()
		case <-ctx.Done():
			ma.flush() // Final flush before shutdown
			ma.close()
			return
		}
	}
}

func (ma *metricsAggregator) flush() {
	ma.mu.Lock()
	defer ma.mu.Unlock()

	if len(ma.metrics) == 0 {
		return
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)

	// Write each metric as a JSON line
	for _, entry := range ma.metrics {
		logEntry := metricLogEntry{
			Timestamp:    timestamp,
			Country:      entry.Country,
			Organization: entry.Organization,
			Action:       entry.Action,
			Count:        entry.Count,
		}

		jsonData, err := json.Marshal(logEntry)
		if err != nil {
			fmt.Printf("[GeoBlock] Error marshaling metric: %v\n", err)
			continue
		}

		ma.logger.Println(string(jsonData))
	}

	// Clear metrics after flushing
	ma.metrics = make(map[string]*metricEntry)

	// Sync to disk
	if ma.logFile != nil {
		if err := ma.logFile.Sync(); err != nil {
			fmt.Printf("[GeoBlock] Error syncing log file: %v\n", err)
		}
	}
}

func (ma *metricsAggregator) cleanupOldLogs() {
	logDir := filepath.Dir(ma.logPath)
	logBase := filepath.Base(ma.logPath)

	files, err := os.ReadDir(logDir)
	if err != nil {
		fmt.Printf("[GeoBlock] Error reading log directory: %v\n", err)
		return
	}

	cutoffTime := time.Now().AddDate(0, 0, -ma.retentionDays)

	for _, file := range files {
		// Check if file is a rotated log file
		if !strings.HasPrefix(file.Name(), logBase) {
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		// Delete if older than retention period
		if info.ModTime().Before(cutoffTime) {
			filePath := filepath.Join(logDir, file.Name())
			if err := os.Remove(filePath); err != nil {
				fmt.Printf("[GeoBlock] Error removing old log file %s: %v\n", filePath, err)
			} else {
				fmt.Printf("[GeoBlock] Removed old log file: %s\n", filePath)
			}
		}
	}
}

func (ma *metricsAggregator) close() {
	if ma.logFile != nil {
		ma.logFile.Close()
	}
}

// Prometheus metrics implementation

func (g *GeoBlock) recordMetrics(country, organization, action string) {
	// Record to legacy JSON aggregator if enabled
	if g.metricsAggregator != nil {
		g.metricsAggregator.recordMetric(country, organization, action)
	}

	// Record to Prometheus metrics if enabled
	if g.promMetrics != nil {
		g.promMetrics.increment(country, organization, action)
	}
}

func (pm *prometheusMetrics) increment(country, organization, action string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	key := fmt.Sprintf("%s|%s|%s", country, organization, action)
	pm.counters[key]++
}

func (g *GeoBlock) servePrometheusMetrics(rw http.ResponseWriter) {
	if g.promMetrics == nil {
		http.Error(rw, "Metrics not enabled", http.StatusNotFound)
		return
	}

	metrics := g.promMetrics.render()

	rw.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	rw.WriteHeader(http.StatusOK)
	if _, err := rw.Write([]byte(metrics)); err != nil {
		fmt.Printf("[GeoBlock] Error writing metrics response: %v\n", err)
	}
}

func (pm *prometheusMetrics) render() string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var buf strings.Builder

	// Write metric header
	buf.WriteString("# HELP traefik_geoblock_requests_total Total number of requests processed by geoblock plugin\n")
	buf.WriteString("# TYPE traefik_geoblock_requests_total counter\n")

	// Write metrics
	for key, count := range pm.counters {
		parts := strings.Split(key, "|")
		if len(parts) != 3 {
			continue
		}

		country := parts[0]
		organization := parts[1]
		action := parts[2]

		// Escape label values for Prometheus format
		country = escapePrometheusLabel(country)
		organization = escapePrometheusLabel(organization)
		action = escapePrometheusLabel(action)

		if organization != "" {
			buf.WriteString(fmt.Sprintf("traefik_geoblock_requests_total{country=\"%s\",organization=\"%s\",action=\"%s\"} %d\n",
				country, organization, action, count))
		} else {
			buf.WriteString(fmt.Sprintf("traefik_geoblock_requests_total{country=\"%s\",action=\"%s\"} %d\n",
				country, action, count))
		}
	}

	return buf.String()
}

func escapePrometheusLabel(s string) string {
	// Escape backslashes, newlines, and double quotes for Prometheus label values
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// fetchCloudflareIPs fetches the list of Cloudflare IP ranges from their official endpoints
func fetchCloudflareIPs() ([]string, error) {
	var allIPs []string

	// Cloudflare's official IP list endpoints
	urls := []string{
		"https://www.cloudflare.com/ips-v4",
		"https://www.cloudflare.com/ips-v6",
	}

	client := &http.Client{Timeout: 10 * time.Second}

	for _, url := range urls {
		resp, err := client.Get(url)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch Cloudflare IPs from %s: %w", url, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("Cloudflare IP endpoint returned status %d for %s", resp.StatusCode, url)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read Cloudflare IP response: %w", err)
		}

		// Parse the response (one IP range per line)
		lines := strings.Split(string(body), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				allIPs = append(allIPs, line)
			}
		}
	}

	if len(allIPs) == 0 {
		return nil, fmt.Errorf("no Cloudflare IPs found")
	}

	return allIPs, nil
}

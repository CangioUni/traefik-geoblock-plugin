// Package traefik_geoblock_plugin implements a Traefik middleware plugin for geoblocking
package traefik_geoblock_plugin

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Config holds the plugin configuration
type Config struct {
	AllowedCountries []string `json:"allowedCountries,omitempty"`
	BlockedCountries []string `json:"blockedCountries,omitempty"`
	QueryURL         string   `json:"queryURL,omitempty"`      // API endpoint for querying (e.g., https://ipapi.co/{ip}/json/)
	DatabaseURL      string   `json:"databaseURL,omitempty"`   // URL to download local database (e.g., https://ipinfo.io/data/ipinfo_lite.json.gz?token=TOKEN)
	DatabasePath     string   `json:"databasePath,omitempty"`  // Path to store local database
	CacheDuration    int      `json:"cacheDuration,omitempty"` // in minutes
	DefaultAction    string   `json:"defaultAction,omitempty"` // "allow" or "block"
	BlockMessage     string   `json:"blockMessage,omitempty"`
	BlockPageTitle   string   `json:"blockPageTitle,omitempty"`
	BlockPageBody    string   `json:"blockPageBody,omitempty"`
	RedirectURL      string   `json:"redirectURL,omitempty"` // URL to redirect blocked users (optional)
	LogBlocked       bool     `json:"logBlocked,omitempty"`
	TrustedProxies   []string `json:"trustedProxies,omitempty"`
}

// CreateConfig creates the default plugin configuration
func CreateConfig() *Config {
	return &Config{
		AllowedCountries: []string{},
		BlockedCountries: []string{},
		QueryURL:         "https://ipapi.co/{ip}/json/",
		DatabaseURL:      "",
		DatabasePath:     "/tmp/ipinfo_lite.json",
		CacheDuration:    60,
		DefaultAction:    "allow",
		BlockMessage:     "Access denied from your country",
		BlockPageTitle:   "Access Denied",
		BlockPageBody:    "",
		RedirectURL:      "",
		LogBlocked:       true,
		TrustedProxies:   []string{},
	}
}

// GeoBlock holds the plugin state
type GeoBlock struct {
	next             http.Handler
	config           *Config
	name             string
	cache            *geoCache
	localDB          *localDatabase
	allowedCountries map[string]bool
	blockedCountries map[string]bool
	trustedProxies   map[string]bool
}

type geoCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
}

type cacheEntry struct {
	country   string
	expiresAt time.Time
}

type localDatabase struct {
	mu          sync.RWMutex
	ranges      []ipRange
	lastUpdate  time.Time
	downloadURL string
	filePath    string
}

type ipRange struct {
	startIP net.IP
	endIP   net.IP
	country string
}

type ipInfoLiteEntry struct {
	StartIP string `json:"start_ip"`
	EndIP   string `json:"end_ip"`
	Country string `json:"country"`
}

type ipAPIResponse struct {
	IP          string `json:"ip"`
	Country     string `json:"country_code"` // ipapi.co format
	CountryCode string `json:"countryCode"`  // ip-api.com format
	CountryISO  string `json:"country"`      // ipinfo.io format
	CountryName string `json:"country_name"`
}

// New creates a new GeoBlock plugin
func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config.QueryURL == "" {
		config.QueryURL = "https://ipapi.co/{ip}/json/"
	}

	if config.DatabasePath == "" {
		config.DatabasePath = "/tmp/ipinfo_lite.json"
	}

	if config.CacheDuration <= 0 {
		config.CacheDuration = 60
	}

	if config.DefaultAction != "allow" && config.DefaultAction != "block" {
		config.DefaultAction = "allow"
	}

	if config.BlockMessage == "" {
		config.BlockMessage = "Access denied from your country"
	}

	if config.BlockPageTitle == "" {
		config.BlockPageTitle = "Access Denied"
	}

	// Create maps for faster lookup
	allowedCountries := make(map[string]bool)
	for _, country := range config.AllowedCountries {
		allowedCountries[strings.ToUpper(country)] = true
	}

	blockedCountries := make(map[string]bool)
	for _, country := range config.BlockedCountries {
		blockedCountries[strings.ToUpper(country)] = true
	}

	trustedProxies := make(map[string]bool)
	for _, proxy := range config.TrustedProxies {
		trustedProxies[proxy] = true
	}

	gb := &GeoBlock{
		next:             next,
		config:           config,
		name:             name,
		cache:            &geoCache{entries: make(map[string]*cacheEntry)},
		allowedCountries: allowedCountries,
		blockedCountries: blockedCountries,
		trustedProxies:   trustedProxies,
	}

	// Initialize local database if configured
	if config.DatabaseURL != "" {
		gb.localDB = &localDatabase{
			downloadURL: config.DatabaseURL,
			filePath:    config.DatabasePath,
			ranges:      make([]ipRange, 0),
		}

		// Initial database load
		if err := gb.loadLocalDatabase(); err != nil {
			fmt.Printf("[GeoBlock] Warning: Failed to load local database: %v. Will use query API as fallback.\n", err)
		} else {
			fmt.Printf("[GeoBlock] Local database loaded successfully with %d IP ranges\n", len(gb.localDB.ranges))
		}

		// Start background updater
		go gb.databaseUpdater(ctx)
	}

	return gb, nil
}

func (g *GeoBlock) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	ip := g.getClientIP(req)
	if ip == "" {
		g.next.ServeHTTP(rw, req)
		return
	}

	country, err := g.getCountry(ip)
	if err != nil {
		if g.config.LogBlocked {
			fmt.Printf("[GeoBlock] Error getting country for IP %s: %v\n", ip, err)
		}
		// On error, apply default action
		if g.config.DefaultAction == "block" {
			g.blockRequest(rw, ip, "UNKNOWN")
			return
		}
		g.next.ServeHTTP(rw, req)
		return
	}

	if g.shouldBlock(country) {
		g.blockRequest(rw, ip, country)
		return
	}

	// Add country header for downstream services
	req.Header.Set("X-Country-Code", country)
	g.next.ServeHTTP(rw, req)
}

func (g *GeoBlock) getClientIP(req *http.Request) string {
	// Check X-Forwarded-For header
	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		// Get the first non-trusted proxy IP
		for _, ip := range ips {
			ip = strings.TrimSpace(ip)
			if !g.trustedProxies[ip] && net.ParseIP(ip) != nil {
				return ip
			}
		}
	}

	// Check X-Real-IP header
	if xri := req.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}

func (g *GeoBlock) getCountry(ip string) (string, error) {
	// Check if it's a private/local IP
	if g.isPrivateIP(ip) {
		return "PRIVATE", nil
	}

	// Check cache first
	if country := g.cache.get(ip); country != "" {
		return country, nil
	}

	var country string
	var err error

	// Use local database if available
	if g.localDB != nil && len(g.localDB.ranges) > 0 {
		country = g.lookupLocalDatabase(ip)
		if country != "" && country != "UNKNOWN" {
			g.cache.set(ip, country, time.Duration(g.config.CacheDuration)*time.Minute)
			return country, nil
		}
	}

	// Fallback to API query
	country, err = g.queryGeoIP(ip)
	if err != nil {
		return "", err
	}

	// Cache the result
	g.cache.set(ip, country, time.Duration(g.config.CacheDuration)*time.Minute)

	return country, nil
}

func (g *GeoBlock) queryGeoIP(ip string) (string, error) {
	url := strings.Replace(g.config.QueryURL, "{ip}", ip, 1)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to query geo IP: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("geo IP API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var data ipAPIResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
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
		if g.config.LogBlocked {
			fmt.Printf("[GeoBlock] Warning: Could not extract country from API response for IP %s. Raw response: %s\n", ip, string(body))
		}
		return "UNKNOWN", nil
	}

	return strings.ToUpper(country), nil
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
	return g.config.DefaultAction == "block"
}
func (g *GeoBlock) blockRequest(rw http.ResponseWriter, ip, country string) {
	if g.config.LogBlocked {
		fmt.Printf("[GeoBlock] Blocked request from IP %s (Country: %s)\n", ip, country)
	}

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
		return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);
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
            width: 100%%;
            padding: 40px;
            text-align: center;
        }
        .icon {
            font-size: 64px;
            margin-bottom: 20px;
        }
        h1 {
            color: #2d3748;
            font-size: 28px;
            margin-bottom: 16px;
            font-weight: 600;
        }
        .message {
            color: #4a5568;
            font-size: 16px;
            line-height: 1.6;
            margin-bottom: 24px;
        }
        .custom-body {
            color: #718096;
            font-size: 14px;
            line-height: 1.8;
            margin-top: 20px;
            padding-top: 20px;
            border-top: 1px solid #e2e8f0;
        }
        .country-info {
            background: #f7fafc;
            border-radius: 8px;
            padding: 12px 16px;
            display: inline-block;
            color: #4a5568;
            font-size: 14px;
            margin-top: 20px;
        }
        .country-code {
            font-weight: 600;
            color: #667eea;
        }
    </style>
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
</html>`, title, title, message, body, country)
	}

	// Default block page
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            background: linear-gradient(135deg, #667eea 0%%, #764ba2 100%%);
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
            width: 100%%;
            padding: 40px;
            text-align: center;
            animation: slideIn 0.3s ease-out;
        }
        @keyframes slideIn {
            from {
                opacity: 0;
                transform: translateY(-20px);
            }
            to {
                opacity: 1;
                transform: translateY(0);
            }
        }
        .icon {
            font-size: 64px;
            margin-bottom: 20px;
            animation: pulse 2s infinite;
        }
        @keyframes pulse {
            0%%, 100%% { transform: scale(1); }
            50%% { transform: scale(1.05); }
        }
        h1 {
            color: #2d3748;
            font-size: 28px;
            margin-bottom: 16px;
            font-weight: 600;
        }
        .message {
            color: #4a5568;
            font-size: 16px;
            line-height: 1.6;
            margin-bottom: 24px;
        }
        .country-info {
            background: #f7fafc;
            border-radius: 8px;
            padding: 12px 16px;
            display: inline-block;
            color: #4a5568;
            font-size: 14px;
        }
        .country-code {
            font-weight: 600;
            color: #667eea;
        }
        .footer {
            margin-top: 30px;
            padding-top: 20px;
            border-top: 1px solid #e2e8f0;
            color: #a0aec0;
            font-size: 12px;
        }
    </style>
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
</html>`, title, title, message, country)
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
	fmt.Printf("[GeoBlock] Downloading database from %s\n", g.localDB.downloadURL)

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
	tmpFile.Seek(0, 0)

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

	fmt.Printf("[GeoBlock] Database downloaded and loaded successfully with %d IP ranges\n", len(g.localDB.ranges))
	return nil
}

func (g *GeoBlock) databaseUpdater(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fmt.Println("[GeoBlock] Starting daily database update...")
			if err := g.downloadDatabase(); err != nil {
				fmt.Printf("[GeoBlock] Failed to update database: %v\n", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (g *GeoBlock) lookupLocalDatabase(ip string) string {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return "UNKNOWN"
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

	return "UNKNOWN"
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
func (c *geoCache) get(ip string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[ip]
	if !exists {
		return ""
	}

	if time.Now().After(entry.expiresAt) {
		return ""
	}

	return entry.country
}

func (c *geoCache) set(ip, country string, duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[ip] = &cacheEntry{
		country:   country,
		expiresAt: time.Now().Add(duration),
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

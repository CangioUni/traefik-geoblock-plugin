// Package traefik_geoblock_plugin implements a Traefik middleware plugin for geoblocking
package traefik_geoblock_plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Config holds the plugin configuration
type Config struct {
	AllowedCountries []string `json:"allowedCountries,omitempty"`
	BlockedCountries []string `json:"blockedCountries,omitempty"`
	DatabaseURL      string   `json:"databaseURL,omitempty"`
	CacheDuration    int      `json:"cacheDuration,omitempty"` // in minutes
	DefaultAction    string   `json:"defaultAction,omitempty"`  // "allow" or "block"
	BlockMessage     string   `json:"blockMessage,omitempty"`
	LogBlocked       bool     `json:"logBlocked,omitempty"`
	TrustedProxies   []string `json:"trustedProxies,omitempty"`
}

// CreateConfig creates the default plugin configuration
func CreateConfig() *Config {
	return &Config{
		AllowedCountries: []string{},
		BlockedCountries: []string{},
		DatabaseURL:      "https://ipapi.co/{ip}/json/",
		CacheDuration:    60,
		DefaultAction:    "allow",
		BlockMessage:     "Access denied from your country",
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

type ipAPIResponse struct {
	IP          string `json:"ip"`
	Country     string `json:"country_code"`
	CountryName string `json:"country_name"`
}

// New creates a new GeoBlock plugin
func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config.DatabaseURL == "" {
		config.DatabaseURL = "https://ipapi.co/{ip}/json/"
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

	return &GeoBlock{
		next:             next,
		config:           config,
		name:             name,
		cache:            &geoCache{entries: make(map[string]*cacheEntry)},
		allowedCountries: allowedCountries,
		blockedCountries: blockedCountries,
		trustedProxies:   trustedProxies,
	}, nil
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

	// Query the API
	country, err := g.queryGeoIP(ip)
	if err != nil {
		return "", err
	}

	// Cache the result
	g.cache.set(ip, country, time.Duration(g.config.CacheDuration)*time.Minute)

	return country, nil
}

func (g *GeoBlock) queryGeoIP(ip string) (string, error) {
	url := strings.Replace(g.config.DatabaseURL, "{ip}", ip, 1)

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

	if data.Country == "" {
		return "UNKNOWN", nil
	}

	return strings.ToUpper(data.Country), nil
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

	rw.Header().Set("Content-Type", "text/plain")
	rw.WriteHeader(http.StatusForbidden)
	fmt.Fprint(rw, g.config.BlockMessage)
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

package traefik_geoblock_plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateConfig(t *testing.T) {
	config := CreateConfig()

	if config.QueryURL == "" {
		t.Error("Default QueryURL should not be empty")
	}

	if config.CacheDuration != 60 {
		t.Errorf("Expected CacheDuration to be 60, got %d", config.CacheDuration)
	}

	if config.DefaultAction != "allow" {
		t.Errorf("Expected DefaultAction to be 'allow', got %s", config.DefaultAction)
	}
}

func TestNew(t *testing.T) {
	config := CreateConfig()
	config.AllowedCountries = []string{"US", "CA"}

	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	handler, err := New(context.Background(), next, config, "test-geoblock")
	if err != nil {
		t.Fatalf("Failed to create plugin: %v", err)
	}

	if handler == nil {
		t.Fatal("Handler should not be nil")
	}
}

func TestPrivateIPDetection(t *testing.T) {
	config := CreateConfig()
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	handler, err := New(context.Background(), next, config, "test-geoblock")
	if err != nil {
		t.Fatalf("Failed to create plugin: %v", err)
	}

	geoBlock := handler.(*GeoBlock)

	testCases := []struct {
		ip       string
		expected bool
	}{
		{"192.168.1.1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"127.0.0.1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
	}

	for _, tc := range testCases {
		result := geoBlock.isPrivateIP(tc.ip)
		if result != tc.expected {
			t.Errorf("isPrivateIP(%s) = %v, expected %v", tc.ip, result, tc.expected)
		}
	}
}

func TestGetClientIP(t *testing.T) {
	config := CreateConfig()
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {})

	handler, err := New(context.Background(), next, config, "test-geoblock")
	if err != nil {
		t.Fatalf("Failed to create plugin: %v", err)
	}

	geoBlock := handler.(*GeoBlock)

	testCases := []struct {
		name     string
		remoteIP string
		xff      string
		xRealIP  string
		expected string
	}{
		{
			name:     "Direct IP",
			remoteIP: "1.2.3.4:1234",
			xff:      "",
			xRealIP:  "",
			expected: "1.2.3.4",
		},
		{
			name:     "X-Forwarded-For single IP",
			remoteIP: "192.168.1.1:1234",
			xff:      "5.6.7.8",
			xRealIP:  "",
			expected: "5.6.7.8",
		},
		{
			name:     "X-Forwarded-For multiple IPs",
			remoteIP: "192.168.1.1:1234",
			xff:      "5.6.7.8, 9.10.11.12",
			xRealIP:  "",
			expected: "5.6.7.8",
		},
		{
			name:     "X-Real-IP",
			remoteIP: "192.168.1.1:1234",
			xff:      "",
			xRealIP:  "13.14.15.16",
			expected: "13.14.15.16",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com", nil)
			req.RemoteAddr = tc.remoteIP
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.xRealIP != "" {
				req.Header.Set("X-Real-IP", tc.xRealIP)
			}

			result := geoBlock.getClientIP(req)
			if result != tc.expected {
				t.Errorf("Expected IP %s, got %s", tc.expected, result)
			}
		})
	}
}

func TestShouldBlock(t *testing.T) {
	testCases := []struct {
		name             string
		allowedCountries []string
		blockedCountries []string
		defaultAction    string
		country          string
		expected         bool
	}{
		{
			name:             "Allowlist - Country allowed",
			allowedCountries: []string{"US", "CA"},
			blockedCountries: []string{},
			defaultAction:    "allow",
			country:          "US",
			expected:         false,
		},
		{
			name:             "Allowlist - Country not allowed",
			allowedCountries: []string{"US", "CA"},
			blockedCountries: []string{},
			defaultAction:    "allow",
			country:          "CN",
			expected:         true,
		},
		{
			name:             "Blocklist - Country blocked",
			allowedCountries: []string{},
			blockedCountries: []string{"CN", "RU"},
			defaultAction:    "allow",
			country:          "CN",
			expected:         true,
		},
		{
			name:             "Blocklist - Country not blocked",
			allowedCountries: []string{},
			blockedCountries: []string{"CN", "RU"},
			defaultAction:    "allow",
			country:          "US",
			expected:         false,
		},
		{
			name:             "Default block",
			allowedCountries: []string{},
			blockedCountries: []string{},
			defaultAction:    "block",
			country:          "US",
			expected:         true,
		},
		{
			name:             "Default allow",
			allowedCountries: []string{},
			blockedCountries: []string{},
			defaultAction:    "allow",
			country:          "US",
			expected:         false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := CreateConfig()
			config.AllowedCountries = tc.allowedCountries
			config.BlockedCountries = tc.blockedCountries
			config.DefaultAction = tc.defaultAction

			next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {})
			handler, err := New(context.Background(), next, config, "test")
			if err != nil {
				t.Fatalf("Failed to create plugin: %v", err)
			}

			geoBlock := handler.(*GeoBlock)
			result := geoBlock.shouldBlock(tc.country)

			if result != tc.expected {
				t.Errorf("Expected shouldBlock to return %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestCache(t *testing.T) {
	cache := &geoCache{entries: make(map[string]*cacheEntry)}

	// Test setting and getting
	testInfo := &geoInfo{Country: "US", Organization: "Test Org"}
	cache.set("1.2.3.4", testInfo, 60*1000)
	info := cache.get("1.2.3.4")

	if info == nil {
		t.Fatal("Expected geoInfo, got nil")
	}

	if info.Country != "US" {
		t.Errorf("Expected country 'US', got '%s'", info.Country)
	}

	if info.Organization != "Test Org" {
		t.Errorf("Expected organization 'Test Org', got '%s'", info.Organization)
	}

	// Test non-existent entry
	info = cache.get("5.6.7.8")
	if info != nil {
		t.Errorf("Expected nil for non-existent entry, got '%v'", info)
	}
}

func TestServeHTTP(t *testing.T) {
	// This test would require mocking the GeoIP API
	// For now, we'll test that private IPs are allowed

	config := CreateConfig()
	config.BlockedCountries = []string{"CN"}

	nextCalled := false
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		nextCalled = true
		rw.WriteHeader(http.StatusOK)
	})

	handler, err := New(context.Background(), next, config, "test")
	if err != nil {
		t.Fatalf("Failed to create plugin: %v", err)
	}

	// Test with private IP (should always allow)
	req := httptest.NewRequest("GET", "http://example.com", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	rw := httptest.NewRecorder()

	handler.ServeHTTP(rw, req)

	if !nextCalled {
		t.Error("Next handler should have been called for private IP")
	}

	if rw.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rw.Code)
	}
}

package api

import (
	"net/http"
	"testing"
)

func newOriginRequest(origin string) *http.Request {
	req, _ := http.NewRequest("GET", "/ws", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	return req
}

func TestCheckOriginEmptyOrigin(t *testing.T) {
	u := createUpgrader([]string{"http://example.com"})
	if !u.CheckOrigin(newOriginRequest("")) {
		t.Error("empty Origin header should be allowed (non-browser clients)")
	}
}

func TestCheckOriginAllowed(t *testing.T) {
	u := createUpgrader([]string{"http://example.com"})
	if !u.CheckOrigin(newOriginRequest("http://example.com")) {
		t.Error("Origin in allowed list should be allowed")
	}
}

func TestCheckOriginDenied(t *testing.T) {
	u := createUpgrader([]string{"http://example.com"})
	if u.CheckOrigin(newOriginRequest("http://evil.com")) {
		t.Error("Origin not in allowed list should be denied")
	}
}

func TestCheckOriginEmptyWhitelist(t *testing.T) {
	u := createUpgrader([]string{})
	if u.CheckOrigin(newOriginRequest("http://example.com")) {
		t.Error("browser origin should be denied when whitelist is empty")
	}
}

func TestCheckOriginMultipleAllowed(t *testing.T) {
	u := createUpgrader([]string{"http://first.com", "http://second.com", "http://last.com"})
	if !u.CheckOrigin(newOriginRequest("http://first.com")) {
		t.Error("first entry in allowed list should match")
	}
	if !u.CheckOrigin(newOriginRequest("http://last.com")) {
		t.Error("last entry in allowed list should match")
	}
	if u.CheckOrigin(newOriginRequest("http://middle.com")) {
		t.Error("origin not in list should be denied")
	}
}

func TestCheckOriginEmptyWhitelistEmptyOrigin(t *testing.T) {
	u := createUpgrader([]string{})
	if !u.CheckOrigin(newOriginRequest("")) {
		t.Error("empty Origin with empty whitelist should be allowed (non-browser)")
	}
}

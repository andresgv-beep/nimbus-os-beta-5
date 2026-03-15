package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ═══════════════════════════════════
// App Reverse Proxy
// Routes: /app/{appId}/* → localhost:{port}/*
// Solves: HTTPS mixed content, X-Frame-Options, CORS
// ═══════════════════════════════════

func handleAppProxy(w http.ResponseWriter, r *http.Request) {
	// Parse /app/{appId}/...
	path := strings.TrimPrefix(r.URL.Path, "/app/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		jsonError(w, 404, "App not found")
		return
	}

	appId := parts[0]
	subPath := "/"
	if len(parts) > 1 {
		subPath = "/" + parts[1]
	}

	// Find port for this app
	port := getAppPort(appId)
	if port == 0 {
		jsonError(w, 404, fmt.Sprintf("App '%s' not found or has no port", appId))
		return
	}

	// Build target URL
	targetURL := fmt.Sprintf("http://127.0.0.1:%d%s", port, subPath)
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	// Create proxy request
	proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		jsonError(w, 500, "Proxy error")
		return
	}

	// Copy headers from original request
	for key, values := range r.Header {
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}
	// Override Host to match what the container expects
	proxyReq.Header.Set("Host", r.Host)

	// Execute request
	client := &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects, pass them through
		},
	}
	resp, err := client.Do(proxyReq)
	if err != nil {
		// App might not be running
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(502)
		fmt.Fprintf(w, `<html><body style="background:#1c1b3a;color:#fff;display:flex;align-items:center;justify-content:center;height:100vh;font-family:sans-serif"><div style="text-align:center"><h2>%s is not responding</h2><p style="color:#888">Port %d — the app may be starting up</p><button onclick="location.reload()" style="margin-top:16px;padding:8px 20px;border-radius:8px;border:none;background:#7c6fff;color:#fff;cursor:pointer">Retry</button></div></body></html>`, appId, port)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		// Remove headers that block iframe embedding
		lower := strings.ToLower(key)
		if lower == "x-frame-options" || lower == "content-security-policy" {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Set status code
	w.WriteHeader(resp.StatusCode)

	// Stream response body
	io.Copy(w, resp.Body)
}

// getAppPort looks up the port for an installed app
func getAppPort(appId string) int {
	// Check registered installed apps first
	apps := getInstalledApps()
	for _, app := range apps {
		id, _ := app["id"].(string)
		if id == appId {
			if p, ok := app["port"].(float64); ok && p > 0 {
				return int(p)
			}
			if p, ok := app["port"].(int); ok && p > 0 {
				return p
			}
		}
	}
	return 0
}

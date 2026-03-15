package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ═══════════════════════════════════
// HTTP API Server (runs alongside Unix socket)
// ═══════════════════════════════════

const httpPort = 5000

// JSON response helper
func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func jsonOk(w http.ResponseWriter, data interface{}) {
	jsonResponse(w, 200, data)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	jsonResponse(w, status, map[string]string{"error": msg})
}

// Read and parse JSON body (max 10MB)
func readBody(r *http.Request) (map[string]interface{}, error) {
	if r.Body == nil {
		return map[string]interface{}{}, nil
	}
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return map[string]interface{}{}, nil
	}
	var body map[string]interface{}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("invalid JSON: %v", err)
	}
	return body, nil
}

// Helper to get string from body map
func bodyStr(body map[string]interface{}, key string) string {
	if v, ok := body[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Helper to get bool from body map
func bodyBool(body map[string]interface{}, key string) (bool, bool) {
	if v, ok := body[key]; ok {
		if b, ok := v.(bool); ok {
			return b, true
		}
	}
	return false, false
}

// Extract Bearer token from Authorization header
func getBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return auth[7:]
	}
	return ""
}

// Authenticate request — returns session data or nil
func authenticate(r *http.Request) map[string]interface{} {
	token := getBearerToken(r)
	if token == "" {
		return nil
	}
	hashed := sha256Hex(token)
	session, err := dbSessionGet(hashed)
	if err != nil {
		return nil
	}
	return session
}

// Require authentication middleware helper — returns session or sends 401
func requireAuth(w http.ResponseWriter, r *http.Request) map[string]interface{} {
	session := authenticate(r)
	if session == nil {
		jsonError(w, 401, "Not authenticated")
		return nil
	}
	return session
}

// Require admin role — returns session or sends error
func requireAdmin(w http.ResponseWriter, r *http.Request) map[string]interface{} {
	session := requireAuth(w, r)
	if session == nil {
		return nil
	}
	if role, _ := session["role"].(string); role != "admin" {
		jsonError(w, 403, "Unauthorized")
		return nil
	}
	return session
}

// Get client IP
func clientIP(r *http.Request) string {
	// Check X-Forwarded-For (behind proxy)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// RemoteAddr is host:port
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}

// CORS preflight handler
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Start HTTP API server (non-blocking)
func startHTTPServer() {
	mux := http.NewServeMux()

	// ── Auth routes ──
	mux.HandleFunc("/api/auth/", handleAuthRoutes)
	mux.HandleFunc("/api/user/", handleUserRoutes)
	mux.HandleFunc("/api/users", handleUsersRoutes)
	mux.HandleFunc("/api/users/", handleUsersRoutes)

	// ── Shares routes ──
	mux.HandleFunc("/api/shares", handleSharesRoutes)
	mux.HandleFunc("/api/shares/", handleSharesRoutes)

	// ── Native Apps routes ──
	mux.HandleFunc("/api/native-apps", handleNativeAppsRoutes)
	mux.HandleFunc("/api/native-apps/", handleNativeAppsRoutes)

	// ── Installed Apps routes ──
	mux.HandleFunc("/api/installed-apps", handleInstalledAppsRoutes)
	mux.HandleFunc("/api/installed-apps/", handleInstalledAppsRoutes)

	// ── Hardware / System monitoring routes ──
	mux.HandleFunc("/api/system", handleHardwareRoutes)
	mux.HandleFunc("/api/system/", handleHardwareRoutes)
	mux.HandleFunc("/api/cpu", handleHardwareRoutes)
	mux.HandleFunc("/api/memory", handleHardwareRoutes)
	mux.HandleFunc("/api/gpu", handleHardwareRoutes)
	mux.HandleFunc("/api/temps", handleHardwareRoutes)
	mux.HandleFunc("/api/network", handleHardwareRoutes)
	mux.HandleFunc("/api/disks", handleHardwareRoutes)
	mux.HandleFunc("/api/uptime", handleHardwareRoutes)
	mux.HandleFunc("/api/containers", handleHardwareRoutes)
	mux.HandleFunc("/api/containers/", handleContainerAction)
	mux.HandleFunc("/api/hostname", handleHardwareRoutes)
	mux.HandleFunc("/api/hardware/", handleHardwareRoutes)

	// ── System power + update + terminal ──
	mux.HandleFunc("/api/system/reboot-service", handleHardwareRoutes)
	mux.HandleFunc("/api/system/reboot", handleHardwareRoutes)
	mux.HandleFunc("/api/system/shutdown", handleHardwareRoutes)
	mux.HandleFunc("/api/system/update/", handleHardwareRoutes)
	mux.HandleFunc("/api/terminal", handleHardwareRoutes)

	// ── Files routes ──
	mux.HandleFunc("/api/files", handleFilesRoutes)
	mux.HandleFunc("/api/files/", handleFilesRoutes)

	// ── Storage routes ──
	mux.HandleFunc("/api/storage", handleStorageRoutes)
	mux.HandleFunc("/api/storage/", handleStorageRoutes)

	// ── Docker routes ──
	mux.HandleFunc("/api/docker/", handleDockerRoutes)
	mux.HandleFunc("/api/docker", handleDockerRoutes)
	mux.HandleFunc("/api/permissions/", handleDockerRoutes)
	mux.HandleFunc("/api/firewall/add-rule", handleDockerRoutes)
	mux.HandleFunc("/api/firewall/remove-rule", handleDockerRoutes)
	mux.HandleFunc("/api/firewall/toggle", handleDockerRoutes)

	// ── Network + VMs routes ──
	registerNetworkRoutes(mux)

	// ── Torrent proxy to NimTorrent ──
	mux.HandleFunc("/api/torrent/", handleTorrentProxy)
	mux.HandleFunc("/api/torrent", handleTorrentProxy)

	// ── App reverse proxy (Docker apps via /app/{id}/) ──
	mux.HandleFunc("/app/", handleAppProxy)

	// ── Static file serving (frontend) — must be last ──
	mux.HandleFunc("/", serveStatic)

	server := &http.Server{
		Addr:         fmt.Sprintf("0.0.0.0:%d", httpPort),
		Handler:      corsMiddleware(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
	}

	go func() {
		logMsg("HTTP server listening on 0.0.0.0:%d", httpPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logMsg("HTTP server error: %v", err)
		}
	}()
}

// Container action handler: POST /api/containers/:id/:action
func handleContainerAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonError(w, 405, "Method not allowed")
		return
	}
	session := requireAuth(w, r)
	if session == nil {
		return
	}

	// Parse /api/containers/:id/:action
	re := regexp.MustCompile(`^/api/containers/([a-zA-Z0-9_.-]+)/(start|stop|restart|pause|unpause)$`)
	matches := re.FindStringSubmatch(r.URL.Path)
	if matches == nil {
		jsonError(w, 404, "Not found")
		return
	}

	result := containerAction(matches[1], matches[2])
	if errMsg, ok := result["error"].(string); ok && errMsg != "" {
		jsonError(w, 400, errMsg)
		return
	}
	jsonOk(w, result)
}

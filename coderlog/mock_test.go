package coderlog_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/google/uuid"
)

// validLogLevels is the set of levels accepted by the Coder API.
var validLogLevels = map[string]bool{
	"trace": true,
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

// mockLogSource mirrors the response from POST /log-source.
type mockLogSource struct {
	WorkspaceAgentID uuid.UUID `json:"workspace_agent_id"`
	ID               uuid.UUID `json:"id"`
	CreatedAt        time.Time `json:"created_at"`
	DisplayName      string    `json:"display_name"`
	Icon             string    `json:"icon"`
}

// mockPatchLogsRequest mirrors the PATCH /logs request body.
type mockPatchLogsRequest struct {
	LogSourceID uuid.UUID `json:"log_source_id"`
	Logs        []struct {
		CreatedAt time.Time `json:"created_at"`
		Output    string    `json:"output"`
		Level     string    `json:"level"`
	} `json:"logs"`
}

// mockPostLogSourceRequest mirrors the POST /log-source request body.
type mockPostLogSourceRequest struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	Icon        string    `json:"icon"`
}

// mockServer simulates the Coder Agent API endpoints.
type mockServer struct {
	mu             sync.Mutex
	sources        map[uuid.UUID]mockLogSource
	logs           []mockReceivedLog
	totalLogBytes  int
	maxLogBytes    int // default 1 MiB
	overflowed     bool
	agentID        uuid.UUID
	expectedToken  string

	server *httptest.Server
}

// mockReceivedLog is a log entry captured by the mock.
type mockReceivedLog struct {
	SourceID  uuid.UUID
	CreatedAt time.Time
	Output    string
	Level     string
}

// newMockServer creates a mock Coder Agent API server.
// If token is empty, authentication is not checked.
func newMockServer(token string) *mockServer {
	m := &mockServer{
		sources:       make(map[uuid.UUID]mockLogSource),
		maxLogBytes:   1 << 20, // 1 MiB
		agentID:       uuid.New(),
		expectedToken: token,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v2/workspaceagents/me/log-source", m.handlePostLogSource)
	mux.HandleFunc("PATCH /api/v2/workspaceagents/me/logs", m.handlePatchLogs)
	m.server = httptest.NewServer(mux)
	return m
}

func (m *mockServer) URL() string {
	return m.server.URL
}

func (m *mockServer) Close() {
	m.server.Close()
}

// getSources returns a copy of registered sources.
func (m *mockServer) getSources() map[uuid.UUID]mockLogSource {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[uuid.UUID]mockLogSource, len(m.sources))
	for k, v := range m.sources {
		out[k] = v
	}
	return out
}

// getLogs returns a copy of received logs.
func (m *mockServer) getLogs() []mockReceivedLog {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]mockReceivedLog, len(m.logs))
	copy(out, m.logs)
	return out
}

func (m *mockServer) checkAuth(w http.ResponseWriter, r *http.Request) bool {
	if m.expectedToken == "" {
		return true
	}
	if r.Header.Get("Coder-Session-Token") != m.expectedToken {
		http.Error(w, `{"message":"Unauthorized"}`, http.StatusUnauthorized)
		return false
	}
	return true
}

func (m *mockServer) handlePostLogSource(w http.ResponseWriter, r *http.Request) {
	if !m.checkAuth(w, r) {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"message":"Bad request"}`, http.StatusBadRequest)
		return
	}

	var req mockPostLogSourceRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, `{"message":"Invalid JSON"}`, http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Idempotent: re-registration of the same ID returns 201 with current data.
	src := mockLogSource{
		WorkspaceAgentID: m.agentID,
		ID:               req.ID,
		CreatedAt:        time.Now().UTC(),
		DisplayName:      req.DisplayName,
		Icon:             req.Icon,
	}
	m.sources[req.ID] = src

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(src)
}

func (m *mockServer) handlePatchLogs(w http.ResponseWriter, r *http.Request) {
	if !m.checkAuth(w, r) {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"message":"Bad request"}`, http.StatusBadRequest)
		return
	}

	var req mockPatchLogsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, `{"message":"Invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if len(req.Logs) == 0 {
		http.Error(w, `{"message":"No logs provided."}`, http.StatusBadRequest)
		return
	}

	// Validate log levels.
	for _, l := range req.Logs {
		lvl := l.Level
		if lvl == "" {
			continue // defaults to info
		}
		if !validLogLevels[lvl] {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"message": "Invalid log level provided.",
				"detail":  "invalid log level: \"" + lvl + "\"",
			})
			return
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already overflowed.
	if m.overflowed {
		http.Error(w, `{"message":"Logs limit exceeded"}`, http.StatusRequestEntityTooLarge)
		return
	}

	// Calculate output length and check overflow.
	outputLen := 0
	for _, l := range req.Logs {
		outputLen += len(l.Output)
	}

	if m.totalLogBytes+outputLen > m.maxLogBytes {
		m.overflowed = true
		http.Error(w, `{"message":"Logs limit exceeded"}`, http.StatusRequestEntityTooLarge)
		return
	}

	m.totalLogBytes += outputLen

	// Store logs.
	for _, l := range req.Logs {
		m.logs = append(m.logs, mockReceivedLog{
			SourceID:  req.LogSourceID,
			CreatedAt: l.CreatedAt,
			Output:    l.Output,
			Level:     l.Level,
		})
	}

	w.WriteHeader(http.StatusOK)
}

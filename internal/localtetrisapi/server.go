package localtetrisapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	roomID       = "local_room"
	controlHuman = "human"
	controlAgent = "agent"
	sourceHuman  = "human"
	sourceAgent  = "agent"
)

type player struct {
	ID          string    `json:"id"`
	AppID       string    `json:"app_id"`
	Version     string    `json:"version"`
	PingMS      int       `json:"ping_ms"`
	Ready       bool      `json:"ready"`
	RoomID      string    `json:"room_id,omitempty"`
	ControlMode string    `json:"control_mode"`
	AgentID     string    `json:"agent_id,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type room struct {
	ID        string    `json:"id"`
	AppID     string    `json:"app_id"`
	Version   string    `json:"version"`
	HostID    string    `json:"host_id"`
	PlayerIDs []string  `json:"player_ids"`
	CreatedAt time.Time `json:"created_at"`
}

type state struct {
	PlayerID  string    `json:"player_id"`
	Source    string    `json:"source"`
	Board     []string  `json:"board,omitempty"`
	Score     int       `json:"score,omitempty"`
	Lines     int       `json:"lines,omitempty"`
	Level     int       `json:"level,omitempty"`
	GameOver  bool      `json:"game_over,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type inputEvent struct {
	PlayerID string         `json:"player_id"`
	Source   string         `json:"source"`
	Action   string         `json:"action"`
	Payload  map[string]any `json:"payload,omitempty"`
	Tick     int64          `json:"tick,omitempty"`
	At       time.Time      `json:"at"`
}

type event struct {
	Type   string         `json:"type"`
	RoomID string         `json:"room_id,omitempty"`
	Player *player        `json:"player,omitempty"`
	Room   *room          `json:"room,omitempty"`
	Input  *inputEvent    `json:"input,omitempty"`
	Meta   map[string]any `json:"meta,omitempty"`
	At     time.Time      `json:"at"`
}

type localStore struct {
	mu     sync.RWMutex
	player *player
	room   room
	states map[string]state
	subs   map[chan []byte]struct{}
}

func newLocalStore() *localStore {
	return &localStore{
		room: room{
			ID:        roomID,
			AppID:     "tetris-local",
			Version:   "0.1.0",
			PlayerIDs: []string{},
			CreatedAt: time.Now().UTC(),
		},
		states: map[string]state{},
		subs:   map[chan []byte]struct{}{},
	}
}

type Server struct {
	store *localStore
}

func NewServer() *Server {
	return &Server{store: newLocalStore()}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/local-tetris/register", s.handleRegister)
	mux.HandleFunc("/api/local-tetris/ready", s.handleReady)
	mux.HandleFunc("/api/local-tetris/player/", s.handlePlayer)
	mux.HandleFunc("/api/local-tetris/room/", s.handleRoom)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		writeNoContent(w)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		PlayerID string `json:"player_id"`
		AppID    string `json:"app_id"`
		Version  string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(req.PlayerID) == "" {
		writeError(w, http.StatusBadRequest, "player_id required")
		return
	}
	if req.AppID == "" {
		req.AppID = "tetris-local"
	}
	if req.Version == "" {
		req.Version = "0.1.0"
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	// Single-seat local harness: re-register replaces current seat safely.
	s.store.player = &player{
		ID:          req.PlayerID,
		AppID:       req.AppID,
		Version:     req.Version,
		Ready:       false,
		RoomID:      roomID,
		ControlMode: controlHuman,
		UpdatedAt:   time.Now().UTC(),
	}
	s.store.room.HostID = req.PlayerID
	s.store.room.PlayerIDs = []string{req.PlayerID}
	s.store.states = map[string]state{}
	cp := *s.store.player
	writeJSON(w, http.StatusOK, map[string]any{"player": cp})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		writeNoContent(w)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		PlayerID string `json:"player_id"`
		PingMS   int    `json:"ping_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if s.store.player == nil || s.store.player.ID != req.PlayerID {
		writeError(w, http.StatusBadRequest, "player not found")
		return
	}
	s.store.player.Ready = true
	s.store.player.PingMS = req.PingMS
	s.store.player.RoomID = roomID
	s.store.player.UpdatedAt = time.Now().UTC()
	cp := s.store.room
	cp.PlayerIDs = append([]string(nil), s.store.room.PlayerIDs...)
	writeJSON(w, http.StatusOK, map[string]any{"matched": true, "room": cp})
}

func (s *Server) handlePlayer(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/local-tetris/player/"), "/")
	if id == "" {
		writeError(w, http.StatusNotFound, "player id missing")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	if s.store.player == nil || s.store.player.ID != id {
		writeError(w, http.StatusBadRequest, "player not found")
		return
	}
	cp := *s.store.player
	writeJSON(w, http.StatusOK, map[string]any{"player": cp})
}

func (s *Server) handleRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		writeNoContent(w)
		return
	}
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/local-tetris/room/"), "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "room id missing")
		return
	}
	if parts[0] != roomID {
		writeError(w, http.StatusBadRequest, "unknown room")
		return
	}
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	switch {
	case action == "" && r.Method == http.MethodGet:
		s.store.mu.RLock()
		cp := s.store.room
		cp.PlayerIDs = append([]string(nil), s.store.room.PlayerIDs...)
		s.store.mu.RUnlock()
		writeJSON(w, http.StatusOK, map[string]any{"room": cp})
	case action == "state" && r.Method == http.MethodGet:
		s.store.mu.RLock()
		cp := s.store.room
		cp.PlayerIDs = append([]string(nil), s.store.room.PlayerIDs...)
		states := make(map[string]state, len(s.store.states))
		for k, v := range s.store.states {
			x := v
			x.Board = append([]string(nil), v.Board...)
			states[k] = x
		}
		s.store.mu.RUnlock()
		writeJSON(w, http.StatusOK, map[string]any{"room": cp, "states": states})
	case action == "stream" && r.Method == http.MethodGet:
		s.handleRoomStream(w, r)
	case action == "control" && r.Method == http.MethodPost:
		s.handleControl(w, r)
	case action == "input" && r.Method == http.MethodPost:
		s.handleInput(w, r)
	default:
		writeError(w, http.StatusNotFound, "route not found")
	}
}

func (s *Server) handleControl(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlayerID string `json:"player_id"`
		ToMode   string `json:"to_mode"`
		AgentID  string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.ToMode != controlHuman && req.ToMode != controlAgent {
		writeError(w, http.StatusBadRequest, "invalid control mode")
		return
	}
	s.store.mu.Lock()
	if s.store.player == nil || s.store.player.ID != req.PlayerID {
		s.store.mu.Unlock()
		writeError(w, http.StatusBadRequest, "player not found")
		return
	}
	s.store.player.ControlMode = req.ToMode
	s.store.player.AgentID = req.AgentID
	s.store.player.UpdatedAt = time.Now().UTC()
	cp := *s.store.player
	s.store.mu.Unlock()
	s.publish(event{
		Type:   "control_switch_applied",
		RoomID: roomID,
		Player: &cp,
		Room:   &room{ID: roomID, HostID: cp.ID, PlayerIDs: []string{cp.ID}},
		Meta:   map[string]any{"player_id": cp.ID, "to_mode": cp.ControlMode},
		At:     time.Now().UTC(),
	})
	writeJSON(w, http.StatusOK, map[string]any{"player": cp})
}

func (s *Server) handleInput(w http.ResponseWriter, r *http.Request) {
	var req inputEvent
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	s.store.mu.Lock()
	if s.store.player == nil || s.store.player.ID != req.PlayerID {
		s.store.mu.Unlock()
		writeError(w, http.StatusBadRequest, "player not found")
		return
	}
	if s.store.player.ControlMode == controlHuman && req.Source != sourceHuman {
		s.store.mu.Unlock()
		writeError(w, http.StatusBadRequest, "input source does not match control mode")
		return
	}
	if s.store.player.ControlMode == controlAgent && req.Source != sourceAgent {
		s.store.mu.Unlock()
		writeError(w, http.StatusBadRequest, "input source does not match control mode")
		return
	}
	if req.At.IsZero() {
		req.At = time.Now().UTC()
	}
	if req.Action == "state_sync" {
		st := state{
			PlayerID:  req.PlayerID,
			Source:    req.Source,
			Board:     toStringSlice(req.Payload["board"]),
			Score:     toInt(req.Payload["score"]),
			Lines:     toInt(req.Payload["lines"]),
			Level:     toInt(req.Payload["level"]),
			GameOver:  toBool(req.Payload["game_over"]),
			UpdatedAt: time.Now().UTC(),
		}
		s.store.states[req.PlayerID] = st
	}
	s.store.mu.Unlock()

	s.publish(event{
		Type:   "room_input",
		RoomID: roomID,
		Input:  &req,
		Room:   &room{ID: roomID, HostID: req.PlayerID, PlayerIDs: []string{req.PlayerID}},
		At:     req.At,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRoomStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	ch := make(chan []byte, 32)
	s.store.mu.Lock()
	s.store.subs[ch] = struct{}{}
	s.store.mu.Unlock()
	defer func() {
		s.store.mu.Lock()
		delete(s.store.subs, ch)
		close(ch)
		s.store.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			if _, err := w.Write([]byte("event: room\ndata: " + string(msg) + "\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) publish(evt event) {
	b, _ := json.Marshal(evt)
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	for ch := range s.store.subs {
		select {
		case ch <- b:
		default:
		}
	}
}

func toStringSlice(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		if s, ok := v.([]string); ok {
			return append([]string(nil), s...)
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		s, ok := x.(string)
		if ok {
			out = append(out, s)
		}
	}
	return out
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

func toBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func writeNoContent(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
	w.WriteHeader(http.StatusNoContent)
}

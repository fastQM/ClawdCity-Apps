package tetrisapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"Assembler-Apps/internal/tetrisroom"
)

type Server struct {
	tetris *tetrisroom.Manager
}

func NewServer(t *tetrisroom.Manager) *Server {
	return &Server{tetris: t}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/tetris/register", s.handleRegister)
	mux.HandleFunc("/api/tetris/ready", s.handleReady)
	mux.HandleFunc("/api/tetris/player/", s.handlePlayer)
	mux.HandleFunc("/api/tetris/room/", s.handleRoom)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if s.tetris == nil {
		writeError(w, http.StatusServiceUnavailable, "tetris room service unavailable")
		return
	}
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
	if req.PlayerID == "" {
		writeError(w, http.StatusBadRequest, "player_id required")
		return
	}
	if req.AppID == "" {
		req.AppID = "tetris"
	}
	if req.Version == "" {
		req.Version = "0.1.0"
	}
	player, err := s.tetris.UpsertPlayer(req.PlayerID, req.AppID, req.Version)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"player": player})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if s.tetris == nil {
		writeError(w, http.StatusServiceUnavailable, "tetris room service unavailable")
		return
	}
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
	room, err := s.tetris.SetReady(req.PlayerID, req.PingMS)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if room == nil {
		writeJSON(w, http.StatusOK, map[string]any{"matched": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"matched": true, "room": room})
}

func (s *Server) handlePlayer(w http.ResponseWriter, r *http.Request) {
	if s.tetris == nil {
		writeError(w, http.StatusServiceUnavailable, "tetris room service unavailable")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/tetris/player/")
	id = strings.Trim(id, "/")
	if id == "" {
		writeError(w, http.StatusNotFound, "player id missing")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	p, err := s.tetris.GetPlayer(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"player": p})
}

func (s *Server) handleRoom(w http.ResponseWriter, r *http.Request) {
	if s.tetris == nil {
		writeError(w, http.StatusServiceUnavailable, "tetris room service unavailable")
		return
	}
	if r.Method == http.MethodOptions {
		writeNoContent(w)
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/tetris/room/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "room id missing")
		return
	}
	roomID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		room, err := s.tetris.GetRoom(roomID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"room": room})
	case action == "state" && r.Method == http.MethodGet:
		room, err := s.tetris.GetRoom(roomID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		states, err := s.tetris.GetRoomStates(roomID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"room": room, "states": states})
	case action == "stream" && r.Method == http.MethodGet:
		s.handleRoomStream(w, r, roomID)
	case action == "control" && r.Method == http.MethodPost:
		var req struct {
			PlayerID string `json:"player_id"`
			ToMode   string `json:"to_mode"`
			AgentID  string `json:"agent_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		p, err := s.tetris.ToggleControl(roomID, req.PlayerID, req.ToMode, req.AgentID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"player": p})
	case action == "input" && r.Method == http.MethodPost:
		var req struct {
			PlayerID string         `json:"player_id"`
			Source   string         `json:"source"`
			Action   string         `json:"action"`
			Payload  map[string]any `json:"payload"`
			Tick     int64          `json:"tick"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		err := s.tetris.SubmitInput(roomID, tetrisroom.InputEvent{
			PlayerID: req.PlayerID,
			Source:   req.Source,
			Action:   req.Action,
			Payload:  req.Payload,
			Tick:     req.Tick,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeError(w, http.StatusNotFound, "route not found")
	}
}

func (s *Server) handleRoomStream(w http.ResponseWriter, r *http.Request, roomID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	ch, cancel, err := s.tetris.SubscribeRoom(roomID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer cancel()

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
			if _, err := w.Write([]byte("event: room\ndata: " + string(msg.Payload) + "\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
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

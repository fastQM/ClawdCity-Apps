package tetrisroom

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"Assembler-Apps/internal/core/network"
)

const (
	ControlHuman = "human"
	ControlAgent = "agent"

	SourceHuman = "human"
	SourceAgent = "agent"
)

var (
	ErrPlayerExists         = errors.New("player already exists")
	ErrLocalSeatOccupied    = errors.New("local seat already occupied by another player")
	ErrPlayerNotFound       = errors.New("player not found")
	ErrRoomNotFound         = errors.New("room not found")
	ErrAlreadyInRoom        = errors.New("player already in room")
	ErrInvalidControlMode   = errors.New("invalid control mode")
	ErrControlModeMismatch  = errors.New("input source does not match control mode")
	ErrPlayerNotInRoom      = errors.New("player not in room")
	ErrPlayerNotRoomMember  = errors.New("player is not room member")
	ErrPingRequiredForReady = errors.New("ping_ms required and must be >= 0")
)

type Player struct {
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

type Room struct {
	ID        string    `json:"id"`
	AppID     string    `json:"app_id"`
	Version   string    `json:"version"`
	HostID    string    `json:"host_id"`
	PlayerIDs []string  `json:"player_ids"`
	CreatedAt time.Time `json:"created_at"`
}

type PlayerState struct {
	PlayerID  string    `json:"player_id"`
	Source    string    `json:"source"`
	Board     []string  `json:"board,omitempty"`
	Score     int       `json:"score,omitempty"`
	Lines     int       `json:"lines,omitempty"`
	Level     int       `json:"level,omitempty"`
	GameOver  bool      `json:"game_over,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type InputEvent struct {
	PlayerID string         `json:"player_id"`
	Source   string         `json:"source"`
	Action   string         `json:"action"`
	Payload  map[string]any `json:"payload,omitempty"`
	Tick     int64          `json:"tick,omitempty"`
	At       time.Time      `json:"at"`
}

type Event struct {
	Type   string         `json:"type"`
	RoomID string         `json:"room_id,omitempty"`
	Player *Player        `json:"player,omitempty"`
	Room   *Room          `json:"room,omitempty"`
	Input  *InputEvent    `json:"input,omitempty"`
	Meta   map[string]any `json:"meta,omitempty"`
	At     time.Time      `json:"at"`
}

// Manager manages matchmaking and room lifecycle.
type Manager struct {
	mu      sync.RWMutex
	pubsub  network.PubSub
	players map[string]*Player
	remote  map[string]*Player
	rooms   map[string]*Room
	states  map[string]map[string]PlayerState
	seq     atomic.Int64
}

func NewManager(pubsub network.PubSub) *Manager {
	m := &Manager{
		pubsub:  pubsub,
		players: make(map[string]*Player),
		remote:  make(map[string]*Player),
		rooms:   make(map[string]*Room),
		states:  make(map[string]map[string]PlayerState),
	}
	m.startSync()
	return m
}

func (m *Manager) RegisterPlayer(id, appID, version string) (*Player, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.players[id]; ok {
		return nil, ErrPlayerExists
	}
	if len(m.players) > 0 {
		return nil, ErrLocalSeatOccupied
	}
	p := &Player{
		ID:          id,
		AppID:       appID,
		Version:     version,
		ControlMode: ControlHuman,
		UpdatedAt:   time.Now().UTC(),
	}
	m.players[id] = p
	cp := *p
	return &cp, nil
}

func (m *Manager) UpsertPlayer(id, appID, version string) (*Player, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.players[id]
	if !ok {
		if len(m.players) > 0 {
			return nil, ErrLocalSeatOccupied
		}
		p = &Player{ID: id, ControlMode: ControlHuman}
		m.players[id] = p
	}
	if p.RoomID != "" && (appID != "" && appID != p.AppID || version != "" && version != p.Version) {
		return nil, ErrAlreadyInRoom
	}
	if appID != "" {
		p.AppID = appID
	}
	if version != "" {
		p.Version = version
	}
	if p.ControlMode == "" {
		p.ControlMode = ControlHuman
	}
	p.UpdatedAt = time.Now().UTC()
	cp := *p
	return &cp, nil
}

func (m *Manager) SetReady(playerID string, pingMS int) (*Room, error) {
	if pingMS < 0 {
		return nil, ErrPingRequiredForReady
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.players[playerID]
	if !ok {
		return nil, ErrPlayerNotFound
	}
	if p.RoomID != "" {
		return nil, ErrAlreadyInRoom
	}
	p.Ready = true
	p.PingMS = pingMS
	p.UpdatedAt = time.Now().UTC()

	m.publishPlayerLocked("player_ready", p)
	room := m.tryMatchLocked(p.AppID, p.Version)
	if room == nil {
		return nil, nil
	}
	cp := *room
	return &cp, nil
}

func (m *Manager) tryMatchLocked(appID, version string) *Room {
	type candidate struct {
		p     *Player
		local bool
	}
	candidatesByID := make(map[string]candidate)
	for _, p := range m.players {
		if p.Ready && p.RoomID == "" && p.AppID == appID && p.Version == version {
			candidatesByID[p.ID] = candidate{p: p, local: true}
		}
	}
	for _, p := range m.remote {
		if p.Ready && p.RoomID == "" && p.AppID == appID && p.Version == version {
			if _, exists := candidatesByID[p.ID]; !exists {
				cp := *p
				candidatesByID[p.ID] = candidate{p: &cp, local: false}
			}
		}
	}
	candidates := make([]candidate, 0, len(candidatesByID))
	for _, c := range candidatesByID {
		candidates = append(candidates, c)
	}
	if len(candidates) < 2 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].p.PingMS == candidates[j].p.PingMS {
			return candidates[i].p.ID < candidates[j].p.ID
		}
		return candidates[i].p.PingMS < candidates[j].p.PingMS
	})
	members := candidates[:2]
	host := members[0].p

	owner := members[0].p.ID
	if members[1].p.ID < owner {
		owner = members[1].p.ID
	}
	if _, local := m.players[owner]; !local {
		return nil
	}

	roomID := fmt.Sprintf("room_%d", m.seq.Add(1))
	room := &Room{
		ID:        roomID,
		AppID:     appID,
		Version:   version,
		HostID:    host.ID,
		PlayerIDs: []string{members[0].p.ID, members[1].p.ID},
		CreatedAt: time.Now().UTC(),
	}
	m.rooms[roomID] = room
	for _, member := range members {
		if member.local {
			lp := m.players[member.p.ID]
			lp.RoomID = roomID
			lp.Ready = false
			lp.ControlMode = ControlHuman
			lp.AgentID = ""
			lp.UpdatedAt = time.Now().UTC()
		}
		delete(m.remote, member.p.ID)
	}

	m.publishRoomLocked("room_assigned", room, map[string]any{"reason": "all_ready", "host_ping_ms": host.PingMS})
	return room
}

func (m *Manager) GetPlayer(playerID string) (*Player, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.players[playerID]
	if !ok {
		return nil, ErrPlayerNotFound
	}
	cp := *p
	return &cp, nil
}

func (m *Manager) GetRoom(roomID string) (*Room, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return nil, ErrRoomNotFound
	}
	cp := *r
	cp.PlayerIDs = append([]string(nil), r.PlayerIDs...)
	return &cp, nil
}

func (m *Manager) ToggleControl(roomID, playerID, toMode, agentID string) (*Player, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if toMode != ControlHuman && toMode != ControlAgent {
		return nil, ErrInvalidControlMode
	}
	r, ok := m.rooms[roomID]
	if !ok {
		return nil, ErrRoomNotFound
	}
	if !contains(r.PlayerIDs, playerID) {
		return nil, ErrPlayerNotRoomMember
	}
	p, ok := m.players[playerID]
	if !ok {
		return nil, ErrPlayerNotFound
	}
	if p.RoomID != roomID {
		return nil, ErrPlayerNotInRoom
	}
	from := p.ControlMode
	p.ControlMode = toMode
	if toMode == ControlAgent {
		p.AgentID = agentID
	} else {
		p.AgentID = ""
	}
	p.UpdatedAt = time.Now().UTC()

	m.publishRoomLocked("control_switch_applied", r, map[string]any{
		"player_id": playerID,
		"from_mode": from,
		"to_mode":   toMode,
		"agent_id":  p.AgentID,
	})

	cp := *p
	return &cp, nil
}

func (m *Manager) SubmitInput(roomID string, in InputEvent) error {
	m.mu.RLock()
	r, ok := m.rooms[roomID]
	if !ok {
		m.mu.RUnlock()
		return ErrRoomNotFound
	}
	if !contains(r.PlayerIDs, in.PlayerID) {
		m.mu.RUnlock()
		return ErrPlayerNotRoomMember
	}
	p, ok := m.players[in.PlayerID]
	if !ok {
		m.mu.RUnlock()
		return ErrPlayerNotFound
	}
	if p.RoomID != roomID {
		m.mu.RUnlock()
		return ErrPlayerNotInRoom
	}
	if p.ControlMode == ControlHuman && in.Source != SourceHuman {
		m.mu.RUnlock()
		return ErrControlModeMismatch
	}
	if p.ControlMode == ControlAgent && in.Source != SourceAgent {
		m.mu.RUnlock()
		return ErrControlModeMismatch
	}
	m.mu.RUnlock()

	if in.At.IsZero() {
		in.At = time.Now().UTC()
	}
	if in.Action == "state_sync" {
		m.upsertRoomState(roomID, in)
	}
	b, _ := json.Marshal(Event{Type: "room_input", RoomID: roomID, Input: &in, At: in.At})
	if err := m.pubsub.Publish(topicForRoom(roomID), b); err != nil {
		return err
	}
	return m.pubsub.Publish("tetris.room", b)
}

func (m *Manager) SubscribeRoom(roomID string) (<-chan network.Message, func(), error) {
	return m.pubsub.Subscribe(topicForRoom(roomID))
}

func (m *Manager) GetRoomStates(roomID string) (map[string]PlayerState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.rooms[roomID]; !ok {
		return nil, ErrRoomNotFound
	}
	stateByPlayer, ok := m.states[roomID]
	if !ok {
		return map[string]PlayerState{}, nil
	}
	out := make(map[string]PlayerState, len(stateByPlayer))
	for k, v := range stateByPlayer {
		cp := v
		cp.Board = append([]string(nil), v.Board...)
		out[k] = cp
	}
	return out, nil
}

func (m *Manager) publishPlayerLocked(eventType string, p *Player) {
	cp := *p
	b, _ := json.Marshal(Event{Type: eventType, Player: &cp, At: time.Now().UTC()})
	_ = m.pubsub.Publish("tetris.player", b)
}

func (m *Manager) publishRoomLocked(eventType string, r *Room, meta map[string]any) {
	cp := *r
	cp.PlayerIDs = append([]string(nil), r.PlayerIDs...)
	evt := Event{Type: eventType, RoomID: r.ID, Room: &cp, Meta: meta, At: time.Now().UTC()}
	b, _ := json.Marshal(evt)
	_ = m.pubsub.Publish(topicForRoom(r.ID), b)
	_ = m.pubsub.Publish("tetris.room", b)
}

func (m *Manager) startSync() {
	playerCh, _, err := m.pubsub.Subscribe("tetris.player")
	if err == nil {
		go m.consumePlayerEvents(playerCh)
	}
	roomCh, _, err := m.pubsub.Subscribe("tetris.room")
	if err == nil {
		go m.consumeRoomEvents(roomCh)
	}
}

func (m *Manager) consumePlayerEvents(ch <-chan network.Message) {
	for msg := range ch {
		var evt Event
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			continue
		}
		if evt.Type != "player_ready" || evt.Player == nil {
			continue
		}
		m.mu.Lock()
		incoming := evt.Player
		if local, ok := m.players[incoming.ID]; ok {
			// Ensure local state stays fresh even when consuming self-published events.
			if local.RoomID == "" {
				local.Ready = incoming.Ready
				local.PingMS = incoming.PingMS
				local.AppID = incoming.AppID
				local.Version = incoming.Version
				local.UpdatedAt = time.Now().UTC()
			}
		} else {
			cp := *incoming
			if cp.RoomID == "" && cp.Ready {
				m.remote[cp.ID] = &cp
			} else {
				delete(m.remote, cp.ID)
			}
		}
		m.tryMatchLocked(incoming.AppID, incoming.Version)
		m.mu.Unlock()
	}
}

func (m *Manager) consumeRoomEvents(ch <-chan network.Message) {
	for msg := range ch {
		var evt Event
		if err := json.Unmarshal(msg.Payload, &evt); err != nil {
			continue
		}
		switch evt.Type {
		case "room_assigned":
			if evt.Room == nil {
				continue
			}
			m.mu.Lock()
			cp := *evt.Room
			cp.PlayerIDs = append([]string(nil), evt.Room.PlayerIDs...)
			m.rooms[cp.ID] = &cp
			for _, pid := range cp.PlayerIDs {
				delete(m.remote, pid)
				if p, ok := m.players[pid]; ok {
					p.RoomID = cp.ID
					p.Ready = false
					p.ControlMode = ControlHuman
					p.AgentID = ""
					p.UpdatedAt = time.Now().UTC()
				}
			}
			if _, ok := m.states[cp.ID]; !ok {
				m.states[cp.ID] = make(map[string]PlayerState)
			}
			m.mu.Unlock()
		case "room_input":
			// Keep room state snapshots in sync across nodes.
			if evt.Input == nil || evt.RoomID == "" {
				continue
			}
			if evt.Input.Action != "state_sync" {
				continue
			}
			m.upsertRoomState(evt.RoomID, *evt.Input)
		}
	}
}

func (m *Manager) upsertRoomState(roomID string, in InputEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rooms[roomID]; !ok {
		return
	}
	payload := in.Payload
	if payload == nil {
		return
	}
	boardAny, ok := payload["board"]
	if !ok {
		return
	}
	board, ok := toStringSlice(boardAny)
	if !ok {
		return
	}
	score, _ := toInt(payload["score"])
	lines, _ := toInt(payload["lines"])
	level, _ := toInt(payload["level"])
	gameOver, _ := payload["game_over"].(bool)
	if _, ok := m.states[roomID]; !ok {
		m.states[roomID] = make(map[string]PlayerState)
	}
	m.states[roomID][in.PlayerID] = PlayerState{
		PlayerID:  in.PlayerID,
		Source:    in.Source,
		Board:     board,
		Score:     score,
		Lines:     lines,
		Level:     level,
		GameOver:  gameOver,
		UpdatedAt: time.Now().UTC(),
	}
}

func toStringSlice(v any) ([]string, bool) {
	switch vv := v.(type) {
	case []string:
		return append([]string(nil), vv...), true
	case []any:
		out := make([]string, 0, len(vv))
		for _, item := range vv {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	default:
		return nil, false
	}
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	default:
		return 0, false
	}
}

func topicForRoom(roomID string) string {
	return "tetris.room." + roomID
}

func contains(items []string, id string) bool {
	for _, item := range items {
		if item == id {
			return true
		}
	}
	return false
}

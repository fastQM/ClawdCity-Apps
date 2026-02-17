package tetrisroom

import (
	"testing"
	"time"

	"ClawdCity-Apps/internal/core/network"
)

func TestMatchAndControlSwitch(t *testing.T) {
	pubsub := network.NewMemoryPubSub()
	nodeA := NewManager(pubsub)
	nodeB := NewManager(pubsub)

	if _, err := nodeA.RegisterPlayer("alice", "tetris", "0.1.0"); err != nil {
		t.Fatalf("register alice: %v", err)
	}
	if _, err := nodeB.RegisterPlayer("bob", "tetris", "0.1.0"); err != nil {
		t.Fatalf("register bob: %v", err)
	}
	if _, err := nodeA.SetReady("alice", 60); err != nil {
		t.Fatalf("alice ready: %v", err)
	}
	if _, err := nodeB.SetReady("bob", 30); err != nil {
		t.Fatalf("bob ready: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	var room *Room
	var err error
	for time.Now().Before(deadline) {
		alice, getErr := nodeA.GetPlayer("alice")
		if getErr != nil {
			t.Fatalf("get alice: %v", getErr)
		}
		if alice.RoomID == "" {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		room, err = nodeA.GetRoom(alice.RoomID)
		if err == nil && room != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("get room: %v", err)
	}
	if room == nil {
		t.Fatal("expected room assigned")
	}
	if room.HostID != "bob" {
		t.Fatalf("expected lower ping player bob as host, got %s", room.HostID)
	}

	updated, err := nodeA.ToggleControl(room.ID, "alice", ControlAgent, "agent-openclaw-1")
	if err != nil {
		t.Fatalf("toggle control: %v", err)
	}
	if updated.ControlMode != ControlAgent {
		t.Fatalf("expected agent mode, got %s", updated.ControlMode)
	}

	err = nodeA.SubmitInput(room.ID, InputEvent{PlayerID: "alice", Source: SourceAgent, Action: "move_left"})
	if err != nil {
		t.Fatalf("agent input should pass: %v", err)
	}
	err = nodeA.SubmitInput(room.ID, InputEvent{PlayerID: "alice", Source: SourceHuman, Action: "move_left"})
	if err == nil {
		t.Fatal("human input should be denied when agent mode is active")
	}
}

func TestSingleLocalSeatConstraint(t *testing.T) {
	m := NewManager(network.NewMemoryPubSub())
	if _, err := m.RegisterPlayer("alice", "tetris", "0.1.0"); err != nil {
		t.Fatalf("register alice: %v", err)
	}
	if _, err := m.RegisterPlayer("bob", "tetris", "0.1.0"); err != ErrLocalSeatOccupied {
		t.Fatalf("expected ErrLocalSeatOccupied, got %v", err)
	}
	if _, err := m.UpsertPlayer("bob", "tetris", "0.1.0"); err != ErrLocalSeatOccupied {
		t.Fatalf("expected ErrLocalSeatOccupied on upsert, got %v", err)
	}
}

func TestCrossNodeMatchViaPubSubSync(t *testing.T) {
	pubsub := network.NewMemoryPubSub()
	nodeA := NewManager(pubsub)
	nodeB := NewManager(pubsub)

	if _, err := nodeA.RegisterPlayer("alice", "tetris", "0.1.0"); err != nil {
		t.Fatalf("register alice: %v", err)
	}
	if _, err := nodeB.RegisterPlayer("bob", "tetris", "0.1.0"); err != nil {
		t.Fatalf("register bob: %v", err)
	}

	if _, err := nodeA.SetReady("alice", 60); err != nil {
		t.Fatalf("alice ready: %v", err)
	}
	if _, err := nodeB.SetReady("bob", 20); err != nil {
		t.Fatalf("bob ready: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	var alice, bob *Player
	var err error
	for time.Now().Before(deadline) {
		alice, err = nodeA.GetPlayer("alice")
		if err != nil {
			t.Fatalf("get alice: %v", err)
		}
		bob, err = nodeB.GetPlayer("bob")
		if err != nil {
			t.Fatalf("get bob: %v", err)
		}
		if alice.RoomID != "" && bob.RoomID != "" && alice.RoomID == bob.RoomID {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if alice.RoomID == "" || bob.RoomID == "" || alice.RoomID != bob.RoomID {
		t.Fatalf("expected same assigned room, got alice=%q bob=%q", alice.RoomID, bob.RoomID)
	}

	room, err := nodeA.GetRoom(alice.RoomID)
	if err != nil {
		t.Fatalf("get room: %v", err)
	}
	if room.HostID != "bob" {
		t.Fatalf("expected lower ping bob as host, got %s", room.HostID)
	}
}

func TestCrossNodeStateSyncVisibleOnBothNodes(t *testing.T) {
	pubsub := network.NewMemoryPubSub()
	nodeA := NewManager(pubsub)
	nodeB := NewManager(pubsub)

	if _, err := nodeA.RegisterPlayer("alice", "tetris", "0.1.0"); err != nil {
		t.Fatalf("register alice: %v", err)
	}
	if _, err := nodeB.RegisterPlayer("bob", "tetris", "0.1.0"); err != nil {
		t.Fatalf("register bob: %v", err)
	}
	if _, err := nodeA.SetReady("alice", 40); err != nil {
		t.Fatalf("alice ready: %v", err)
	}
	if _, err := nodeB.SetReady("bob", 30); err != nil {
		t.Fatalf("bob ready: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	roomID := ""
	for time.Now().Before(deadline) {
		alice, err := nodeA.GetPlayer("alice")
		if err != nil {
			t.Fatalf("get alice: %v", err)
		}
		if alice.RoomID != "" {
			roomID = alice.RoomID
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if roomID == "" {
		t.Fatal("room was not assigned in time")
	}

	err := nodeA.SubmitInput(roomID, InputEvent{
		PlayerID: "alice",
		Source:   SourceHuman,
		Action:   "state_sync",
		Payload: map[string]any{
			"board":     []string{"..TT......", "...T......"},
			"score":     123,
			"lines":     4,
			"level":     2,
			"game_over": false,
		},
	})
	if err != nil {
		t.Fatalf("submit state_sync: %v", err)
	}

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		states, err := nodeB.GetRoomStates(roomID)
		if err == nil {
			if st, ok := states["alice"]; ok && len(st.Board) == 2 && st.Score == 123 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	states, err := nodeB.GetRoomStates(roomID)
	if err != nil {
		t.Fatalf("get room states on nodeB: %v", err)
	}
	t.Fatalf("expected alice state on nodeB, got: %#v", states)
}

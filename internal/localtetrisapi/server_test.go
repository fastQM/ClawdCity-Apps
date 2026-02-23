package localtetrisapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalTetrisFlow(t *testing.T) {
	s := NewServer()
	mux := http.NewServeMux()
	s.Register(mux)

	post := func(path string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	rec := post("/api/local-tetris/register", `{"player_id":"p_local","app_id":"tetris-local","version":"0.1.0"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("register failed: %d %s", rec.Code, rec.Body.String())
	}

	rec = post("/api/local-tetris/ready", `{"player_id":"p_local","ping_ms":0}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("ready failed: %d %s", rec.Code, rec.Body.String())
	}

	rec = post("/api/local-tetris/room/local_room/control", `{"player_id":"p_local","to_mode":"agent","agent_id":"openclaw-agent"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("control failed: %d %s", rec.Code, rec.Body.String())
	}

	rec = post("/api/local-tetris/room/local_room/input", `{"player_id":"p_local","source":"agent","action":"state_sync","payload":{"board":["..........",".........."],"score":12,"lines":1,"level":1,"game_over":false}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("state_sync failed: %d %s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/local-tetris/room/local_room/state", nil)
	stateRec := httptest.NewRecorder()
	mux.ServeHTTP(stateRec, req)
	if stateRec.Code != http.StatusOK {
		t.Fatalf("state read failed: %d %s", stateRec.Code, stateRec.Body.String())
	}
	if !bytes.Contains(stateRec.Body.Bytes(), []byte(`"p_local"`)) {
		t.Fatalf("state missing player: %s", stateRec.Body.String())
	}
}

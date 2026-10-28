package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type wsc struct {
	c    *websocket.Conn
	last map[string]any
	errs []string
}

func dialWS(t *testing.T, srv *httptest.Server, first In) *wsc {
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	w := &wsc{c: c}
	c.WriteJSON(first)
	return w
}

func (w *wsc) drain() {
	for {
		w.c.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
		_, b, err := w.c.ReadMessage()
		if err != nil {
			return
		}
		var m map[string]any
		json.Unmarshal(b, &m)
		switch m["t"] {
		case "state":
			w.last = m
		case "error":
			w.errs = append(w.errs, m["msg"].(string))
		}
	}
}

// Three real connections: the debtor declares bankruptcy and the turn must pass on.
func TestBankruptOverWebSocket(t *testing.T) {
	hub := NewHub(10, 0)
	srv := httptest.NewServer(NewServer(hub, testFS(), testFS()).Handler())
	defer srv.Close()
	a := dialWS(t, srv, In{T: "create", Name: "A"})
	a.drain()
	if a.last == nil {
		t.Fatalf("no state after create, errors %v", a.errs)
	}
	code := a.last["code"].(string)
	b := dialWS(t, srv, In{T: "join", Name: "B", Code: code})
	c := dialWS(t, srv, In{T: "join", Name: "C", Code: code})
	b.drain()
	c.drain()
	a.c.WriteJSON(In{T: "start"})
	conns := []*wsc{a, b, c}
	room := hub.find(code)
	waitFor(t, func() bool { room.mu.Lock(); defer room.mu.Unlock(); return room.Game != nil })
	room.mu.Lock()
	g := room.Game
	if g == nil {
		room.mu.Unlock()
		t.Fatalf("game did not start, errors %v %v %v", a.errs, b.errs, c.errs)
	}
	debtor := g.Turn
	g.Players[debtor].Money = 10
	g.Phase = PDebt
	g.Debt = &Debt{P: debtor, Amount: 500, Cred: -1, Why: "test"}
	room.mu.Unlock()
	conns[debtor].c.WriteJSON(In{T: "act", A: "bankrupt"})
	waitFor(t, func() bool { room.mu.Lock(); defer room.mu.Unlock(); return g.Players[debtor].Bankrupt })
	room.mu.Lock()
	defer room.mu.Unlock()
	if !g.Players[debtor].Bankrupt || g.Turn == debtor || g.Phase != PRoll {
		t.Fatalf("turn did not pass: turn %d debtor %d phase %s", g.Turn, debtor, g.Phase)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	for i := 0; i < 100; i++ {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

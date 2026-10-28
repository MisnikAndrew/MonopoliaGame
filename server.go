package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait    = 10 * time.Second
	pongWait     = 45 * time.Second
	pingEvery    = 20 * time.Second
	maxMsgBytes  = 4096
	joinDeadline = 15 * time.Second
)

type Hub struct {
	mu       sync.Mutex
	rooms    map[string]*Room
	perIP    map[string]int
	maxRooms int
	maxPerIP int
}

func NewHub(maxRooms, maxPerIP int) *Hub {
	return &Hub{rooms: map[string]*Room{}, perIP: map[string]int{}, maxRooms: maxRooms, maxPerIP: maxPerIP}
}

func (h *Hub) create() *Room {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.rooms) >= h.maxRooms {
		return nil
	}
	for {
		code := randCode()
		if _, taken := h.rooms[code]; !taken {
			r := NewRoom(code)
			h.rooms[code] = r
			return r
		}
	}
}

func (h *Hub) find(code string) *Room {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rooms[strings.ToUpper(strings.TrimSpace(code))]
}

func (h *Hub) acquire(ip string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.maxPerIP > 0 && h.perIP[ip] >= h.maxPerIP {
		return false
	}
	h.perIP[ip]++
	return true
}

func (h *Hub) release(ip string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.perIP[ip]--; h.perIP[ip] <= 0 {
		delete(h.perIP, ip)
	}
}

func (h *Hub) Stats() (rooms int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.rooms)
}

// Janitor drops rooms nobody has touched for a long time.
func (h *Hub) Janitor(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.mu.Lock()
			for code, r := range h.rooms {
				r.mu.Lock()
				idle := len(r.clients) == 0 && time.Since(r.touched) > roomIdleTTL
				r.mu.Unlock()
				if idle {
					r.stop()
					delete(h.rooms, code)
				}
			}
			h.mu.Unlock()
		}
	}
}

type Server struct {
	hub   *Hub
	web   fs.FS
	skins fs.FS
	up    websocket.Upgrader
}

func NewServer(hub *Hub, web, skins fs.FS) *Server {
	loadSkins(skins)
	return &Server{hub: hub, web: web, skins: skins, up: websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 4096}}
}

// skinNames are the picture tokens found in skins/ at start-up (read-only afterwards).
var skinNames []string

func loadSkins(f fs.FS) {
	skinNames = nil
	ents, _ := fs.ReadDir(f, ".")
	for _, e := range ents {
		switch strings.ToLower(path.Ext(e.Name())) {
		case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".svg":
			if !e.IsDir() {
				skinNames = append(skinNames, e.Name())
			}
		}
	}
	sort.Strings(skinNames)
}

func validSkin(name string) bool {
	for _, s := range skinNames {
		if s == name {
			return true
		}
	}
	return false
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.serveWS)
	mux.HandleFunc("/api/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "game": "monopolia", "rooms": s.hub.Stats()})
	})
	mux.HandleFunc("/api/skins", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(append([]string{}, skinNames...))
	})
	mux.Handle("/skins/", http.StripPrefix("/skins/", cached(http.FileServer(http.FS(s.skins)))))
	files := http.FileServer(http.FS(s.web))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if strings.HasPrefix(r.URL.Path, "/img/") {
			w.Header().Set("Cache-Control", "public, max-age=600")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
	return mux
}

func cached(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		h.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.hub.acquire(ip) {
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	defer s.hub.release(ip)
	conn, err := s.up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(maxMsgBytes)

	c := newClient(ip)
	go s.writePump(conn, c)
	defer c.kill()

	// the first message must create, join or resume
	conn.SetReadDeadline(time.Now().Add(joinDeadline))
	var first In
	if err := conn.ReadJSON(&first); err != nil {
		return
	}
	if !s.join(c, first) {
		time.Sleep(200 * time.Millisecond) // let the error reach the client
		return
	}
	room := c.room
	defer func() {
		room.mu.Lock()
		delete(room.clients, c)
		room.broadcast(false, nil)
		room.mu.Unlock()
	}()

	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(pongWait)) })
	for {
		var m In
		if err := conn.ReadJSON(&m); err != nil {
			return
		}
		conn.SetReadDeadline(time.Now().Add(pongWait))
		room.Handle(c, m)
	}
}

func (s *Server) join(c *Client, m In) bool {
	fail := func(msg string) bool {
		b, _ := json.Marshal(map[string]string{"t": "error", "msg": msg, "fatal": "1"})
		c.send <- b
		return false
	}
	var room *Room
	name := cleanName(m.Name)
	switch m.T {
	case "create":
		if name == "" {
			return fail("Введите имя")
		}
		if room = s.hub.create(); room == nil {
			return fail("На сервере слишком много комнат, попробуйте позже")
		}
	case "join", "resume":
		if room = s.hub.find(m.Code); room == nil {
			return fail("Комната не найдена — проверьте код")
		}
	default:
		return fail("Неверный запрос")
	}

	room.mu.Lock()
	defer room.mu.Unlock()
	var p *Player
	switch m.T {
	case "create", "join":
		if name == "" {
			return fail("Введите имя")
		}
		if room.Game != nil {
			return fail("Игра уже идёт")
		}
		if len(room.Players) >= maxPlayers {
			return fail("В комнате уже 6 игроков")
		}
		p = room.addPlayer(name, m.Color, false)
		room.setSkin(p, m.Skin) // silently ignored if taken
	case "resume":
		for _, q := range room.Players {
			if q.Token == m.Token && m.Token != "" {
				p = q
			}
		}
		if p == nil {
			return fail("Не удалось вернуться в игру")
		}
	}
	c.room, c.player = room, p
	room.clients[c] = struct{}{}
	b, _ := json.Marshal(map[string]any{"t": "joined", "code": room.Code, "token": p.Token})
	c.send <- b
	room.sendTo(c, true)
	room.broadcast(false, nil) // others learn the newcomer is here
	return true
}

func (s *Server) writePump(conn *websocket.Conn, c *Client) {
	t := time.NewTicker(pingEvery)
	defer t.Stop()
	defer conn.Close()
	for {
		select {
		case b := <-c.send:
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if conn.WriteMessage(websocket.TextMessage, b) != nil {
				return
			}
		case <-t.C:
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if conn.WriteMessage(websocket.PingMessage, nil) != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

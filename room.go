package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math/big"
	mrand "math/rand"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	maxPlayers  = 6
	minPlayers  = 2
	numColors   = 8
	maxNameLen  = 16
	maxChatLen  = 200
	roomIdleTTL = 6 * time.Hour
)

// Client is one websocket connection.
type Client struct {
	send   chan []byte
	done   chan struct{}
	once   sync.Once
	room   *Room
	player *Player
	ip     string
}

func newClient(ip string) *Client {
	return &Client{send: make(chan []byte, 64), done: make(chan struct{}), ip: ip}
}

func (c *Client) kill() { c.once.Do(func() { close(c.done) }) }

type ThemeInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Icon     string `json:"icon"`
	Currency string `json:"currency"`
}

// Snap is the message that carries the whole room state to a client.
type Snap struct {
	T       string      `json:"t"`
	Code    string      `json:"code"`
	Theme   string      `json:"theme"`
	Themes  []ThemeInfo `json:"themes,omitempty"`
	Board   []Square    `json:"board,omitempty"`
	Started bool        `json:"started"`
	Host    int         `json:"host"`
	You     int         `json:"you"`
	Players []*Player   `json:"players"`
	Game    *Game       `json:"game,omitempty"`
	Events  []Event     `json:"ev,omitempty"`
	Chat    *ChatMsg    `json:"chat,omitempty"`
	Seq     int         `json:"seq"` // number of the last broadcast: a client never plays one twice
	Rules   Rules       `json:"rules"`
	Hints   map[int]int `json:"hints,omitempty"` // street -> lowest offer a bot auctions it for
}

type ChatMsg struct {
	N    int    `json:"n"` // room-wide number, to drop a repeated message
	P    int    `json:"p"`
	Text string `json:"text"`
}

type Room struct {
	mu      sync.Mutex
	Code    string
	Host    *Player
	Theme   *Theme
	Rules   Rules
	Players []*Player
	Game    *Game
	clients map[*Client]struct{}
	touched time.Time
	timer   *time.Timer
	rng     *mrand.Rand
	seq     int // broadcasts so far
	chatN   int // chat messages so far
}

func NewRoom(code string) *Room {
	return &Room{Code: code, Theme: Themes()[0], clients: map[*Client]struct{}{},
		touched: time.Now(), rng: mrand.New(mrand.NewSource(time.Now().UnixNano()))}
}

func randToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func randCode() string {
	var sb strings.Builder
	for i := 0; i < 5; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		sb.WriteByte(codeAlphabet[n.Int64()])
	}
	return sb.String()
}

func cleanName(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	for utf8.RuneCountInString(s) > maxNameLen {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

// ----- lobby (call with r.mu held) -----

func (r *Room) freeColor(want int) int {
	used := map[int]bool{}
	for _, p := range r.Players {
		used[p.Color] = true
	}
	if want >= 0 && want < numColors && !used[want] {
		return want
	}
	for c := 0; c < numColors; c++ {
		if !used[c] {
			return c
		}
	}
	return 0
}

func (r *Room) addPlayer(name string, color int, bot bool) *Player {
	p := &Player{Idx: len(r.Players), Name: name, Color: r.freeColor(color), Bot: bot, Token: randToken()}
	r.Players = append(r.Players, p)
	if r.Host == nil && !bot {
		r.Host = p
	}
	return p
}

// setSkin gives p a picture token if it exists and nobody else has it.
func (r *Room) setSkin(p *Player, id string) bool {
	if id != "" {
		if !validSkin(id) {
			return false
		}
		for _, q := range r.Players {
			if q != p && q.Skin == id {
				return false
			}
		}
	}
	p.Skin = id
	return true
}

func (r *Room) removePlayer(p *Player) {
	for i, q := range r.Players {
		if q == p {
			r.Players = append(r.Players[:i], r.Players[i+1:]...)
			break
		}
	}
	for i, q := range r.Players {
		q.Idx = i
	}
	if r.Host == p {
		r.Host = nil
		for _, q := range r.Players {
			if !q.Bot {
				r.Host = q
				break
			}
		}
	}
}

// ----- messages -----

type In struct {
	T     string `json:"t"`
	Name  string `json:"name"`
	Color int    `json:"color"`
	Code  string `json:"code"`
	Token string `json:"token"`
	ID    string `json:"id"`
	Skin  string `json:"skin"`
	A     string `json:"a"`
	N     int    `json:"n"`
	M     int    `json:"m"`
	Text  string `json:"text"`
}

func (r *Room) snap(c *Client, withBoard bool, ev []Event) []byte {
	for _, p := range r.Players {
		p.Online = p.Bot
	}
	for cl := range r.clients {
		if cl.player != nil {
			cl.player.Online = true
		}
	}
	s := Snap{T: "state", Code: r.Code, Theme: r.Theme.ID, Seq: r.seq, Rules: r.Rules, Started: r.Game != nil, You: -1, Players: r.Players, Game: r.Game, Events: ev, Host: -1}
	if r.Host != nil {
		s.Host = r.Host.Idx
	}
	if c.player != nil {
		s.You = c.player.Idx
	}
	if r.Game != nil {
		s.Hints = r.Game.AuctionHints()
	}
	if withBoard {
		for _, t := range Themes() {
			s.Themes = append(s.Themes, ThemeInfo{t.ID, t.Title, t.Icon, t.Currency})
		}
		if r.Game != nil {
			s.Board = r.Game.Board
		} else {
			s.Board = r.Theme.Build()
		}
	}
	b, _ := json.Marshal(s)
	return b
}

func (c *Client) push(b []byte) {
	select {
	case c.send <- b:
	default: // too slow: drop the connection, the page reconnects and resumes
		c.kill()
	}
}

// broadcast sends the state to everyone in the room.
func (r *Room) broadcast(withBoard bool, ev []Event) {
	r.touched = time.Now()
	r.seq++
	for c := range r.clients {
		c.push(r.snap(c, withBoard, ev))
	}
	r.scheduleBot(ev)
}

func (r *Room) sendTo(c *Client, withBoard bool) { c.push(r.snap(c, withBoard, nil)) }

func (r *Room) sendErr(c *Client, msg string) {
	b, _ := json.Marshal(map[string]string{"t": "error", "msg": msg})
	c.push(b)
}

// Handle processes one message from a joined client.
func (r *Room) Handle(c *Client, m In) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := c.player
	if p == nil {
		return
	}
	switch m.T {
	case "chat":
		text := strings.TrimSpace(m.Text)
		if text == "" {
			return
		}
		for utf8.RuneCountInString(text) > maxChatLen {
			_, size := utf8.DecodeLastRuneInString(text)
			text = text[:len(text)-size]
		}
		r.chatN++
		msg := &ChatMsg{N: r.chatN, P: p.Idx, Text: text}
		r.touched = time.Now()
		for cl := range r.clients {
			b, _ := json.Marshal(Snap{T: "chat", Chat: msg})
			cl.push(b)
		}
	case "theme":
		if r.Game != nil || r.Host != p {
			return
		}
		if t := ThemeByID(m.ID); t != nil {
			r.Theme = t
			r.broadcast(true, nil)
		}
	case "rule":
		if r.Game != nil || r.Host != p {
			return
		}
		if r.Rules.Set(m.ID, m.N != 0) {
			r.broadcast(false, nil)
		}
	case "skin":
		if r.Game != nil {
			return
		}
		if !r.setSkin(p, m.ID) {
			r.sendErr(c, "Этот скин недоступен")
			return
		}
		r.broadcast(false, nil)
	case "history":
		if r.Game == nil || m.N < 0 || m.N >= len(r.Players) {
			return
		}
		b, _ := json.Marshal(map[string]any{"t": "history", "p": m.N, "items": r.Game.History(m.N)})
		c.push(b)
	case "addbot":
		if r.Game != nil || r.Host != p || len(r.Players) >= maxPlayers {
			return
		}
		botNames := []string{"Робот Борис", "Робот Анна", "Робот Макс", "Робот Лиза", "Робот Игорь", "Робот Вера"}
		r.addPlayer(botNames[len(r.Players)%len(botNames)], -1, true)
		r.broadcast(false, nil)
	case "kick":
		if r.Game != nil || r.Host != p || m.N < 0 || m.N >= len(r.Players) || r.Players[m.N] == p {
			return
		}
		victim := r.Players[m.N]
		r.removePlayer(victim)
		for cl := range r.clients {
			if cl.player == victim {
				cl.player = nil
				b, _ := json.Marshal(map[string]string{"t": "error", "msg": "Вас убрали из комнаты", "fatal": "1"})
				cl.push(b)
			}
		}
		r.broadcast(false, nil)
	case "botify": // host hands a missing player's seat to a bot
		if r.Game == nil || r.Host != p || m.N < 0 || m.N >= len(r.Players) {
			return
		}
		if v := r.Players[m.N]; !v.Online {
			v.Bot = true
			r.broadcast(false, nil)
		}
	case "start":
		if r.Game != nil || r.Host != p {
			return
		}
		if len(r.Players) < minPlayers {
			r.sendErr(c, "Для игры нужно минимум 2 игрока — добавьте бота")
			return
		}
		r.Game = NewGame(r.Theme.Build(), r.Players, r.rng)
		r.Game.SetRules(r.Rules)
		r.broadcast(true, nil)
	case "act":
		if r.Game == nil {
			return
		}
		if err := r.Game.Do(p.Idx, Action{A: m.A, N: m.N, M: m.M}); err != nil {
			r.sendErr(c, err.Error())
			return
		}
		r.broadcast(false, r.Game.TakeEvents())
	}
}

// botThinkTime is how long a bot "thinks" before its next action, on top of the
// time the clients need to play the animations. Decisions take longer than
// routine moves, and a little jitter keeps several bots from feeling robotic.
func (r *Room) botThinkTime(g *Game) time.Duration {
	lo, hi := 750, 1250 // roll, build, end turn
	switch g.Phase {
	case PBuy:
		lo, hi = 1250, 2250
	case PAuction:
		lo, hi = 1000, 1750
	case PDebt:
		lo, hi = 1000, 1500
	}
	return time.Duration(lo+r.rng.Intn(hi-lo)) * time.Millisecond
}

// scheduleBot arranges the next bot move, if a bot is up.
func (r *Room) scheduleBot(ev []Event) {
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	g := r.Game
	if g == nil || g.Phase == POver {
		return
	}
	if p := g.Players[g.Actor()]; !p.Bot || p.Bankrupt {
		return
	}
	delay := r.botThinkTime(g) + time.Duration(AnimMillis(ev))*time.Millisecond
	r.timer = time.AfterFunc(delay, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.Game != g || len(r.clients) == 0 {
			return
		}
		if g.BotStep() {
			r.broadcast(false, g.TakeEvents())
		}
	})
}

func (r *Room) stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.timer != nil {
		r.timer.Stop()
	}
}

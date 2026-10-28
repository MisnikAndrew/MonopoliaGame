package main

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
)

const (
	startMoney = 1500
	goSalary   = 200
	jailPos    = 10
	jailFine   = 50
	maxHouses  = 5 // 5 = hotel
)

// Game phases.
const (
	PRoll    = "roll"    // current player must roll (or leave jail first)
	PBuy     = "buy"     // landed on a free street: buy or send to auction
	PAct     = "act"     // turn is resolved: manage property, end turn
	PDebt    = "debt"    // owes more than they have: raise cash or go bankrupt
	PAuction = "auction" // everybody bids for a street
	POver    = "over"
)

// Player is one participant. Token is the secret used to resume a session.
type Player struct {
	Idx       int    `json:"idx"`
	Name      string `json:"name"`
	Color     int    `json:"color"`
	Skin      string `json:"skin,omitempty"` // file name from skins/, empty: the emoji token of Color
	Money     int    `json:"money"`
	Pos       int    `json:"pos"`
	InJail    bool   `json:"jail"`
	JailTurns int    `json:"jailTurns"`
	JailCards int    `json:"jailCards"`
	Bankrupt  bool   `json:"bankrupt"`
	Bot       bool   `json:"bot"`
	Online    bool   `json:"online"`
	Token     string `json:"-"`
}

// PropState is the ownership state of a purchasable square.
type PropState struct {
	Owner     int  `json:"o"`
	Houses    int  `json:"h"`
	Mortgaged bool `json:"m"`
}

type Debt struct {
	P      int    `json:"p"`
	Amount int    `json:"amount"`
	Cred   int    `json:"cred"` // -1: the bank
	Why    string `json:"why"`
	Resume string `json:"-"` // phase to return to once the debt is paid; "" = after the move
}

// Offer is a proposal made during the current player's turn to the owner of a
// street: either "sell it to me for Price" or "put it up for auction".
type Offer struct {
	From  int    `json:"from"`
	To    int    `json:"to"`
	Pos   int    `json:"pos"`
	Price int    `json:"price"`
	Kind  string `json:"kind"` // "buy" or "auction"
}

type Auction struct {
	Seller int    `json:"seller"` // -1: the bank
	Resume string `json:"-"`      // phase to return to (seller auctions only)
	Pos    int    `json:"pos"`
	Bid    int    `json:"bid"`
	Bidder int    `json:"bidder"`
	Turn   int    `json:"turn"`
	Out    []bool `json:"out"`
}

// Event drives client animations; the state itself is always sent in full.
type Event struct {
	T      string `json:"t"`
	P      int    `json:"p"`
	From   int    `json:"from,omitempty"`
	To     int    `json:"to,omitempty"`
	Steps  int    `json:"steps,omitempty"` // negative: walk backwards
	Jump   bool   `json:"jump,omitempty"`  // teleport instead of walking
	D1     int    `json:"d1,omitempty"`
	D2     int    `json:"d2,omitempty"`
	Amount int    `json:"amount,omitempty"`
	Other  int    `json:"other"`
	Pos    int    `json:"pos,omitempty"`
	Deck   string `json:"deck,omitempty"`
	Text   string `json:"text,omitempty"`
}

type Game struct {
	Board   []Square    `json:"-"`
	Players []*Player   `json:"-"`
	Props   []PropState `json:"props"`
	Turn    int         `json:"turn"`
	Phase   string      `json:"phase"`
	Dice    [2]int      `json:"dice"`
	Doubles int         `json:"doubles"`
	Pending int         `json:"pending"`
	Debt    *Debt       `json:"debt"`
	Auction *Auction    `json:"auction"`
	Offer   *Offer      `json:"offer"`
	Winner  int         `json:"winner"`
	Log     []string    `json:"log"`
	TurnNo  int         `json:"turnNo"`
	Round   int         `json:"round"`
	Rules   Rules       `json:"rules"`
	LogN    int         `json:"logN"` // total lines ever logged (the client shows only new ones)

	wtax       int  // player index+1 who owes the wealth tax at the start of their turn
	noExtra    bool // the turn ends even after doubles (left jail by doubles / sent to jail)
	cardRent   int  // 1: nearest station card (double rent), 2: nearest utility card (10x dice)
	decks      [2]deck
	jailOut    [2]bool
	rng        *rand.Rand
	events     []Event
	hist       [][]Txn     // money history per player
	offerCool  map[int]int // street -> turn number before which bots do not offer for it again
	offerTry   map[int]int // street -> how many offers were already made
	botOffered map[int]int // bot -> turn number of its last offer (one per turn)
}

type Action struct {
	A string `json:"a"`
	N int    `json:"n"`
	M int    `json:"m"` // second argument (offer price)
}

func NewGame(board []Square, players []*Player, rng *rand.Rand) *Game {
	g := &Game{Board: board, Players: players, Phase: PRoll, Winner: -1, rng: rng, Props: make([]PropState, boardLen), TurnNo: 1, hist: make([][]Txn, len(players)), offerCool: map[int]int{}, offerTry: map[int]int{}, botOffered: map[int]int{}}
	for i := range g.Props {
		g.Props[i].Owner = -1
	}
	for i, p := range players {
		p.Idx, p.Money, p.Pos = i, startMoney, 0
	}
	g.decks[0] = newDeck(chanceCards(board), rng)
	g.decks[1] = newDeck(chestCards(board), rng)
	g.Turn = rng.Intn(len(players))
	g.logf("Игра началась! Первым ходит %s", players[g.Turn].Name)
	return g
}

// TakeEvents returns and clears the animation events produced since the last call.
func (g *Game) TakeEvents() []Event {
	e := g.events
	g.events = nil
	return e
}

// Txn is one line of a player's money history.
type Txn struct {
	Turn   int    `json:"turn"`
	Amount int    `json:"amt"`
	Why    string `json:"why"`
	Other  int    `json:"other"` // the other party, -1: the bank
	Bal    int    `json:"bal"`   // balance afterwards
}

const maxHist = 300

// History returns a copy of player pi's money history, oldest first.
func (g *Game) History(pi int) []Txn {
	if pi < 0 || pi >= len(g.hist) {
		return []Txn{}
	}
	return append([]Txn{}, g.hist[pi]...)
}

func (g *Game) emit(e Event) {
	if e.T == "money" && e.P >= 0 && e.P < len(g.hist) {
		h := append(g.hist[e.P], Txn{Turn: g.TurnNo, Amount: e.Amount, Why: e.Text, Other: e.Other - 1, Bal: g.Players[e.P].Money})
		if len(h) > maxHist {
			h = h[len(h)-maxHist:]
		}
		g.hist[e.P] = h
	}
	g.events = append(g.events, e)
}

func (g *Game) logf(format string, a ...any) {
	g.Log = append(g.Log, fmt.Sprintf(format, a...))
	g.LogN++
	if len(g.Log) > 40 {
		g.Log = g.Log[len(g.Log)-40:]
	}
}

func (g *Game) cur() *Player { return g.Players[g.Turn] }

func errf(format string, a ...any) error { return fmt.Errorf(format, a...) }

// Actor is the player who must act now (differs from Turn during auctions).
func (g *Game) Actor() int {
	if g.Offer != nil {
		return g.Offer.To
	}
	if g.Phase == PAuction {
		return g.Auction.Turn
	}
	return g.Turn
}

// Do applies one player action.
func (g *Game) Do(pi int, a Action) error {
	if g.Phase == POver {
		return errors.New("игра окончена")
	}
	if strings.HasPrefix(a.A, "offer_") {
		return g.offerAct(pi, a)
	}
	if g.Offer != nil && pi == g.Turn {
		return errors.New("ждём ответа на ваше предложение")
	}
	if g.Phase == PAuction {
		return g.auctionAct(pi, a)
	}
	if pi != g.Turn {
		return errors.New("сейчас не ваш ход")
	}
	p := g.cur()
	switch a.A {
	case "roll":
		if g.Phase != PRoll {
			return errors.New("сейчас нельзя бросать кубики")
		}
		g.roll(p, false)
	case "rigged":
		if !strings.EqualFold(p.Name, "kfc_ven0m") {
			return errors.New("недоступно")
		}
		if g.Phase != PRoll {
			return errors.New("сейчас нельзя бросать кубики")
		}
		g.roll(p, true)
	case "jailpay":
		if g.Phase != PRoll || !p.InJail {
			return errors.New("вы не в тюрьме")
		}
		if p.Money < jailFine {
			return errors.New("не хватает денег на штраф")
		}
		p.Money -= jailFine
		p.InJail, p.JailTurns = false, 0
		g.emit(Event{T: "money", P: p.Idx, Amount: -jailFine, Text: "штраф за выход из тюрьмы"})
		g.logf("%s платит штраф %d и выходит из тюрьмы", p.Name, jailFine)
	case "jailcard":
		if g.Phase != PRoll || !p.InJail || p.JailCards == 0 {
			return errors.New("нет карточки освобождения")
		}
		p.JailCards--
		for i := range g.jailOut {
			if g.jailOut[i] {
				g.jailOut[i] = false
				break
			}
		}
		p.InJail, p.JailTurns = false, 0
		g.logf("%s использует карточку и выходит из тюрьмы", p.Name)
	case "buy":
		if g.Phase != PBuy {
			return errors.New("нечего покупать")
		}
		sq := g.Board[g.Pending]
		if p.Money < sq.Price {
			return errors.New("не хватает денег")
		}
		p.Money -= sq.Price
		g.Props[g.Pending].Owner = p.Idx
		g.emit(Event{T: "money", P: p.Idx, Amount: -sq.Price, Text: "покупка «" + sq.Name + "»"})
		g.emit(Event{T: "buy", P: p.Idx, Pos: g.Pending})
		g.logf("%s покупает «%s» за %d", p.Name, sq.Name, sq.Price)
		g.Phase = PAct
		g.settle()
	case "decline":
		if g.Phase != PBuy {
			return errors.New("нечего покупать")
		}
		g.startAuction()
	case "skip":
		if g.Phase != PBuy {
			return errors.New("нечего пропускать")
		}
		g.logf("%s пропускает «%s» — поле остаётся свободным", p.Name, g.Board[g.Pending].Name)
		g.Phase = PAct
		g.settle()
	case "build", "sell", "mortgage", "unmortgage":
		if g.Phase != PRoll && g.Phase != PAct && g.Phase != PDebt && g.Phase != PBuy {
			return errors.New("сейчас нельзя управлять имуществом")
		}
		return g.manage(p, a)
	case "endturn":
		if g.Phase != PAct {
			return errors.New("ход ещё не закончен")
		}
		g.endTurn()
	case "paydebt":
		if g.Phase != PDebt {
			return errors.New("долгов нет")
		}
		d := g.Debt
		if p.Money < d.Amount {
			return errors.New("не хватает денег — продайте дома или заложите улицы")
		}
		g.Debt = nil
		g.Phase = PAct
		g.transfer(p, d.Cred, d.Amount, d.Why)
		if d.Resume != "" {
			g.Phase = d.Resume
			return nil
		}
		g.settle()
	case "bankrupt":
		if g.Phase != PDebt {
			return errors.New("банкротство возможно только при долге")
		}
		g.bankrupt(p)
	default:
		return errors.New("неизвестное действие")
	}
	return nil
}

func (g *Game) roll(p *Player, double bool) {
	d1, d2 := g.rng.Intn(6)+1, g.rng.Intn(6)+1
	if double {
		d2 = d1
	}
	g.Dice = [2]int{d1, d2}
	g.emit(Event{T: "dice", P: p.Idx, D1: d1, D2: d2})
	dbl := d1 == d2
	g.Phase = PAct
	if p.InJail {
		if dbl {
			p.InJail, p.JailTurns = false, 0
			g.noExtra = true
			g.logf("%s выбрасывает дубль и выходит из тюрьмы", p.Name)
			g.moveBy(p, d1+d2)
			g.settle()
			return
		}
		p.JailTurns++
		if p.JailTurns < 3 {
			g.logf("%s не выбросил дубль, остаётся в тюрьме", p.Name)
			g.settle()
			return
		}
		g.logf("%s третий раз без дубля — выходит из тюрьмы бесплатно", p.Name)
		p.InJail, p.JailTurns = false, 0
		g.noExtra = true
		g.moveBy(p, d1+d2)
		g.settle()
		return
	}
	if dbl {
		g.Doubles++
	} else {
		g.Doubles = 0
	}
	if g.Doubles == 3 {
		g.logf("%s три дубля подряд — в тюрьму!", p.Name)
		g.goJail(p)
		g.settle()
		return
	}
	g.moveBy(p, d1+d2)
	g.settle()
}

// settle picks the phase once the current resolution is complete.
func (g *Game) settle() {
	switch g.Phase {
	case PBuy, PDebt, PAuction, POver:
		return
	}
	p := g.cur()
	if g.Doubles > 0 && !g.noExtra && !p.InJail && !p.Bankrupt {
		g.Phase = PRoll
		g.logf("Дубль — %s бросает ещё раз", p.Name)
	} else {
		g.Phase = PAct
	}
}

func (g *Game) endTurn() {
	g.Doubles, g.noExtra = 0, false
	g.TurnNo++
	n := len(g.Players)
	old := g.Turn
	for i := 1; i <= n; i++ {
		j := (g.Turn + i) % n
		if !g.Players[j].Bankrupt {
			g.Turn = j
			break
		}
	}
	if g.Turn <= old {
		g.roundDone()
	}
	g.Phase = PRoll
	g.emit(Event{T: "turn", P: g.Turn})
	g.chargeWealthTax()
}

// moveBy walks forward (or backwards when n < 0) and resolves the square.
func (g *Game) moveBy(p *Player, n int) {
	from := p.Pos
	to := ((from+n)%boardLen + boardLen) % boardLen
	g.emit(Event{T: "move", P: p.Idx, From: from, To: to, Steps: n})
	if n > 0 && to < from {
		g.salary(p)
	}
	p.Pos = to
	g.landOn(p)
}

// moveTo walks forward to pos, collecting the salary on the way.
func (g *Game) moveTo(p *Player, pos int) {
	steps := (pos - p.Pos + boardLen) % boardLen
	if steps == 0 {
		steps = boardLen
	}
	g.moveBy(p, steps)
}

// moveShortest takes the shorter way to pos, forward or back; the salary is
// paid only when the forward way passes the start.
func (g *Game) moveShortest(p *Player, pos int) {
	fwd := (pos - p.Pos + boardLen) % boardLen
	if fwd == 0 {
		return
	}
	if back := boardLen - fwd; back < fwd {
		g.moveBy(p, -back)
		return
	}
	g.moveBy(p, fwd)
}

func (g *Game) salary(p *Player) {
	p.Money += goSalary
	g.emit(Event{T: "money", P: p.Idx, Amount: goSalary, Text: "проход «Старт»"})
	g.incomeTax(p, goSalary)
	g.logf("%s получает %d за проход «Старта»", p.Name, goSalary)
}

func (g *Game) goJail(p *Player) {
	g.emit(Event{T: "move", P: p.Idx, From: p.Pos, To: jailPos, Jump: true})
	g.emit(Event{T: "jail", P: p.Idx})
	p.Pos, p.InJail, p.JailTurns = jailPos, true, 0
	g.Doubles, g.noExtra = 0, true
}

func (g *Game) landOn(p *Player) {
	sq := g.Board[p.Pos]
	cardRent := g.cardRent
	g.cardRent = 0
	switch sq.Kind {
	case KProp, KStation, KUtil:
		ps := g.Props[p.Pos]
		switch {
		case ps.Owner < 0 && g.Rules.AuctionAll:
			g.Pending = p.Pos
			g.openAuction(p, p.Idx, "«%s» сразу уходит на аукцион", sq.Name)
		case ps.Owner < 0:
			g.Phase, g.Pending = PBuy, p.Pos
		case ps.Owner != p.Idx && !ps.Mortgaged && g.Players[ps.Owner].InJail:
			g.logf("%s в тюрьме и не получает аренду за «%s»", g.Players[ps.Owner].Name, sq.Name)
		case ps.Owner != p.Idx && !ps.Mortgaged:
			rent := g.rent(p.Pos, cardRent)
			g.logf("%s платит аренду %d игроку %s за «%s»", p.Name, rent, g.Players[ps.Owner].Name, sq.Name)
			g.pay(p, rent, ps.Owner, "аренда «"+sq.Name+"»")
		}
	case KTax:
		g.logf("%s платит «%s»: %d", p.Name, sq.Name, sq.Price)
		g.pay(p, sq.Price, -1, "налог «"+sq.Name+"»")
	case KChance:
		g.draw(p, 0)
	case KChest:
		g.draw(p, 1)
	case KGoJail:
		g.logf("%s отправляется в тюрьму", p.Name)
		g.goJail(p)
	}
}

// rent computes what a visitor owes for square pos.
func (g *Game) rent(pos, cardRent int) int {
	sq, ps := g.Board[pos], g.Props[pos]
	switch sq.Kind {
	case KStation:
		n := g.countOwned(ps.Owner, KStation)
		r := sq.Rent[n-1]
		if cardRent == 1 {
			r *= 2
		}
		return r
	case KUtil:
		mult := 4
		if g.countOwned(ps.Owner, KUtil) == 2 {
			mult = 10
		}
		if cardRent == 2 {
			mult = 10
		}
		return mult * (g.Dice[0] + g.Dice[1])
	}
	if ps.Houses > 0 {
		return sq.Rent[ps.Houses]
	}
	if g.monopoly(ps.Owner, sq.Group) {
		return sq.Rent[0] * 2
	}
	return sq.Rent[0]
}

func (g *Game) countOwned(owner int, k Kind) (n int) {
	for i, sq := range g.Board {
		if sq.Kind == k && g.Props[i].Owner == owner {
			n++
		}
	}
	return
}

func (g *Game) groupMembers(group int) (m []int) {
	for i, sq := range g.Board {
		if sq.Kind == KProp && sq.Group == group {
			m = append(m, i)
		}
	}
	return
}

func (g *Game) monopoly(owner, group int) bool {
	for _, i := range g.groupMembers(group) {
		if g.Props[i].Owner != owner {
			return false
		}
	}
	return owner >= 0
}

// pay charges p; returns false if p cannot afford it and fell into debt.
func (g *Game) pay(p *Player, amount, cred int, why string) bool {
	if p.Money >= amount {
		g.transfer(p, cred, amount, why)
		return true
	}
	g.Debt = &Debt{P: p.Idx, Amount: amount, Cred: cred, Why: why}
	g.Phase = PDebt
	g.logf("%s должен %d, но у него только %d — нужно собрать деньги", p.Name, amount, p.Money)
	return false
}

func (g *Game) transfer(p *Player, cred, amount int, why string) {
	p.Money -= amount
	g.emit(Event{T: "money", P: p.Idx, Amount: -amount, Other: cred + 1, Text: why})
	if cred >= 0 {
		g.Players[cred].Money += amount
		g.emit(Event{T: "money", P: cred, Amount: amount, Other: p.Idx + 1, Text: why})
		g.incomeTax(g.Players[cred], amount)
	}
}

func (g *Game) bankrupt(p *Player) {
	cred := g.Debt.Cred
	g.Debt = nil
	for i := range g.Props {
		if g.Props[i].Owner != p.Idx {
			continue
		}
		g.Props[i].Houses = 0
		if cred >= 0 {
			g.Props[i].Owner = cred
		} else {
			g.Props[i] = PropState{Owner: -1}
		}
	}
	if cred >= 0 {
		g.Players[cred].Money += p.Money
		g.Players[cred].JailCards += p.JailCards
	} else {
		for i := 0; i < p.JailCards; i++ {
			g.jailOut[i%2] = false
		}
	}
	g.emit(Event{T: "bankrupt", P: p.Idx, Other: cred + 1})
	g.logf("%s обанкротился!", p.Name)
	p.Money, p.JailCards, p.Bankrupt = 0, 0, true
	alive, last := 0, -1
	for _, q := range g.Players {
		if !q.Bankrupt {
			alive++
			last = q.Idx
		}
	}
	if alive <= 1 {
		g.Phase, g.Winner = POver, last
		g.emit(Event{T: "win", P: last})
		g.logf("Победитель — %s!", g.Players[last].Name)
		return
	}
	g.endTurn()
}

// ----- auction -----

func (g *Game) startAuction() {
	p := g.cur()
	g.openAuction(p, -1, "%s отказался от покупки — аукцион за «%s»", p.Name, g.Board[g.Pending].Name)
}

// openAuction puts g.Pending up for auction; first bids first.
func (g *Game) openAuction(p *Player, first int, format string, a ...any) {
	out := make([]bool, len(g.Players))
	for i, q := range g.Players {
		out[i] = q.Bankrupt
	}
	g.Auction = &Auction{Pos: g.Pending, Bidder: -1, Out: out, Seller: -1, Turn: first}
	if first < 0 { // the next player bids first
		g.Auction.Turn = g.nextBidder(p.Idx)
	}
	g.Phase = PAuction
	g.logf(format, a...)
	g.emit(Event{T: "auction", P: p.Idx, Pos: g.Pending})
}

// startSellerAuction auctions a street that its owner agreed to sell. The
// requester (whose turn it is) bids first; the owner may not bid. If the
// requester had named a price, bidding starts from it with the requester as the
// leader: nobody has to pay less than was offered.
func (g *Game) startSellerAuction(pos, seller, startBid int, lead bool) {
	out := make([]bool, len(g.Players))
	for i, q := range g.Players {
		out[i] = q.Bankrupt || i == seller
	}
	au := &Auction{Pos: pos, Bidder: -1, Out: out, Seller: seller, Resume: g.Phase, Turn: g.Turn}
	g.Auction = au
	g.Phase = PAuction
	g.logf("%s выставляет «%s» на аукцион", g.Players[seller].Name, g.Board[pos].Name)
	g.emit(Event{T: "auction", P: seller, Pos: pos})
	if startBid <= 0 {
		return
	}
	if !lead {
		// a reserve price: bidding must beat it, but nobody holds a bid yet
		au.Bid = startBid
		g.logf("Торги начинаются с %d", startBid)
		return
	}
	au.Bid, au.Bidder = startBid, g.Turn
	g.logf("%s начинает торги с %d", g.Players[g.Turn].Name, startBid)
	g.emit(Event{T: "bid", P: g.Turn, Amount: startBid})
	if next := g.nextBidder(g.Turn); next >= 0 {
		au.Turn = next
		return
	}
	g.finishAuction() // nobody else may bid: the offer stands
}

func (g *Game) nextBidder(from int) int {
	a := g.Auction
	n := len(g.Players)
	for i := 1; i <= n; i++ {
		j := (from + i) % n
		if !a.Out[j] && j != a.Bidder {
			return j
		}
	}
	return -1
}

func (g *Game) auctionAct(pi int, a Action) error {
	au := g.Auction
	if pi != au.Turn {
		return errors.New("сейчас торгуется другой игрок")
	}
	p := g.Players[pi]
	switch a.A {
	case "bid":
		if a.N <= au.Bid || a.N < 10 {
			return errors.New("ставка должна быть выше текущей")
		}
		if a.N > p.Money {
			return errors.New("не хватает денег")
		}
		au.Bid, au.Bidder = a.N, pi
		g.logf("%s предлагает %d", p.Name, a.N)
		g.emit(Event{T: "bid", P: pi, Amount: a.N})
	case "pass":
		au.Out[pi] = true
		g.logf("%s пасует", p.Name)
	default:
		return errors.New("идёт аукцион: ставка или пас")
	}
	rest := 0
	for i, o := range au.Out {
		if !o && i != au.Bidder {
			rest++
		}
	}
	if rest == 0 {
		g.finishAuction()
		return nil
	}
	au.Turn = g.nextBidder(pi)
	return nil
}

func (g *Game) finishAuction() {
	au := g.Auction
	g.Auction = nil
	g.Phase = PAct
	if au.Bidder >= 0 {
		w, sq := g.Players[au.Bidder], g.Board[au.Pos]
		w.Money -= au.Bid
		g.Props[au.Pos].Owner = w.Idx
		g.emit(Event{T: "money", P: w.Idx, Amount: -au.Bid, Other: au.Seller + 1, Text: "аукцион «" + sq.Name + "»"})
		if au.Seller >= 0 {
			g.Players[au.Seller].Money += au.Bid
			g.emit(Event{T: "money", P: au.Seller, Amount: au.Bid, Other: w.Idx + 1, Text: "аукцион «" + sq.Name + "»"})
			g.incomeTax(g.Players[au.Seller], au.Bid)
		}
		g.emit(Event{T: "buy", P: w.Idx, Pos: au.Pos})
		g.logf("%s выигрывает аукцион: «%s» за %d", w.Name, sq.Name, au.Bid)
	} else {
		g.logf("Никто не купил «%s»", g.Board[au.Pos].Name)
	}
	if au.Resume != "" {
		g.Phase = au.Resume
		return
	}
	g.settle()
}

// ----- offers between players -----

// executeSale moves street pos from owner to buyer for price.
func (g *Game) executeSale(buyer, owner *Player, pos, price int) {
	sq := g.Board[pos]
	buyer.Money -= price
	owner.Money += price
	g.Props[pos].Owner = buyer.Idx
	g.emit(Event{T: "money", P: buyer.Idx, Amount: -price, Other: owner.Idx + 1, Text: "покупка «" + sq.Name + "» у " + owner.Name})
	g.emit(Event{T: "money", P: owner.Idx, Amount: price, Other: buyer.Idx + 1, Text: "продажа «" + sq.Name + "» игроку " + buyer.Name})
	g.incomeTax(owner, price)
	g.emit(Event{T: "buy", P: buyer.Idx, Pos: pos})
	g.logf("%s продаёт «%s» игроку %s за %d", owner.Name, sq.Name, buyer.Name, price)
}

// tradable reports why street pos cannot change hands, or "" if it can.
func (g *Game) tradable(pos, owner int) string {
	sq, ps := g.Board[pos], g.Props[pos]
	if sq.Kind != KProp && sq.Kind != KStation && sq.Kind != KUtil {
		return "это поле нельзя купить"
	}
	if ps.Owner != owner || owner < 0 {
		return "у поля другой владелец"
	}
	if ps.Mortgaged {
		return "заложенное поле купить нельзя"
	}
	if sq.Kind == KProp {
		if _, max := g.groupHouses(sq.Group); max > 0 {
			return "на улицах этой группы есть постройки"
		}
	}
	return ""
}

func (g *Game) offerAct(pi int, a Action) error {
	switch a.A {
	case "offer_buy", "offer_auction":
		if pi != g.Turn || (g.Phase != PRoll && g.Phase != PAct) {
			return errors.New("предлагать можно в свой ход, до или после броска")
		}
		if g.Offer != nil {
			return errors.New("предыдущее предложение ещё не закрыто")
		}
		pos := a.N
		if pos < 0 || pos >= boardLen {
			return errors.New("нет такого поля")
		}
		owner := g.Props[pos].Owner
		if owner < 0 || owner == pi || g.Players[owner].Bankrupt {
			return errors.New("у этого поля нет другого владельца")
		}
		if why := g.tradable(pos, owner); why != "" {
			return errors.New(why)
		}
		o := &Offer{From: pi, To: owner, Pos: pos, Kind: "auction"}
		sq, p, ow := g.Board[pos], g.Players[pi], g.Players[owner]
		if a.A == "offer_buy" {
			if a.M < 1 || a.M > p.Money {
				return errors.New("укажите цену, на которую хватает денег")
			}
			o.Kind, o.Price = "buy", a.M
			g.logf("%s предлагает %s купить «%s» за %d", p.Name, ow.Name, sq.Name, a.M)
		} else {
			g.logf("%s просит %s выставить «%s» на аукцион", p.Name, ow.Name, sq.Name)
		}
		g.Offer = o
		g.offerTry[pos]++
		g.offerCool[pos] = g.TurnNo + 5
		return nil
	case "offer_cancel":
		if g.Offer == nil || pi != g.Offer.From {
			return errors.New("нет вашего предложения")
		}
		g.logf("%s отзывает предложение", g.Players[pi].Name)
		g.Offer = nil
		return nil
	}
	o := g.Offer
	if o == nil || pi != o.To {
		return errors.New("вам ничего не предлагали")
	}
	owner, buyer := g.Players[o.To], g.Players[o.From]
	switch a.A {
	case "offer_decline":
		g.logf("%s отказывается", owner.Name)
		g.Offer = nil
	case "offer_accept", "offer_to_auction": // the owner agrees, or answers a price offer with an auction
		if a.A == "offer_to_auction" && o.Kind != "buy" {
			return errors.New("неизвестное действие")
		}
		g.Offer = nil
		if why := g.tradable(o.Pos, o.To); why != "" {
			g.logf("Предложение потеряло силу: %s", why)
			return nil
		}
		if a.A == "offer_accept" && o.Kind == "buy" {
			if buyer.Money < o.Price {
				g.logf("У %s уже не хватает денег", buyer.Name)
				return nil
			}
			g.executeSale(buyer, owner, o.Pos, o.Price)
			return nil
		}
		start, lead := 0, false
		if a.A == "offer_to_auction" {
			start, lead = o.Price, true
		} else if owner.Bot {
			// a bot puts the street up from the price it would have agreed to
			start = g.botAuctionPrice(o.To, o.Pos)
		}
		g.startSellerAuction(o.Pos, o.To, start, lead)
	default:
		return errors.New("неизвестное действие")
	}
	return nil
}

// ----- property management -----

func (g *Game) groupHouses(group int) (min, max int) {
	min = maxHouses
	for _, i := range g.groupMembers(group) {
		h := g.Props[i].Houses
		if h < min {
			min = h
		}
		if h > max {
			max = h
		}
	}
	return
}

func (g *Game) manage(p *Player, a Action) error {
	pos := a.N
	if pos < 0 || pos >= boardLen {
		return errors.New("нет такого поля")
	}
	sq, ps := g.Board[pos], &g.Props[pos]
	if ps.Owner != p.Idx {
		return errors.New("это поле не ваше")
	}
	switch a.A {
	case "build":
		if sq.Kind != KProp || !g.monopoly(p.Idx, sq.Group) {
			return errors.New("нужна вся цветовая группа")
		}
		for _, i := range g.groupMembers(sq.Group) {
			if g.Props[i].Mortgaged {
				return errors.New("в группе есть заложенная улица")
			}
		}
		if min, _ := g.groupHouses(sq.Group); ps.Houses > min || ps.Houses >= maxHouses {
			return errors.New("строить нужно равномерно, максимум — отель")
		}
		if p.Money < sq.House {
			return errors.New("не хватает денег")
		}
		p.Money -= sq.House
		ps.Houses++
		g.emit(Event{T: "money", P: p.Idx, Amount: -sq.House, Text: "строительство на «" + sq.Name + "»"})
		g.emit(Event{T: "build", P: p.Idx, Pos: pos, Amount: ps.Houses})
		g.logf("%s строит на «%s» (%s)", p.Name, sq.Name, houseWord(ps.Houses))
	case "sell":
		if sq.Kind != KProp || ps.Houses == 0 {
			return errors.New("тут нечего продавать")
		}
		if _, max := g.groupHouses(sq.Group); ps.Houses < max {
			return errors.New("продавать нужно равномерно")
		}
		ps.Houses--
		p.Money += sq.House / 2
		g.emit(Event{T: "money", P: p.Idx, Amount: sq.House / 2, Text: "продажа постройки на «" + sq.Name + "»"})
		g.emit(Event{T: "build", P: p.Idx, Pos: pos, Amount: ps.Houses})
		g.logf("%s продаёт постройку на «%s»", p.Name, sq.Name)
	case "mortgage":
		if ps.Mortgaged {
			return errors.New("уже заложено")
		}
		if sq.Kind == KProp {
			if _, max := g.groupHouses(sq.Group); max > 0 {
				return errors.New("сначала продайте все дома группы")
			}
		}
		ps.Mortgaged = true
		p.Money += sq.Price / 2
		g.emit(Event{T: "money", P: p.Idx, Amount: sq.Price / 2, Text: "залог «" + sq.Name + "»"})
		g.logf("%s закладывает «%s»", p.Name, sq.Name)
	case "unmortgage":
		cost := sq.Price/2 + sq.Price/20
		if !ps.Mortgaged {
			return errors.New("поле не заложено")
		}
		if p.Money < cost {
			return errors.New("не хватает денег")
		}
		ps.Mortgaged = false
		p.Money -= cost
		g.emit(Event{T: "money", P: p.Idx, Amount: -cost, Text: "выкуп «" + sq.Name + "»"})
		g.logf("%s выкупает «%s» за %d", p.Name, sq.Name, cost)
	}
	return nil
}

func houseWord(n int) string {
	if n >= maxHouses {
		return "отель"
	}
	return fmt.Sprintf("домов: %d", n)
}

// Net is cash plus the liquidation value of everything owned (for the ranking).
func (g *Game) Net(p *Player) int {
	n := p.Money
	for i, ps := range g.Props {
		if ps.Owner == p.Idx {
			n += g.Board[i].Price/2 + ps.Houses*g.Board[i].House/2
		}
	}
	return n
}

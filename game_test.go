package main

import (
	"math/rand"
	"testing"
)

func newTestGame(n int, seed int64) *Game {
	var ps []*Player
	for i := 0; i < n; i++ {
		ps = append(ps, &Player{Name: string(rune('A' + i)), Color: i})
	}
	g := NewGame(Themes()[0].Build(), ps, rand.New(rand.NewSource(seed)))
	g.Turn = 0
	return g
}

func TestBoardShape(t *testing.T) {
	for _, th := range Themes() {
		b := th.Build()
		if len(b) != boardLen {
			t.Fatalf("%s: %d squares", th.ID, len(b))
		}
		count := map[Kind]int{}
		groups := map[int]int{}
		for _, sq := range b {
			count[sq.Kind]++
			if sq.Name == "" {
				t.Fatalf("%s: unnamed square", th.ID)
			}
			if sq.Kind == KProp {
				groups[sq.Group]++
			}
		}
		if count[KProp] != 22 || count[KStation] != 4 || count[KUtil] != 2 || count[KChance] != 3 || count[KChest] != 3 {
			t.Fatalf("%s: bad counts %v", th.ID, count)
		}
		if len(groups) != 8 {
			t.Fatalf("%s: groups %v", th.ID, groups)
		}
	}
}

func own(g *Game, pi int, pos ...int) {
	for _, p := range pos {
		g.Props[p].Owner = pi
	}
}

func TestRentAndMonopoly(t *testing.T) {
	g := newTestGame(2, 1)
	own(g, 1, 1)
	if r := g.rent(1, 0); r != 2 {
		t.Fatalf("rent %d", r)
	}
	own(g, 1, 3)
	if r := g.rent(1, 0); r != 4 {
		t.Fatalf("monopoly rent %d", r)
	}
	own(g, 1, 5, 15, 25)
	if r := g.rent(15, 0); r != 100 {
		t.Fatalf("3 stations rent %d", r)
	}
	own(g, 1, 12, 28)
	g.Dice = [2]int{3, 4}
	if r := g.rent(12, 0); r != 70 {
		t.Fatalf("utility rent %d", r)
	}
}

func TestBuyAndPayRent(t *testing.T) {
	g := newTestGame(2, 1)
	a, b := g.Players[0], g.Players[1]
	a.Pos = 0
	g.Phase = PRoll
	g.moveBy(a, 1)
	g.settle()
	if g.Phase != PBuy {
		t.Fatalf("phase %s", g.Phase)
	}
	if err := g.Do(0, Action{A: "buy"}); err != nil {
		t.Fatal(err)
	}
	if a.Money != startMoney-60 || g.Props[1].Owner != 0 {
		t.Fatal("purchase failed")
	}
	b.Pos = 0
	g.Turn, g.Phase = 1, PRoll
	g.moveBy(b, 1)
	if b.Money != startMoney-2 || a.Money != startMoney-60+2 {
		t.Fatalf("rent: %d %d", a.Money, b.Money)
	}
}

func TestPassingGo(t *testing.T) {
	g := newTestGame(2, 1)
	p := g.Players[0]
	p.Pos = 38 // tax square is 38, we land on 2 (chest) — only check salary on the way
	g.moveBy(p, 4)
	if p.Money < startMoney+goSalary-200 { // chest may cost up to 100
		t.Fatalf("no salary: %d", p.Money)
	}
}

func TestBuildEvenly(t *testing.T) {
	g := newTestGame(2, 1)
	own(g, 0, 1, 3)
	g.Phase = PAct
	if err := g.Do(0, Action{A: "build", N: 1}); err != nil {
		t.Fatal(err)
	}
	if err := g.Do(0, Action{A: "build", N: 1}); err == nil {
		t.Fatal("uneven build allowed")
	}
	if err := g.Do(0, Action{A: "build", N: 3}); err != nil {
		t.Fatal(err)
	}
	if g.rent(1, 0) != 10 {
		t.Fatalf("rent with house %d", g.rent(1, 0))
	}
	if err := g.Do(0, Action{A: "mortgage", N: 1}); err == nil {
		t.Fatal("mortgaged a street with houses in group")
	}
	if err := g.Do(0, Action{A: "sell", N: 3}); err != nil {
		t.Fatal(err)
	}
	// houses on 1 is 1, on 3 is 0 → selling on 3 again is impossible, on 1 fine
	if err := g.Do(0, Action{A: "sell", N: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestJail(t *testing.T) {
	g := newTestGame(2, 1)
	p := g.Players[0]
	g.goJail(p)
	g.endTurn()
	g.endTurn()
	if !p.InJail || p.Pos != jailPos || g.Turn != 0 {
		t.Fatal("jail state")
	}
	if err := g.Do(0, Action{A: "jailpay"}); err != nil {
		t.Fatal(err)
	}
	if p.InJail || p.Money != startMoney-jailFine {
		t.Fatal("pay fine")
	}
}

func TestDebtAndBankruptcy(t *testing.T) {
	g := newTestGame(2, 1)
	a, b := g.Players[0], g.Players[1]
	own(g, 1, 39)
	a.Money, a.Pos = 10, 38
	g.Phase = PRoll
	g.moveBy(a, 1) // Boardwalk, rent 50
	if g.Phase != PDebt {
		t.Fatalf("phase %s", g.Phase)
	}
	if err := g.Do(0, Action{A: "paydebt"}); err == nil {
		t.Fatal("paid without money")
	}
	own(g, 0, 1)
	if err := g.Do(0, Action{A: "mortgage", N: 1}); err != nil {
		t.Fatal(err)
	}
	if err := g.Do(0, Action{A: "bankrupt"}); err != nil {
		t.Fatal(err)
	}
	if g.Phase != POver || g.Winner != 1 || g.Props[1].Owner != 1 || !g.Props[1].Mortgaged {
		t.Fatalf("bankruptcy: phase %s winner %d", g.Phase, g.Winner)
	}
	_ = b
}

func TestAuction(t *testing.T) {
	g := newTestGame(3, 1)
	g.Players[0].Pos = 0
	g.moveBy(g.Players[0], 1)
	if err := g.Do(0, Action{A: "decline"}); err != nil {
		t.Fatal(err)
	}
	if g.Phase != PAuction || g.Auction.Turn != 1 {
		t.Fatalf("auction start: %s turn %d", g.Phase, g.Auction.Turn)
	}
	steps := []struct {
		pi int
		a  Action
	}{{1, Action{A: "bid", N: 30}}, {2, Action{A: "bid", N: 40}}, {0, Action{A: "pass"}}, {1, Action{A: "pass"}}}
	for _, s := range steps {
		if err := g.Do(s.pi, s.a); err != nil {
			t.Fatalf("%v: %v", s, err)
		}
	}
	if g.Phase == PAuction || g.Props[1].Owner != 2 || g.Players[2].Money != startMoney-40 {
		t.Fatalf("auction result: owner %d", g.Props[1].Owner)
	}
}

// Bots play whole games on every board: no deadlocks, money is conserved sanely.
func TestBotsFinishGames(t *testing.T) {
	for seed := int64(1); seed <= 40; seed++ {
		th := Themes()[int(seed)%4]
		var ps []*Player
		for i := 0; i < 4; i++ {
			ps = append(ps, &Player{Name: string(rune('A' + i)), Color: i, Bot: true})
		}
		g := NewGame(th.Build(), ps, rand.New(rand.NewSource(seed)))
		if seed%2 == 0 {
			g.Rules = Rules{AuctionAll: true, WealthTax: true, IncomeTax: true}
		}
		steps := 0
		for g.Phase != POver && steps < 20000 {
			if !g.BotStep() {
				t.Fatalf("seed %d: bot stuck in phase %s (turn %d)", seed, g.Phase, g.Turn)
			}
			g.TakeEvents()
			steps++
		}
		for _, p := range g.Players {
			if p.Money < 0 {
				t.Fatalf("seed %d: negative money", seed)
			}
		}
	}
}

func TestHistory(t *testing.T) {
	g := newTestGame(2, 1)
	a := g.Players[0]
	a.Pos = 0
	g.moveBy(a, 1)
	if err := g.Do(0, Action{A: "buy"}); err != nil {
		t.Fatal(err)
	}
	h := g.History(0)
	if len(h) != 1 || h[0].Amount != -60 || h[0].Bal != startMoney-60 || h[0].Why == "" {
		t.Fatalf("history: %+v", h)
	}
	g.Players[1].Pos = 0
	g.Turn, g.Phase = 1, PRoll
	g.moveBy(g.Players[1], 1)
	if h := g.History(0); len(h) != 2 || h[1].Amount != 2 || h[1].Other != 1 {
		t.Fatalf("rent income not recorded: %+v", h)
	}
	if len(g.History(7)) != 0 {
		t.Fatal("unknown player must have empty history")
	}
}

func TestOfferBuy(t *testing.T) {
	g := newTestGame(2, 1)
	own(g, 1, 1)
	g.Phase = PRoll
	if err := g.Do(0, Action{A: "offer_buy", N: 1, M: 100}); err != nil {
		t.Fatal(err)
	}
	if g.Actor() != 1 {
		t.Fatal("owner must answer")
	}
	if err := g.Do(0, Action{A: "roll"}); err == nil {
		t.Fatal("proposer must wait for the answer")
	}
	if err := g.Do(1, Action{A: "offer_accept"}); err != nil {
		t.Fatal(err)
	}
	if g.Props[1].Owner != 0 || g.Players[0].Money != startMoney-100 || g.Players[1].Money != startMoney+100 {
		t.Fatalf("sale: owner %d money %d/%d", g.Props[1].Owner, g.Players[0].Money, g.Players[1].Money)
	}
	if g.Phase != PRoll {
		t.Fatalf("turn phase must stay, got %s", g.Phase)
	}
	// payments to player 0 are not shown as bank payments
	if h := g.History(0); h[0].Other != 1 {
		t.Fatalf("counterparty: %+v", h)
	}
	if h := g.History(1); h[0].Other != 0 {
		t.Fatalf("counterparty of player 0 lost: %+v", h)
	}
}

func TestOfferAuctionKeepsPhase(t *testing.T) {
	g := newTestGame(3, 1)
	own(g, 2, 1)
	g.Phase = PRoll
	if err := g.Do(0, Action{A: "offer_auction", N: 1}); err != nil {
		t.Fatal(err)
	}
	if err := g.Do(2, Action{A: "offer_accept"}); err != nil {
		t.Fatal(err)
	}
	if g.Phase != PAuction || g.Auction.Turn != 0 || !g.Auction.Out[2] {
		t.Fatalf("auction start: %+v", g.Auction)
	}
	if err := g.Do(0, Action{A: "bid", N: 80}); err != nil {
		t.Fatal(err)
	}
	if err := g.Do(1, Action{A: "pass"}); err != nil {
		t.Fatal(err)
	}
	if g.Props[1].Owner != 0 || g.Players[2].Money != startMoney+80 || g.Phase != PRoll {
		t.Fatalf("result: owner %d seller money %d phase %s", g.Props[1].Owner, g.Players[2].Money, g.Phase)
	}
}

func TestOfferRules(t *testing.T) {
	g := newTestGame(2, 1)
	own(g, 1, 1, 3)
	g.Props[1].Houses = 1
	g.Phase = PRoll
	if g.Do(0, Action{A: "offer_buy", N: 1, M: 100}) == nil {
		t.Fatal("street with houses sold")
	}
	g.Props[1].Houses = 0
	g.Props[1].Mortgaged = true
	if g.Do(0, Action{A: "offer_buy", N: 1, M: 100}) == nil {
		t.Fatal("mortgaged street sold")
	}
	g.Props[1].Mortgaged = false
	if g.Do(0, Action{A: "offer_buy", N: 1, M: startMoney + 1}) == nil {
		t.Fatal("offer above cash")
	}
	if g.Do(0, Action{A: "offer_buy", N: 0, M: 10}) == nil {
		t.Fatal("offer for a corner")
	}
	if err := g.Do(0, Action{A: "offer_buy", N: 3, M: 100}); err != nil {
		t.Fatal(err)
	}
	if err := g.Do(0, Action{A: "offer_cancel"}); err != nil || g.Offer != nil {
		t.Fatal("cancel")
	}
}

func TestBotValuesMonopoly(t *testing.T) {
	g := newTestGame(2, 1)
	own(g, 0, 1)
	if g.propValue(0, 3) < 2*float64(g.Board[3].Price) {
		t.Fatal("completing a set must be valued highly")
	}
	own(g, 1, 11, 13) // opponent needs only square 14 for the pink set
	if g.propValue(0, 14) < 1.5*float64(g.Board[14].Price) {
		t.Fatal("blocking an opponent's set must be valued highly")
	}
}

// Bots of different games must still finish and sometimes trade.
func TestBotsTradeAndFinish(t *testing.T) {
	finished, offers := 0, 0
	for seed := int64(1); seed <= 60; seed++ {
		var ps []*Player
		for i := 0; i < 4; i++ {
			ps = append(ps, &Player{Name: string(rune('A' + i)), Color: i, Bot: true})
		}
		g := NewGame(Themes()[int(seed)%4].Build(), ps, rand.New(rand.NewSource(seed)))
		for steps := 0; g.Phase != POver && steps < 40000; steps++ {
			if g.Offer != nil {
				offers++
			}
			if !g.BotStep() {
				t.Fatalf("seed %d: stuck in %s (actor %d)", seed, g.Phase, g.Actor())
			}
			g.TakeEvents()
		}
		if g.Phase == POver {
			finished++
		}
	}
	t.Logf("finished %d/60, offers seen %d", finished, offers)
	if finished < 30 {
		t.Fatalf("only %d of 60 games finished", finished)
	}
}

// The owner's "auction instead" answer must not be confused with the proposer's request.
func TestOwnerAnswersWithAuction(t *testing.T) {
	g := newTestGame(3, 1)
	own(g, 2, 1)
	g.Phase = PAct
	if err := g.Do(0, Action{A: "offer_buy", N: 1, M: 70}); err != nil {
		t.Fatal(err)
	}
	if err := g.Do(2, Action{A: "offer_to_auction"}); err != nil {
		t.Fatal(err)
	}
	if g.Phase != PAuction || g.Auction.Seller != 2 || g.Offer != nil {
		t.Fatalf("no auction: phase %s", g.Phase)
	}
}

func TestBotAuctionThreshold(t *testing.T) {
	g0 := newTestGame(3, 1)
	g0.Players[1].Bot = true
	own(g0, 1, 1)
	thr := g0.botAuctionPrice(1, 1)
	if thr < 60 {
		t.Fatalf("threshold %d below the list price", thr)
	}
	for _, tc := range []struct {
		price   int
		auction bool
	}{{thr - 1, false}, {thr, true}, {thr * 10, true}} {
		g := newTestGame(3, 1)
		g.Players[1].Bot = true
		g.Players[0].Money = 100000
		own(g, 1, 1)
		g.Phase = PAct
		if err := g.Do(0, Action{A: "offer_buy", N: 1, M: tc.price}); err != nil {
			t.Fatal(err)
		}
		if !g.BotStep() {
			t.Fatal("bot did not answer")
		}
		if g.Props[1].Owner != 1 {
			t.Fatalf("price %d: the bot sold the street", tc.price)
		}
		if got := g.Phase == PAuction; got != tc.auction {
			t.Fatalf("price %d (threshold %d): auction=%v, want %v", tc.price, thr, got, tc.auction)
		}
	}
}

func TestAuctionHints(t *testing.T) {
	g := newTestGame(3, 1)
	g.Players[1].Bot = true
	own(g, 1, 1, 6)
	own(g, 0, 3) // a human's street is not listed
	g.Props[6].Mortgaged = true
	h := g.AuctionHints()
	if len(h) != 1 || h[1] != g.botAuctionPrice(1, 1) {
		t.Fatalf("hints %v", h)
	}
	g.Turn = 1 // a bot is on turn: nothing is shown
	if g.AuctionHints() != nil {
		t.Fatal("hints during a bot's turn")
	}
}

func TestAuctionStartsFromOffer(t *testing.T) {
	g := newTestGame(3, 1)
	own(g, 2, 1)
	g.Phase = PAct
	g.Do(0, Action{A: "offer_buy", N: 1, M: 90})
	if err := g.Do(2, Action{A: "offer_to_auction"}); err != nil {
		t.Fatal(err)
	}
	au := g.Auction
	if g.Phase != PAuction || au.Bid != 90 || au.Bidder != 0 || au.Turn != 1 {
		t.Fatalf("auction: %+v", au)
	}
	if err := g.Do(1, Action{A: "bid", N: 90}); err == nil {
		t.Fatal("a bid equal to the offer was accepted")
	}
	if err := g.Do(1, Action{A: "pass"}); err != nil {
		t.Fatal(err)
	}
	if g.Props[1].Owner != 0 || g.Players[0].Money != startMoney-90 || g.Players[2].Money != startMoney+90 || g.Phase != PAct {
		t.Fatalf("offer must stand: owner %d money %d/%d phase %s", g.Props[1].Owner, g.Players[0].Money, g.Players[2].Money, g.Phase)
	}
	// only the owner could bid otherwise: the offer is settled at once
	g2 := newTestGame(2, 1)
	own(g2, 1, 1)
	g2.Phase = PRoll
	g2.Do(0, Action{A: "offer_buy", N: 1, M: 80})
	g2.Do(1, Action{A: "offer_to_auction"})
	if g2.Phase != PRoll || g2.Props[1].Owner != 0 || g2.Auction != nil {
		t.Fatalf("two players: phase %s owner %d", g2.Phase, g2.Props[1].Owner)
	}
}

func TestRiggedDouble(t *testing.T) {
	g := newTestGame(2, 1)
	g.Players[0].Name = "kfc_ven0m"
	if err := g.Do(0, Action{A: "rigged"}); err != nil {
		t.Fatal(err)
	}
	if g.Dice[0] != g.Dice[1] {
		t.Fatalf("not a double: %v", g.Dice)
	}
	// in jail the double must free the player
	g = newTestGame(2, 2)
	g.Players[0].Name = "kfc_ven0m"
	g.goJail(g.Players[0])
	g.Phase, g.Turn = PRoll, 0
	if err := g.Do(0, Action{A: "rigged"}); err != nil {
		t.Fatal(err)
	}
	if g.Players[0].InJail {
		t.Fatal("still in jail after a rigged double")
	}
	g = newTestGame(2, 3)
	if err := g.Do(0, Action{A: "rigged"}); err == nil {
		t.Fatal("rigged must be refused for other players")
	}
}

func TestCardShortestAndStation(t *testing.T) {
	g := newTestGame(2, 5)
	p := g.Players[0]
	p.Pos = 26
	g.moveShortest(p, 24)
	if p.Pos != 24 || p.Money != startMoney {
		t.Fatalf("back move: pos %d money %d", p.Pos, p.Money)
	}
	g = newTestGame(2, 5)
	p = g.Players[0]
	p.Pos = 36
	g.moveShortest(p, 3) // 7 forward over the start beats 33 back
	if p.Pos != 3 || p.Money != startMoney+goSalary {
		t.Fatalf("forward over start: pos %d money %d", p.Pos, p.Money)
	}
	g = newTestGame(2, 5)
	p = g.Players[0]
	p.Pos = 7
	g.moveTo(p, g.nearest(p.Pos, KStation))
	if p.Pos != 15 {
		t.Fatalf("station: %d", p.Pos)
	}
}

func TestBotAuctionReserve(t *testing.T) {
	g := newTestGame(2, 6)
	g.Players[1].Bot = true
	g.Props[1].Owner = 1
	g.Phase, g.Turn = PAct, 0
	if err := g.Do(0, Action{A: "offer_auction", N: 1}); err != nil {
		t.Fatal(err)
	}
	if err := g.Do(1, Action{A: "offer_accept"}); err != nil {
		t.Fatal(err)
	}
	if want := g.botAuctionPrice(1, 1); g.Auction == nil || g.Auction.Bid != want || g.Auction.Bidder != -1 {
		t.Fatalf("auction %+v, want reserve %d", g.Auction, want)
	}
}

func TestNoRentToJailedOwner(t *testing.T) {
	g := newTestGame(2, 7)
	g.Props[1].Owner = 1
	g.Players[1].InJail = true
	g.Players[0].Pos = 1
	g.landOn(g.Players[0])
	if g.Players[0].Money != startMoney || g.Players[1].Money != startMoney {
		t.Fatal("rent was paid to a jailed owner")
	}
	g.Players[1].InJail = false
	g.landOn(g.Players[0])
	if g.Players[0].Money >= startMoney {
		t.Fatal("rent must be paid when the owner is free")
	}
}

func TestJailThirdMissIsFree(t *testing.T) {
	for seed := int64(1); seed < 200; seed++ {
		g := newTestGame(2, seed)
		p := g.Players[0]
		g.goJail(p)
		g.Phase, g.Turn, p.JailTurns = PRoll, 0, 2
		g.roll(p, false)
		if g.Dice[0] == g.Dice[1] {
			continue
		}
		if p.InJail || p.Money != startMoney {
			t.Fatalf("seed %d: injail=%v money=%d", seed, p.InJail, p.Money)
		}
		return
	}
	t.Fatal("no non-double roll found")
}

func TestAuctionAllMode(t *testing.T) {
	g := newTestGame(2, 8)
	g.Rules.AuctionAll = true
	g.Phase, g.Turn = PRoll, 0
	g.moveBy(g.Players[0], 1)
	if g.Phase != PAuction || g.Auction == nil || g.Auction.Pos != 1 || g.Auction.Turn != 0 {
		t.Fatalf("expected an auction on square 1, got phase %s", g.Phase)
	}
	if err := g.Do(0, Action{A: "bid", N: 10}); err != nil {
		t.Fatal(err)
	}
	if err := g.Do(1, Action{A: "pass"}); err != nil {
		t.Fatal(err)
	}
	if g.Props[1].Owner != 0 || g.Players[0].Money != startMoney-10 {
		t.Fatal("the winner must pay the bid")
	}
}

func TestIncomeTax(t *testing.T) {
	for _, c := range []struct{ bal, want int }{{1500, 13}, {2000, 13}, {2001, 15}, {5001, 18}} {
		if got := incomeTaxRate(c.bal); got != c.want {
			t.Fatalf("rate(%d)=%d want %d", c.bal, got, c.want)
		}
	}
	g := newTestGame(2, 9)
	g.Rules.IncomeTax = true
	g.salary(g.Players[0])
	if g.Players[0].Money != startMoney+goSalary-26 {
		t.Fatalf("salary after tax: %d", g.Players[0].Money)
	}
	g.Props[1].Owner = 1
	g.Players[0].Pos = 1
	before := g.Players[1].Money
	g.landOn(g.Players[0])
	rent := g.rent(1, 0)
	if g.Players[1].Money != before+rent-(rent*13+50)/100 {
		t.Fatalf("rent after tax: %d", g.Players[1].Money-before)
	}
}

func TestWealthTax(t *testing.T) {
	g := newTestGame(2, 10)
	g.Rules.WealthTax = true
	g.Players[0].Money = 3000
	g.Turn, g.Phase = 1, PAct
	for i := 0; i < 2*wealthTaxEvery; i++ {
		g.endTurn()
		if g.Turn == 0 && g.Round == wealthTaxEvery {
			break
		}
	}
	if g.Round != wealthTaxEvery || g.Players[0].Money != 2700 {
		t.Fatalf("round %d, richest has %d", g.Round, g.Players[0].Money)
	}
	if g.Phase != PRoll {
		t.Fatalf("phase %s", g.Phase)
	}
}

func TestSkipLeavesStreetFree(t *testing.T) {
	g := newTestGame(2, 11)
	g.moveBy(g.Players[0], 1)
	if g.Phase != PBuy {
		t.Fatal("expected a buy phase")
	}
	if err := g.Do(0, Action{A: "skip"}); err != nil {
		t.Fatal(err)
	}
	if g.Props[1].Owner != -1 || g.Phase != PAct || g.Players[0].Money != startMoney {
		t.Fatalf("skip: owner %d phase %s", g.Props[1].Owner, g.Phase)
	}
}

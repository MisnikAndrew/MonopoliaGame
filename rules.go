package main

import "strings"

// Rules are the optional game modes chosen by the host before the start.
type Rules struct {
	AuctionAll bool `json:"auctionAll"` // a free street always goes to auction, from 10
	WealthTax  bool `json:"wealthTax"`  // every 5 rounds the richest pays 10% of their fortune
	IncomeTax  bool `json:"incomeTax"`  // the bank takes 13/15/18% of every income
}

const (
	wealthTaxEvery = 5  // rounds
	wealthTaxPct   = 10 // % of the fortune
)

// Set switches one mode; it returns false for an unknown id.
func (r *Rules) Set(id string, on bool) bool {
	switch id {
	case "auctionAll":
		r.AuctionAll = on
	case "wealthTax":
		r.WealthTax = on
	case "incomeTax":
		r.IncomeTax = on
	default:
		return false
	}
	return true
}

// SetRules fixes the modes of a new game and announces them in the log.
func (g *Game) SetRules(r Rules) {
	g.Rules = r
	var on []string
	if r.AuctionAll {
		on = append(on, "аукцион на каждую клетку")
	}
	if r.WealthTax {
		on = append(on, "налог на богатых")
	}
	if r.IncomeTax {
		on = append(on, "налог на доход")
	}
	if len(on) > 0 {
		g.logf("Режимы: %s", strings.Join(on, ", "))
	}
}

// incomeTaxRate is the share in percent that the bank takes from a player who
// held bal before the income arrived.
func incomeTaxRate(bal int) int {
	switch {
	case bal > 5000:
		return 18
	case bal > 2000:
		return 15
	}
	return 13
}

// incomeTax takes the bank's share of amt, which p has just received.
func (g *Game) incomeTax(p *Player, amt int) {
	if !g.Rules.IncomeTax || amt <= 0 {
		return
	}
	rate := incomeTaxRate(p.Money - amt)
	tax := (amt*rate + 50) / 100
	if tax <= 0 {
		return
	}
	p.Money -= tax
	g.emit(Event{T: "money", P: p.Idx, Amount: -tax, Text: "налог на доход " + itoa(rate) + "%"})
}

// worth is the fortune of p: cash, streets at their price (half for mortgaged
// ones) and buildings at their cost.
func (g *Game) worth(p *Player) int {
	w := p.Money
	for i, ps := range g.Props {
		if ps.Owner != p.Idx {
			continue
		}
		sq := g.Board[i]
		if ps.Mortgaged {
			w += sq.Price / 2
		} else {
			w += sq.Price
		}
		w += ps.Houses * sq.House
	}
	return w
}

// roundDone counts a finished round and, every few rounds, marks the richest
// player for the wealth tax, which they pay when their turn begins.
func (g *Game) roundDone() {
	g.Round++
	if !g.Rules.WealthTax || g.Round%wealthTaxEvery != 0 {
		return
	}
	best, bw := -1, -1
	for _, q := range g.Players {
		if w := g.worth(q); !q.Bankrupt && w > bw {
			best, bw = q.Idx, w
		}
	}
	g.wtax = best + 1
	if best >= 0 {
		g.logf("Конец %d-го круга: налог на богатых — его заплатит %s", g.Round, g.Players[best].Name)
	}
}

// chargeWealthTax is called when a turn begins.
func (g *Game) chargeWealthTax() {
	if g.wtax != g.Turn+1 {
		return
	}
	g.wtax = 0
	p := g.cur()
	tax := g.worth(p) * wealthTaxPct / 100
	if tax <= 0 {
		return
	}
	g.logf("%s — самый богатый и платит налог на богатых: %d", p.Name, tax)
	g.pay(p, tax, -1, "налог на богатых")
	if g.Phase == PDebt {
		g.Debt.Resume = PRoll
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

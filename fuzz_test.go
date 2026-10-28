package main

import (
	"math/rand"
	"testing"
)

// Random (valid or not) actions must never break the turn invariants.
func TestFuzzInvariants(t *testing.T) {
	acts := []string{"roll", "buy", "decline", "endturn", "paydebt", "bankrupt", "bid", "pass", "mortgage", "unmortgage", "build", "sell",
		"jailpay", "jailcard", "offer_buy", "offer_auction", "offer_accept", "offer_decline", "offer_to_auction", "offer_cancel", "skip"}
	for seed := int64(1); seed <= 300; seed++ {
		rng := rand.New(rand.NewSource(seed))
		var ps []*Player
		for i := 0; i < 2+int(seed)%4; i++ {
			ps = append(ps, &Player{Name: string(rune('A' + i)), Color: i})
		}
		g := NewGame(Themes()[int(seed)%4].Build(), ps, rand.New(rand.NewSource(seed)))
		if seed%2 == 0 { // every second game runs with all the modes on
			g.Rules = Rules{AuctionAll: true, WealthTax: true, IncomeTax: true}
		}
		for step := 0; step < 3000 && g.Phase != POver; step++ {
			pi := rng.Intn(len(ps))
			if rng.Intn(3) > 0 {
				pi = g.Actor()
			}
			a := Action{A: acts[rng.Intn(len(acts))], N: rng.Intn(40), M: rng.Intn(400)}
			if a.A == "roll" || a.A == "endturn" || a.A == "buy" {
				a.N = 0
			}
			before := g.Phase
			g.Do(pi, a)
			if g.Phase == POver {
				break
			}
			if g.Players[g.Turn].Bankrupt {
				t.Fatalf("seed %d step %d: bankrupt player %d keeps the turn after %s (phase %s -> %s)", seed, step, g.Turn, a.A, before, g.Phase)
			}
			if g.Phase == PDebt && (g.Debt == nil || g.Debt.P != g.Turn) {
				t.Fatalf("seed %d step %d: debt %+v does not belong to turn %d after %s", seed, step, g.Debt, g.Turn, a.A)
			}
			if g.Phase != PDebt && g.Debt != nil {
				t.Fatalf("seed %d step %d: stale debt in phase %s after %s", seed, step, g.Phase, a.A)
			}
			if g.Phase == PAuction && g.Auction == nil {
				t.Fatalf("seed %d: auction phase without auction", seed)
			}
		}
	}
}

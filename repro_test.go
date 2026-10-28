package main

import "testing"

func TestBotAnswersStationOffer(t *testing.T) {
	for _, owned := range [][]int{{}, {5}, {5, 15}, {5, 15, 25}} {
		g := newTestGame(2, 1)
		g.Players[1].Bot = true
		own(g, 1, 35)
		own(g, 0, owned...)
		g.Phase = PAct
		if err := g.Do(0, Action{A: "offer_buy", N: 35, M: 201}); err != nil {
			t.Fatal(err)
		}
		if !g.BotStep() {
			t.Fatalf("bot did not answer, owned=%v", owned)
		}
		if g.Offer != nil && g.Phase != PAuction {
			t.Fatalf("offer still pending, owned=%v", owned)
		}
	}
}

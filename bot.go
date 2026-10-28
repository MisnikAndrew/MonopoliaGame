package main

import "math"

// Bot strategy, in short:
//   - streets are valued by how close they bring the bot to a monopoly (or
//     keep an opponent away from one), not by price alone;
//   - the cash reserve follows the real danger: the biggest rent an opponent
//     can charge right now;
//   - early on, buy everything and leave jail fast; late, when opponents have
//     built up, sit in jail and keep cash;
//   - build where an extra house adds the most rent per coin;
//   - raise money for a monopoly by mortgaging non-essential streets, and when
//     in debt give up the least valuable assets first;
//   - propose buying the last missing street of a group, and answer offers by
//     their price against the street's value to the bot.

// groupWeight reflects how often each colour group is landed on and how fast it pays back.
var groupWeight = [8]float64{0.95, 1.0, 1.1, 1.25, 1.2, 1.1, 1.05, 1.0}

// groupState counts, from pi's point of view, the streets of a group other than skip.
func (g *Game) groupState(pi, group, skip int) (mine, free int, others map[int]int) {
	others = map[int]int{}
	for _, m := range g.groupMembers(group) {
		if m == skip {
			continue
		}
		switch o := g.Props[m].Owner; {
		case o == pi:
			mine++
		case o < 0:
			free++
		default:
			others[o]++
		}
	}
	return
}

// propValue is what street pos is worth to player pi, in coins.
func (g *Game) propValue(pi, pos int) float64 {
	sq := g.Board[pos]
	v := float64(sq.Price)
	switch sq.Kind {
	case KStation:
		v *= 1 + 0.3*float64(g.countOwnedExcept(pi, KStation, pos))
	case KUtil:
		v *= 0.8
		if g.countOwnedExcept(pi, KUtil, pos) == 1 {
			v *= 1.5
		}
	case KProp:
		n := len(g.groupMembers(sq.Group))
		mine, _, others := g.groupState(pi, sq.Group, pos)
		v *= groupWeight[sq.Group]
		blocks := false
		for _, c := range others {
			if c == n-1 {
				blocks = true // an opponent owns the rest: this street stops their monopoly
			}
		}
		switch {
		case mine == n-1:
			v *= 2.4
		case blocks:
			v *= 1.8
		case len(others) == 0 && mine > 0:
			v *= 1 + 0.4*float64(mine)
		case len(others) > 0 && mine > 0:
			v *= 0.75
		case len(others) > 1:
			v *= 0.85
		}
	}
	return v
}

func (g *Game) countOwnedExcept(owner int, k Kind, skip int) (n int) {
	for i, sq := range g.Board {
		if i != skip && sq.Kind == k && g.Props[i].Owner == owner {
			n++
		}
	}
	return
}

// completesMonopoly reports whether taking pos would give pi a full set.
func (g *Game) completesMonopoly(pi, pos int) bool {
	sq := g.Board[pos]
	switch sq.Kind {
	case KProp:
		mine, free, others := g.groupState(pi, sq.Group, pos)
		return free == 0 && len(others) == 0 && mine == len(g.groupMembers(sq.Group))-1
	case KStation:
		return g.countOwnedExcept(pi, KStation, pos) == 3
	}
	return false
}

// threat is the biggest rent an opponent's street would charge pi right now.
func (g *Game) threat(pi int) int {
	t := 0
	for i, ps := range g.Props {
		if ps.Owner < 0 || ps.Owner == pi || ps.Mortgaged || g.Players[ps.Owner].Bankrupt {
			continue
		}
		r := 70
		if g.Board[i].Kind != KUtil {
			r = g.rent(i, 0)
		}
		t = max(t, r)
	}
	return t
}

func (g *Game) unownedCount() (n int) {
	for i, sq := range g.Board {
		if (sq.Kind == KProp || sq.Kind == KStation || sq.Kind == KUtil) && g.Props[i].Owner < 0 {
			n++
		}
	}
	return
}

// reserve is the cash pi tries to keep.
func (g *Game) reserve(pi int) int {
	r := 100 + g.threat(pi)/2
	if g.unownedCount() > 10 {
		r = min(r, 120) // early game: land is worth more than a cushion
	}
	return min(max(r, 100), 450)
}

func (g *Game) groupMortgaged(group int) bool {
	for _, m := range g.groupMembers(group) {
		if g.Props[m].Mortgaged {
			return true
		}
	}
	return false
}

// buildChoice picks the street where one more house adds the most rent per coin.
func (g *Game) buildChoice(pi, budget int) int {
	best, bestScore := -1, 0.0
	for pos, sq := range g.Board {
		ps := g.Props[pos]
		if sq.Kind != KProp || ps.Owner != pi || ps.Houses >= maxHouses || sq.House > budget {
			continue
		}
		if !g.monopoly(pi, sq.Group) || g.groupMortgaged(sq.Group) {
			continue
		}
		if min, _ := g.groupHouses(sq.Group); ps.Houses > min {
			continue
		}
		score := float64(sq.Rent[ps.Houses+1]-sq.Rent[ps.Houses]) / float64(sq.House) * groupWeight[sq.Group]
		if ps.Houses >= 3 {
			score *= 0.85
		}
		if score > bestScore {
			best, bestScore = pos, score
		}
	}
	return best
}

// mortgageChoice picks the street that is cheapest to lose. Without force,
// streets of a monopoly (or one step from it) are kept.
func (g *Game) mortgageChoice(pi int, force bool) int {
	best, bestVal := -1, 0.0
	for pos, sq := range g.Board {
		ps := g.Props[pos]
		if ps.Owner != pi || ps.Mortgaged || ps.Houses > 0 {
			continue
		}
		if sq.Kind == KProp {
			if _, max := g.groupHouses(sq.Group); max > 0 {
				continue
			}
			if !force {
				n := len(g.groupMembers(sq.Group))
				if mine, _, _ := g.groupState(pi, sq.Group, pos); mine >= n-1 {
					continue
				}
			}
		}
		if v := g.propValue(pi, pos); best < 0 || v < bestVal {
			best, bestVal = pos, v
		}
	}
	return best
}

// sellChoice picks the house that costs the least rent per coin to give up.
func (g *Game) sellChoice(pi int) int {
	best, bestScore := -1, 0.0
	for pos, sq := range g.Board {
		ps := g.Props[pos]
		if sq.Kind != KProp || ps.Owner != pi || ps.Houses == 0 {
			continue
		}
		if _, max := g.groupHouses(sq.Group); ps.Houses < max {
			continue
		}
		score := float64(sq.Rent[ps.Houses]-sq.Rent[ps.Houses-1]) / float64(sq.House)
		if best < 0 || score < bestScore {
			best, bestScore = pos, score
		}
	}
	return best
}

// BotStep makes one move for the player who must act, if that player is a bot.
// It returns false when there is nothing for a bot to do.
func (g *Game) BotStep() bool {
	if g.Phase == POver {
		return false
	}
	pi := g.Actor()
	p := g.Players[pi]
	if !p.Bot || p.Bankrupt {
		return false
	}
	do := func(a string, n int, m ...int) bool {
		act := Action{A: a, N: n}
		if len(m) > 0 {
			act.M = m[0]
		}
		return g.Do(pi, act) == nil
	}
	if g.Offer != nil {
		// whatever goes wrong, an unanswered offer must not freeze the game
		return g.botRespond(pi, do) || do("offer_decline", 0)
	}
	res := g.reserve(pi)

	switch g.Phase {
	case PRoll:
		if p.InJail {
			early := g.unownedCount() >= 6
			if early || g.threat(pi) < 150 { // outside is safe or there is land to grab: get out
				if p.JailCards > 0 && do("jailcard", 0) {
					return true
				}
				if p.Money > 300 && do("jailpay", 0) {
					return true
				}
			}
			// late game with built-up opponents: jail is the safest place, wait for a double
		}
		return do("roll", 0)

	case PBuy:
		sq := g.Board[g.Pending]
		val := g.propValue(pi, g.Pending)
		key := val >= 1.7*float64(sq.Price) // completes a set or stops an opponent's
		want := false
		switch {
		case key:
			want = true
		case val >= 0.9*float64(sq.Price):
			want = p.Money-sq.Price >= res
		default:
			want = p.Money-sq.Price >= res+300
		}
		if want && p.Money >= sq.Price && do("buy", 0) {
			return true
		}
		if key && p.Money < sq.Price {
			// raise the missing cash by mortgaging what we do not need
			if pos := g.mortgageChoice(pi, false); pos >= 0 && g.Board[pos].Price/2+p.Money >= sq.Price && do("mortgage", pos) {
				return true
			}
		}
		return do("decline", 0)

	case PAuction:
		au := g.Auction
		sq := g.Board[au.Pos]
		val := g.propValue(pi, au.Pos)
		budget := float64(p.Money - res/2)
		if val >= 1.7*float64(sq.Price) {
			budget = float64(p.Money) // key street: all in
		}
		limit := int(min(val, budget))
		if au.Bid < limit {
			if do("bid", min(limit, au.Bid+max(10, sq.Price/15))) {
				return true
			}
		}
		return do("pass", 0)

	case PAct:
		// 1. buy back mortgaged streets (monopolies first), keeping the reserve
		for pos, ps := range g.Props {
			if ps.Owner != pi || !ps.Mortgaged {
				continue
			}
			sq := g.Board[pos]
			cost := sq.Price/2 + sq.Price/20
			keep := res + 200
			if sq.Kind == KProp && g.monopoly(pi, sq.Group) {
				keep = res
			}
			if p.Money-cost >= keep && do("unmortgage", pos) {
				return true
			}
		}
		// 2. build
		if pos := g.buildChoice(pi, p.Money-res); pos >= 0 {
			extra := 0
			if g.Props[pos].Houses >= 3 {
				extra = 100
			}
			if p.Money-g.Board[pos].House >= res+extra && do("build", pos) {
				return true
			}
		}
		// 3. try to complete a set by buying the missing street (once per turn)
		if g.offerDone(pi) == false {
			if pos, price := g.botOffer(pi); pos >= 0 {
				g.markOffered(pi)
				if do("offer_buy", pos, price) {
					return true
				}
			}
		}
		return do("endturn", 0)

	case PDebt:
		d := g.Debt
		if p.Money >= d.Amount {
			return do("paydebt", 0)
		}
		if pos := g.mortgageChoice(pi, false); pos >= 0 && do("mortgage", pos) {
			return true
		}
		if pos := g.sellChoice(pi); pos >= 0 && do("sell", pos) {
			return true
		}
		if pos := g.mortgageChoice(pi, true); pos >= 0 && do("mortgage", pos) {
			return true
		}
		return do("bankrupt", 0)
	}
	return false
}

func (g *Game) offerDone(pi int) bool { return g.botOffered[pi] == g.TurnNo }
func (g *Game) markOffered(pi int)    { g.botOffered[pi] = g.TurnNo }

// botOffer finds the last missing street of a set and a price to try for it.
func (g *Game) botOffer(pi int) (pos, price int) {
	p := g.Players[pi]
	pos, bestW := -1, 0.0
	for cand, sq := range g.Board {
		ps := g.Props[cand]
		if ps.Owner < 0 || ps.Owner == pi || g.Players[ps.Owner].Bankrupt || g.tradable(cand, ps.Owner) != "" {
			continue
		}
		if g.offerCool[cand] > g.TurnNo || !g.completesMonopoly(pi, cand) {
			continue
		}
		w := 1.0
		if sq.Kind == KProp {
			w = groupWeight[sq.Group]
		}
		offer := int(float64(sq.Price)*(1.6+0.5*float64(g.offerTry[cand]))) / 10 * 10
		offer = min(offer, (p.Money-100)/10*10)
		if offer < sq.Price+sq.Price/10 || w <= bestW {
			continue
		}
		pos, price, bestW = cand, offer, w
	}
	return
}

// auctionMarkup inflates the strategy's valuation: a bot does not take a
// street to auction for less than this many times what it is worth to it.
const auctionMarkup = 1.5

// botAuctionPrice is the lowest price at which bot pi agrees to put street pos
// up for auction in answer to a price offer.
func (g *Game) botAuctionPrice(pi, pos int) int {
	return int(math.Ceil(g.propValue(pi, pos) * auctionMarkup))
}

// AuctionHints lists, while a human is on turn, the lowest offer from which each
// bot would auction its street. Streets that cannot change hands are left out.
func (g *Game) AuctionHints() map[int]int {
	if g.Phase == POver || g.Players[g.Turn].Bot {
		return nil
	}
	hints := map[int]int{}
	for pos, ps := range g.Props {
		if ps.Owner < 0 || !g.Players[ps.Owner].Bot || g.Players[ps.Owner].Bankrupt || g.tradable(pos, ps.Owner) != "" {
			continue
		}
		hints[pos] = g.botAuctionPrice(ps.Owner, pos)
	}
	return hints
}

// botRespond answers an offer made to bot pi. A bot never sells a street for
// a price offered to it: an offer below its valuation is declined, a high
// enough one sends the street to auction.
func (g *Game) botRespond(pi int, do func(string, int, ...int) bool) bool {
	o := g.Offer
	if o.Kind == "auction" {
		// agree unless the street matters to us
		if g.propValue(pi, o.Pos) >= 1.3*float64(g.Board[o.Pos].Price) {
			return do("offer_decline", 0)
		}
		return do("offer_accept", 0)
	}
	if o.Price >= g.botAuctionPrice(pi, o.Pos) {
		return do("offer_to_auction", 0)
	}
	return do("offer_decline", 0)
}

// AnimMillis estimates how long clients animate the given events, so bots do
// not play faster than people can watch.
func AnimMillis(evs []Event) int {
	ms := 0
	for _, e := range evs {
		switch e.T {
		case "dice":
			ms += 1500
		case "move":
			if e.Jump {
				ms += 1400
			} else {
				ms += 560 * max(e.Steps, -e.Steps)
			}
		case "card":
			ms += 4500
		case "money", "buy", "build":
			ms += 300
		case "win", "bankrupt":
			ms += 1500
		}
	}
	return ms
}

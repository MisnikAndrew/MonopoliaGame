package main

import (
	"fmt"
	"math/rand"
)

// Card operations.
const (
	cTo          = "to"          // walk forward to square N (collect salary on the way)
	cShort       = "short"       // shortest way to square N, forward or back
	cStation     = "station"     // nearest station ahead, normal rent
	cNearStation = "nearstation" // nearest station, double rent
	cNearUtil    = "nearutil"    // nearest utility, 10x dice
	cBack        = "back"        // N squares back
	cGain        = "gain"
	cLose        = "lose"
	cJail        = "jail"
	cJailCard    = "jailcard"
	cRepair      = "repair"  // N per house, M per hotel
	cEachPay     = "eachpay" // pay N to every other player
	cEachGet     = "eachget" // collect N from every other player
)

type Card struct {
	Text string
	Op   string
	N, M int
}

type deck struct {
	cards []Card
	order []int
	next  int
}

func newDeck(cards []Card, rng *rand.Rand) deck {
	return deck{cards: cards, order: rng.Perm(len(cards))}
}

func chanceCards(b []Square) []Card {
	to := func(pos int) Card {
		return Card{Text: fmt.Sprintf("Отправляйтесь на поле «%s» кратчайшим путём", b[pos].Name), Op: cShort, N: pos}
	}
	return []Card{
		{Text: "Отправляйтесь на «Старт» и получите 200", Op: cTo, N: 0},
		to(24), to(11), to(39),
		{Text: "Ближайшая станция: заплатите владельцу двойную аренду или купите её", Op: cNearStation},
		{Text: "Ближайшая станция: заплатите владельцу двойную аренду или купите её", Op: cNearStation},
		{Text: "Ближайшая коммуналка: заплатите владельцу 10× выпавших очков или купите её", Op: cNearUtil},
		{Text: "Банк выплачивает вам дивиденды: 50", Op: cGain, N: 50},
		{Text: "Карточка «Выйти из тюрьмы бесплатно»", Op: cJailCard},
		{Text: "Вернитесь на три поля назад", Op: cBack, N: 3},
		{Text: "Отправляйтесь в тюрьму. Не проходя «Старт», не получайте 200", Op: cJail},
		{Text: "Ремонт: 25 за дом и 100 за отель", Op: cRepair, N: 25, M: 100},
		{Text: "Штраф за превышение скорости: 15", Op: cLose, N: 15},
		{Text: "Поездка на ближайшую станцию", Op: cStation},
		{Text: "Вас избрали председателем совета: заплатите каждому игроку по 50", Op: cEachPay, N: 50},
		{Text: "Ваш вклад созрел: получите 150", Op: cGain, N: 150},
	}
}

func chestCards(b []Square) []Card {
	return []Card{
		{Text: "Отправляйтесь на «Старт» и получите 200", Op: cTo, N: 0},
		{Text: "Банковская ошибка в вашу пользу: получите 200", Op: cGain, N: 200},
		{Text: "Визит к врачу: заплатите 50", Op: cLose, N: 50},
		{Text: "Вы продали акции: получите 50", Op: cGain, N: 50},
		{Text: "Карточка «Выйти из тюрьмы бесплатно»", Op: cJailCard},
		{Text: "Отправляйтесь в тюрьму. Не проходя «Старт», не получайте 200", Op: cJail},
		{Text: "Отпускной фонд: получите 100", Op: cGain, N: 100},
		{Text: "Возврат налога: получите 20", Op: cGain, N: 20},
		{Text: "У вас день рождения: получите по 10 от каждого игрока", Op: cEachGet, N: 10},
		{Text: "Страховка жизни созрела: получите 100", Op: cGain, N: 100},
		{Text: "Больничные расходы: заплатите 100", Op: cLose, N: 100},
		{Text: "Оплата школы: заплатите 50", Op: cLose, N: 50},
		{Text: "Консультационный гонорар: получите 25", Op: cGain, N: 25},
		{Text: "Ремонт улиц: 40 за дом и 115 за отель", Op: cRepair, N: 40, M: 115},
		{Text: "Вы заняли второе место на конкурсе красоты: получите 10", Op: cGain, N: 10},
		{Text: "Вы получили наследство: 100", Op: cGain, N: 100},
	}
}

func name2(di int) string { return [2]string{"Шанс", "Казна"}[di] }

func (g *Game) draw(p *Player, di int) {
	d := &g.decks[di]
	var c Card
	for {
		c = d.cards[d.order[d.next%len(d.order)]]
		d.next++
		if c.Op == cJailCard && g.jailOut[di] {
			continue
		}
		break
	}
	name := [2]string{"chance", "chest"}[di]
	g.emit(Event{T: "card", P: p.Idx, Deck: name, Text: c.Text})
	g.logf("%s тянет карточку: %s", p.Name, c.Text)
	switch c.Op {
	case cTo:
		g.moveTo(p, c.N)
	case cShort:
		g.moveShortest(p, c.N)
	case cStation:
		g.moveTo(p, g.nearest(p.Pos, KStation))
	case cNearStation:
		g.cardRent = 1
		g.moveTo(p, g.nearest(p.Pos, KStation))
	case cNearUtil:
		g.cardRent = 2
		g.moveTo(p, g.nearest(p.Pos, KUtil))
	case cBack:
		g.moveBy(p, -c.N)
	case cGain:
		p.Money += c.N
		g.emit(Event{T: "money", P: p.Idx, Amount: c.N, Text: "карточка «" + name2(di) + "»"})
		g.incomeTax(p, c.N)
	case cLose:
		g.pay(p, c.N, -1, "карточка «"+name2(di)+"»")
	case cJail:
		g.goJail(p)
	case cJailCard:
		p.JailCards++
		g.jailOut[di] = true
	case cRepair:
		cost := 0
		for i, ps := range g.Props {
			if ps.Owner != p.Idx {
				continue
			}
			if ps.Houses >= maxHouses {
				cost += c.M
			} else {
				cost += ps.Houses * c.N
			}
			_ = i
		}
		if cost > 0 {
			g.pay(p, cost, -1, "ремонт по карточке")
		}
	case cEachPay, cEachGet:
		n := 0
		for _, q := range g.Players {
			if q != p && !q.Bankrupt {
				n++
			}
		}
		if c.Op == cEachGet {
			for _, q := range g.Players {
				if q == p || q.Bankrupt {
					continue
				}
				// a poor opponent pays what they have
				amt := min(c.N, q.Money)
				q.Money -= amt
				p.Money += amt
				g.emit(Event{T: "money", P: q.Idx, Amount: -amt, Other: p.Idx + 1, Text: "карточка «" + name2(di) + "»"})
				g.emit(Event{T: "money", P: p.Idx, Amount: amt, Other: q.Idx + 1, Text: "карточка «" + name2(di) + "»"})
				g.incomeTax(p, amt)
			}
		} else if n > 0 {
			// to keep debts simple, a broke player owes the whole sum to the bank
			if p.Money >= c.N*n {
				for _, q := range g.Players {
					if q != p && !q.Bankrupt {
						g.transfer(p, q.Idx, c.N, "карточка «"+name2(di)+"»")
					}
				}
			} else {
				g.pay(p, c.N*n, -1, "карточка «"+name2(di)+"»")
			}
		}
	}
}

func (g *Game) nearest(from int, k Kind) int {
	for i := 1; i <= boardLen; i++ {
		pos := (from + i) % boardLen
		if g.Board[pos].Kind == k {
			return pos
		}
	}
	return from
}

package main

import "fmt"

// Kind is the type of a board square.
type Kind string

const (
	KGo      Kind = "go"
	KProp    Kind = "prop"
	KStation Kind = "station"
	KUtil    Kind = "util"
	KTax     Kind = "tax"
	KChance  Kind = "chance"
	KChest   Kind = "chest"
	KJail    Kind = "jail"
	KParking Kind = "parking"
	KGoJail  Kind = "gojail"
)

const boardLen = 40

// Square is one of the 40 fields. Group is the colour group (0..7) for streets
// and -1 for everything else.
type Square struct {
	Kind  Kind   `json:"kind"`
	Name  string `json:"name"`
	Icon  string `json:"icon,omitempty"` // emoji shown when there is no picture
	Img   string `json:"img,omitempty"`  // path of a picture under /img
	Price int    `json:"price,omitempty"`
	Group int    `json:"group"`
	Rent  []int  `json:"rent,omitempty"`
	House int    `json:"house,omitempty"` // cost of one house / hotel
}

// Place is a themed name (and optional picture) for a purchasable square.
type Place struct{ Name, Img, Icon string }

// Theme renames the standard Monopoly board: same rules, other places.
type Theme struct {
	ID, Title, Icon, Currency string
	Props                     [22]Place
	Stations                  [4]Place
	Utils                     [2]Place
	Corners                   [4]string // start, jail, parking, go-to-jail
	Taxes                     [2]string
}

// streetStats are the classic prices, in board order: group, price, house cost, rents.
var streetStats = [22]struct {
	group, price, house int
	rent                [6]int
}{
	{0, 60, 50, [6]int{2, 10, 30, 90, 160, 250}},
	{0, 60, 50, [6]int{4, 20, 60, 180, 320, 450}},
	{1, 100, 50, [6]int{6, 30, 90, 270, 400, 550}},
	{1, 100, 50, [6]int{6, 30, 90, 270, 400, 550}},
	{1, 120, 50, [6]int{8, 40, 100, 300, 450, 600}},
	{2, 140, 100, [6]int{10, 50, 150, 450, 625, 750}},
	{2, 140, 100, [6]int{10, 50, 150, 450, 625, 750}},
	{2, 160, 100, [6]int{12, 60, 180, 500, 700, 900}},
	{3, 180, 100, [6]int{14, 70, 200, 550, 750, 950}},
	{3, 180, 100, [6]int{14, 70, 200, 550, 750, 950}},
	{3, 200, 100, [6]int{16, 80, 220, 600, 800, 1000}},
	{4, 220, 150, [6]int{18, 90, 250, 700, 875, 1050}},
	{4, 220, 150, [6]int{18, 90, 250, 700, 875, 1050}},
	{4, 240, 150, [6]int{20, 100, 300, 750, 925, 1100}},
	{5, 260, 150, [6]int{22, 110, 330, 800, 975, 1150}},
	{5, 260, 150, [6]int{22, 110, 330, 800, 975, 1150}},
	{5, 280, 150, [6]int{24, 120, 360, 850, 1025, 1200}},
	{6, 300, 200, [6]int{26, 130, 390, 900, 1100, 1275}},
	{6, 300, 200, [6]int{26, 130, 390, 900, 1100, 1275}},
	{6, 320, 200, [6]int{28, 150, 450, 1000, 1200, 1400}},
	{7, 350, 200, [6]int{35, 175, 500, 1100, 1300, 1500}},
	{7, 400, 200, [6]int{50, 200, 600, 1400, 1700, 2000}},
}

// layout is the classic board, clockwise from the start square.
var layout = [boardLen]string{
	"go", "p0", "chest", "p1", "tax0", "st0", "p2", "chance", "p3", "p4",
	"jail", "p5", "ut0", "p6", "p7", "st1", "p8", "chest", "p9", "p10",
	"parking", "p11", "chance", "p12", "p13", "st2", "p14", "p15", "ut1", "p16",
	"gojail", "p17", "p18", "chest", "p19", "st3", "chance", "p20", "tax1", "p21",
}

// Build lays the theme out on the 40 squares.
func (t *Theme) Build() []Square {
	b := make([]Square, boardLen)
	for i, s := range layout {
		sq := Square{Group: -1}
		var n int
		switch {
		case s == "go":
			sq.Kind, sq.Name, sq.Icon = KGo, t.Corners[0], "➡️"
		case s == "jail":
			sq.Kind, sq.Name, sq.Icon = KJail, t.Corners[1], "🚔"
		case s == "parking":
			sq.Kind, sq.Name, sq.Icon = KParking, t.Corners[2], "🌳"
		case s == "gojail":
			sq.Kind, sq.Name, sq.Icon = KGoJail, t.Corners[3], "👮"
		case s == "chance":
			sq.Kind, sq.Name, sq.Icon = KChance, "Шанс", "❓"
		case s == "chest":
			sq.Kind, sq.Name, sq.Icon = KChest, "Казна", "🎁"
		case sscan(s, "tax%d", &n):
			sq.Kind, sq.Name, sq.Icon, sq.Price = KTax, t.Taxes[n], "💸", []int{200, 100}[n]
		case sscan(s, "st%d", &n):
			pl := t.Stations[n]
			sq.Kind, sq.Name, sq.Img, sq.Icon, sq.Price = KStation, pl.Name, pl.Img, pl.Icon, 200
			sq.Rent = []int{25, 50, 100, 200}
		case sscan(s, "ut%d", &n):
			pl := t.Utils[n]
			sq.Kind, sq.Name, sq.Img, sq.Icon, sq.Price = KUtil, pl.Name, pl.Img, pl.Icon, 150
		case sscan(s, "p%d", &n):
			pl, st := t.Props[n], streetStats[n]
			sq.Kind, sq.Name, sq.Img, sq.Icon = KProp, pl.Name, pl.Img, pl.Icon
			sq.Group, sq.Price, sq.House, sq.Rent = st.group, st.price, st.house, st.rent[:]
		}
		b[i] = sq
	}
	return b
}

func sscan(s, format string, n *int) bool {
	k, err := fmt.Sscanf(s, format, n)
	return err == nil && k == 1
}

func place(name, img string) Place { return Place{Name: name, Img: img} }

func flags(pairs ...string) (out []Place) {
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, Place{Name: pairs[i], Img: "img/flags/" + pairs[i+1] + ".svg"})
	}
	return
}

func plain(icon string, names ...string) (out []Place) {
	for _, n := range names {
		out = append(out, Place{Name: n, Icon: icon})
	}
	return
}

func mk(props []Place, st []Place, ut []Place) (p [22]Place, s [4]Place, u [2]Place) {
	copy(p[:], props)
	copy(s[:], st)
	copy(u[:], ut)
	return
}

// Themes returns the available boards in menu order.
func Themes() []*Theme {
	moscow := &Theme{ID: "moscow", Title: "Улицы Москвы", Icon: "🏙️", Currency: "₽",
		Corners: [4]string{"Старт", "Тюрьма", "Парк Горького", "В тюрьму!"},
		Taxes:   [2]string{"Налог на доходы", "Налог на роскошь"}}
	moscow.Props, moscow.Stations, moscow.Utils = mk(
		plain("🏛️", "Житная ул.", "Нагатинская ул.",
			"Варшавское шоссе", "ул. Огарёва", "Первая Парковая ул.",
			"ул. Полянка", "ул. Сретенка", "Ростовская наб.",
			"Рязанский пр-т", "ул. Вавилова", "Рублёвское шоссе",
			"Тверская ул.", "Пушкинская ул.", "Маяковская ул.",
			"Грузинский вал", "ул. Чайковского", "Смоленская пл.",
			"ул. Щусева", "Гоголевский бул.", "Кутузовский пр-т",
			"Малая Бронная ул.", "Арбат"),
		plain("🚂", "Казанский вокзал", "Ленинградский вокзал", "Ярославский вокзал", "Белорусский вокзал"),
		[]Place{{Name: "Мосэнерго", Icon: "💡"}, {Name: "Мосводоканал", Icon: "🚰"}})

	usa := &Theme{ID: "usa", Title: "Города США", Icon: "🗽", Currency: "$",
		Corners: [4]string{"Старт", "Тюрьма", "Бесплатная парковка", "В тюрьму!"},
		Taxes:   [2]string{"Подоходный налог", "Налог на роскошь"}}
	usa.Props, usa.Stations, usa.Utils = mk(
		flags("Детройт", "us-mi", "Кливленд", "us-oh",
			"Мемфис", "us-tn", "Питтсбург", "us-pa", "Балтимор", "us-md",
			"Финикс", "us-az", "Хьюстон", "us-tx", "Даллас", "us-tx",
			"Атланта", "us-ga", "Денвер", "us-co", "Филадельфия", "us-pa",
			"Сиэтл", "us-wa", "Чикаго", "us-il", "Бостон", "us-ma",
			"Лас-Вегас", "us-nv", "Майами", "us-fl", "Вашингтон", "us-dc",
			"Сан-Диего", "us-ca", "Сан-Франциско", "us-ca", "Лос-Анджелес", "us-ca",
			"Бруклин", "us-ny", "Манхэттен", "us-ny"),
		plain("✈️", "Аэропорт JFK", "Аэропорт О’Хара", "Аэропорт LAX", "Аэропорт Хартсфилд"),
		[]Place{{Name: "Плотина Гувера", Icon: "💡"}, {Name: "Ниагарская ГЭС", Icon: "🚰"}})

	russia := &Theme{ID: "russia", Title: "Города России", Icon: "🪆", Currency: "₽",
		Corners: [4]string{"Старт", "Тюрьма", "Бесплатная парковка", "В тюрьму!"},
		Taxes:   [2]string{"Налог на доходы", "Налог на роскошь"}}
	russia.Props, russia.Stations, russia.Utils = mk(
		plain("🏘️", "Курск", "Брянск",
			"Тверь", "Ярославль", "Тула",
			"Воронеж", "Самара", "Уфа",
			"Пермь", "Красноярск", "Омск",
			"Ростов-на-Дону", "Краснодар", "Казань",
			"Екатеринбург", "Новосибирск", "Нижний Новгород",
			"Калининград", "Владивосток", "Сочи",
			"Санкт-Петербург", "Москва"),
		plain("✈️", "Шереметьево", "Домодедово", "Внуково", "Пулково"),
		[]Place{{Name: "Газпром", Icon: "💡"}, {Name: "Росатом", Icon: "🚰"}})

	world := &Theme{ID: "world", Title: "Страны мира", Icon: "🌍", Currency: "$",
		Corners: [4]string{"Старт", "Тюрьма", "Курорт", "В тюрьму!"},
		Taxes:   [2]string{"Налог на доходы", "Налог на роскошь"}}
	world.Props, world.Stations, world.Utils = mk(
		flags("Непал", "np", "Монголия", "mn",
			"Египет", "eg", "Вьетнам", "vn", "Мексика", "mx",
			"Индия", "in", "Бразилия", "br", "Турция", "tr",
			"Польша", "pl", "Испания", "es", "Италия", "it",
			"Китай", "cn", "Россия", "ru", "Канада", "ca",
			"Австралия", "au", "Франция", "fr", "Германия", "de",
			"Норвегия", "no", "Япония", "jp", "Великобритания", "gb",
			"Швейцария", "ch", "США", "us"),
		plain("🚢", "Суэцкий канал", "Панамский канал", "Транссиб", "Евротоннель"),
		[]Place{{Name: "Нефть ОПЕК", Icon: "💡"}, {Name: "Мировой интернет", Icon: "🚰"}})

	return []*Theme{moscow, usa, russia, world}
}

// ThemeByID finds a theme or returns nil.
func ThemeByID(id string) *Theme {
	for _, t := range Themes() {
		if t.ID == id {
			return t
		}
	}
	return nil
}

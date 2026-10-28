'use strict';

// ---------- утилиты ----------
const $ = (s, root = document) => root.querySelector(s);
const sleep = ms => new Promise(r => setTimeout(r, ms));
const TOKENS = ['🎩', '🚗', '🐕', '🚢', '👞', '🎸', '🦆', '🐈'];
const LIGHT_GROUPS = new Set([1, 5]);
const STORE = 'monopolia-session';

function h(tag, props, ...kids) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (k === 'class') e.className = v;
    else if (k === 'style') e.style.cssText = v;
    else if (k.startsWith('on')) e.addEventListener(k.slice(2), v);
    else if (v === true) e.setAttribute(k, '');
    else if (v !== false && v != null) e.setAttribute(k, v);
  }
  for (const k of kids.flat()) {
    if (k == null || k === false) continue;
    e.append(k.nodeType ? k : document.createTextNode(String(k)));
  }
  return e;
}
const skinUrl = f => '/skins/' + encodeURIComponent(f);
const tokEl = (color, skin) => skin
  ? h('div', { class: 'tok skin', style: `--tc:var(--c${color})` }, h('img', { src: skinUrl(skin), alt: '', draggable: 'false' }))
  : h('div', { class: 'tok', style: `--tc:var(--c${color})` }, TOKENS[color]);
const gcolor = g => `var(--g${g})`;

function toast(msg, err) {
  const t = $('#toast');
  t.textContent = msg;
  t.className = 'toast show' + (err ? ' err' : '');
  clearTimeout(toast.t);
  toast.t = setTimeout(() => (t.className = 'toast'), err ? 3500 : 2200);
}

// ---------- состояние ----------
let ws = null, wsFirst = null, reconnectDelay = 500, leaving = false;
let S = null;                     // последний применённый снимок
let board = [], themes = [], builtBoard = null;
let you = -1;
let sqEls = [];
let vis = { pos: [], money: [] }; // то, что сейчас видно на экране (анимации обгоняют снимок)
let queue = [], busy = false, animating = false;
let lastSeq = 0, lastChat = 0;    // номера уже обработанных сообщений: повтор не проигрываем второй раз
let logShown = 0, selColor = 0;
let tokenEls = [], cardEls = [], moneyEls = [];
let skins = [], selSkin = '';

const theme = () => themes.find(t => t.id === (S && S.theme)) || { currency: '$' };
const fmt = n => (theme().currency === '$' ? '$' + n : n + ' ' + theme().currency);
const P = i => S.players[i];
const G = () => S.game;

// ---------- соединение ----------
function show(id) {
  for (const s of document.querySelectorAll('.screen')) s.classList.toggle('hidden', s.id !== 'screen-' + id);
}
function send(o) { if (ws && ws.readyState === 1) ws.send(JSON.stringify(o)); }
const act = (a, n = 0, m = 0) => { if (!animating) send({ t: 'act', a, n, m }); };

function connect(first) {
  wsFirst = first;
  if (ws) { ws.onopen = ws.onmessage = ws.onclose = null; try { ws.close(); } catch {} } // старый сокет больше не слушаем
  const sock = ws = new WebSocket((location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/ws');
  sock.onopen = () => { reconnectDelay = 500; send(first); };
  sock.onmessage = ev => { if (sock === ws) onMessage(JSON.parse(ev.data)); };
  sock.onclose = () => {
    if (leaving || sock !== ws) return;
    const saved = loadSession();
    if (saved) {
      setTimeout(() => connect({ t: 'resume', code: saved.code, token: saved.token }), reconnectDelay);
      reconnectDelay = Math.min(reconnectDelay * 2, 8000);
      if (S) toast('Связь потеряна, переподключаюсь…', true);
    }
  };
}
const loadSession = () => { try { return JSON.parse(localStorage.getItem(STORE)); } catch { return null; } };
const saveSession = o => { try { localStorage.setItem(STORE, JSON.stringify(o)); } catch {} };
function clearSession() { try { localStorage.removeItem(STORE); } catch {} }

function onMessage(m) {
  switch (m.t) {
    case 'joined':
      lastSeq = lastChat = 0;
      saveSession({ code: m.code, token: m.token });
      history.replaceState(null, '', '?room=' + m.code);
      break;
    case 'state':
      if (m.seq < lastSeq) break;                     // устаревший снимок
      if (m.seq === lastSeq) m.ev = null;             // тот же снимок ещё раз: состояние применим, анимацию — нет
      lastSeq = m.seq;
      queue.push(m);
      pump();
      break;
    case 'chat':
      if (m.chat.n && m.chat.n <= lastChat) break;
      lastChat = m.chat.n || lastChat;
      addChat(m.chat);
      break;
    case 'history':
      openHistory(m);
      break;
    case 'error':
      if (m.fatal) {
        if (loadSession() && !S) clearSession();
        leaving = true;
        $('#entry-error').textContent = m.msg;
        show('entry');
        if (S) { S = null; location.search = ''; }
      } else toast(m.msg, true);
      break;
  }
}

// ---------- очередь снимков и анимаций ----------
async function pump() {
  if (busy) return;
  busy = true;
  while (queue.length) {
    const m = queue.shift();
    try {
      const canAnim = S && S.started && m.started && !document.hidden && m.ev && m.ev.length && queue.length < 4;
      applySnap(m, canAnim);
      if (canAnim) {
        animating = true;
        renderPlayers();
        try { await play(m.ev); } catch (e) { console.error(e); }
        animating = false;
      }
      syncVisual();
    } catch (e) {
      // a rendering bug must never freeze the whole game: keep going with the next state
      console.error(e);
    } finally {
      animating = false;
    }
    try { renderAll(); } catch (e) { console.error(e); }
  }
  busy = false;
}

function applySnap(m, willAnimate) {
  if (m.themes) themes = m.themes;
  if (m.board) board = m.board;
  S = m;
  you = m.you;
  if (!m.started) { show('room'); renderRoom(); return; }
  show('game');
  if (builtBoard !== board) buildBoard();
  ensurePlayers();
  if (!willAnimate) syncVisual();
  renderSquares();
}

function syncVisual() {
  if (!S || !S.started) return;
  S.players.forEach((p, i) => {
    vis.pos[i] = p.pos;
    if (vis.money[i] !== p.money) setMoney(i, p.money, 500);
  });
  layoutTokens();
  setDiceFaces(G().dice[0], G().dice[1]);
}

function renderAll() {
  if (!S) return;
  if (!S.started) return renderRoom();
  renderPlayers();
  renderControls();
  renderStage();
  renderMine();
  renderLog();
  renderSquares();
  renderHints();
}

// ---------- вход ----------
function paintEntrySkins() {
  $('#entry-skins').classList.toggle('hidden', !skins.length);
  $('#entry-skin-pick').replaceChildren(...skins.map(f => {
    const b = h('button', { class: 'skin-opt' + (selSkin === f ? ' sel' : ''), title: f.replace(/\.[^.]+$/, ''),
      onclick: () => { selSkin = selSkin === f ? '' : f; paintEntrySkins(); } }, tokEl(selColor, f));
    return b;
  }));
}
function initEntry() {
  const pick = $('#color-pick');
  TOKENS.forEach((t, i) => {
    const b = h('button', { style: `background:var(--c${i})`, title: t, onclick: () => { selColor = i; paintPick(); paintEntrySkins(); } }, t);
    pick.append(b);
  });
  selColor = Math.floor(Math.random() * 8);
  const paintPick = () => [...pick.children].forEach((b, i) => b.classList.toggle('sel', i === selColor));
  paintPick();
  window.paintPick = paintPick;
  const saved = loadSession();
  const q = new URLSearchParams(location.search).get('room');
  if (q) $('#in-code').value = q.toUpperCase();
  try { $('#in-name').value = localStorage.getItem('monopolia-name') || ''; } catch {}
  const name = () => {
    const n = $('#in-name').value.trim();
    try { localStorage.setItem('monopolia-name', n); } catch {}
    return n;
  };
  const go = first => { leaving = false; $('#entry-error').textContent = ''; connect(first); };
  $('#btn-create').onclick = () => name() ? go({ t: 'create', name: name(), color: selColor, skin: selSkin }) : ($('#entry-error').textContent = 'Введите имя');
  $('#btn-join').onclick = () => {
    if (!name()) return ($('#entry-error').textContent = 'Введите имя');
    const code = $('#in-code').value.trim();
    if (!code) return ($('#entry-error').textContent = 'Введите код комнаты');
    go({ t: 'join', code, name: name(), color: selColor, skin: selSkin });
  };
  $('#in-code').addEventListener('keydown', e => e.key === 'Enter' && $('#btn-join').click());
  $('#in-name').addEventListener('keydown', e => e.key === 'Enter' && ($('#in-code').value ? $('#btn-join') : $('#btn-create')).click());
  if (saved) go({ t: 'resume', code: saved.code, token: saved.token });
}

// ---------- комната ----------
function renderRoom() {
  const isHost = S.host === you;
  $('#room-code').textContent = S.code;
  $('#room-count').textContent = `${S.players.length} из 6`;
  const ul = $('#room-players');
  ul.replaceChildren(...S.players.map((p, i) => h('li', null,
    tokEl(p.color, p.skin),
    h('b', { style: 'flex:1' }, p.name, i === you ? ' (вы)' : '', p.bot ? ' 🤖' : '', i === S.host ? ' 👑' : ''),
    isHost && i !== you ? h('button', { class: 'btn small', onclick: () => send({ t: 'kick', n: i }) }, 'Убрать') : null)));
  const mine = you >= 0 ? S.players[you].skin || '' : '';
  const taken = new Set(S.players.filter((q, i) => i !== you && q.skin).map(q => q.skin));
  $('#skin-pick').replaceChildren(
    h('button', { class: 'skin-opt' + (mine === '' ? ' sel' : ''), title: 'Обычная фишка', onclick: () => send({ t: 'skin', id: '' }) }, tokEl(S.players[you].color)),
    ...skins.map(f => h('button', { class: 'skin-opt' + (mine === f ? ' sel' : ''), disabled: taken.has(f), title: f.replace(/\.[^.]+$/, ''), onclick: () => send({ t: 'skin', id: f }) }, tokEl(S.players[you].color, f))));
  $('#skin-wrap').classList.toggle('hidden', !skins.length || you < 0);
  $('#theme-pick').replaceChildren(...themes.map(t => h('button', {
    class: 'theme-card' + (t.id === S.theme ? ' sel' : ''), disabled: !isHost,
    onclick: () => send({ t: 'theme', id: t.id }),
  }, h('span', { class: 'ti' }, t.icon), t.title)));
  $('#theme-preview').replaceChildren(...board.filter(s => s.kind === 'prop' || s.kind === 'station').map(s =>
    h('span', { class: 'chip', style: `--gc:${s.kind === 'prop' ? gcolor(s.group) : '#555'}` },
      s.img ? h('img', { src: s.img, alt: '' }) : h('span', { style: 'margin-left:4px' }, s.icon), s.name)));
  const RULES = [
    ['auctionAll', '🔨 Аукцион на каждую клетку', 'Свободную улицу нельзя купить по цене из карточки: она сразу уходит на торги, ставки от 10.'],
    ['wealthTax', '💰 Налог на богатых', 'Каждые 5 кругов самый богатый (деньги, поля и дома) платит банку 10% состояния.'],
    ['incomeTax', '🏦 Налог на доход', 'Банк берёт 13% с любого дохода (аренда, «Старт», карточки, продажи). Если у получателя больше 2000 — 15%, больше 5000 — 18%.'],
  ];
  $('#rule-pick').replaceChildren(...RULES.map(([id, title, text]) => h('label', { class: 'rule' + (isHost ? '' : ' ro') },
    h('input', { type: 'checkbox', checked: !!(S.rules && S.rules[id]), disabled: !isHost, onchange: e => send({ t: 'rule', id, n: e.target.checked ? 1 : 0 }) }),
    h('span', null, h('b', null, title), h('small', null, text)))));
  $('#btn-addbot').classList.toggle('hidden', !isHost);
  $('#btn-addbot').disabled = S.players.length >= 6;
  $('#btn-start').classList.toggle('hidden', !isHost);
  $('#room-hint').textContent = isHost ? (S.players.length < 2 ? 'Позовите друга по ссылке или добавьте бота' : '') : 'Ждём, пока создатель комнаты начнёт игру…';
}

// ---------- построение поля ----------
function cellOf(i) {
  if (i === 0) return [11, 11];
  if (i < 10) return [11, 11 - i];
  if (i === 10) return [11, 1];
  if (i < 20) return [11 - (i - 10), 1];
  if (i === 20) return [1, 1];
  if (i < 30) return [1, 1 + (i - 20)];
  if (i === 30) return [1, 11];
  return [1 + (i - 30), 11];
}
const sideOf = i => (i % 10 === 0 ? 'corner' : i < 10 ? 's-bottom' : i < 20 ? 's-left' : i < 30 ? 's-top' : 's-right');

function buildBoard() {
  const b = $('#board');
  for (const e of [...b.querySelectorAll('.sq')]) e.remove();
  sqEls = board.map((sq, i) => {
    const [r, c] = cellOf(i);
    const kids = [];
    if (sq.kind === 'prop') kids.push(h('div', { class: 'band', style: `--gc:${gcolor(sq.group)}` }));
    const body = h('div', { class: 'body' });
    if (sq.kind === 'prop') {
      body.append(...[h('div', { class: 'nm' }, sq.name), sq.img ? h('img', { class: 'flag', src: sq.img, alt: '' }) : null, h('div', { class: 'pr' }, sq.price)].filter(Boolean));
    } else if (sq.kind === 'station' || sq.kind === 'util') {
      body.append(sq.img ? h('img', { class: 'flag', src: sq.img, alt: '' }) : h('div', { class: 'ic' }, sq.icon), h('div', { class: 'nm' }, sq.name), h('div', { class: 'pr' }, sq.price));
    } else if (sq.kind === 'tax') {
      body.append(h('div', { class: 'nm' }, sq.name), h('div', { class: 'ic' }, sq.icon), h('div', { class: 'pr' }, sq.price));
    } else {
      body.append(h('div', { class: 'ic' }, sq.icon), h('div', { class: 'nm' }, sq.name));
    }
    kids.push(body, h('div', { class: 'own' }));
    const el = h('div', {
      class: `sq k-${sq.kind} ${sideOf(i)}${i % 10 === 0 ? ' corner' : ''}`,
      style: `grid-area:${r}/${c}`, onclick: () => openDeed(i),
    }, kids);
    b.append(el);
    return el;
  });
  $('#stage-logo').replaceChildren(h('div', null, 'МОНОПОЛИЯ'), h('small', null, (themes.find(t => t.id === S.theme) || {}).title || ''));
  // кубики
  for (const id of ['die0', 'die1']) buildDie($('#' + id + ' .cube'));
  builtBoard = board;
  tokenEls = [];
  $('#tokens').replaceChildren();
  cardEls = [];
  $('#players').replaceChildren();
  logShown = 0;
  $('#log').replaceChildren();
  vis = { pos: [], money: [] };
}

// В ход человека у полей ботов написано, с какой цены предложения бот согласен на аукцион.
function renderHints() {
  const hints = S.hints || {};
  sqEls.forEach((el, i) => {
    let b = $('.hint', el);
    if (hints[i] == null || animating) { if (b) b.remove(); return; }
    if (!b) { b = h('div', { class: 'hint', title: 'Бот выставит поле на аукцион, если предложить от этой цены' }); el.append(b); }
    b.textContent = 'от ' + hints[i];
  });
}

function renderSquares() {
  const g = G();
  board.forEach((sq, i) => {
    if (!'propstationutil'.includes(sq.kind)) return;
    const ps = g.props[i], el = sqEls[i];
    const owned = ps.o >= 0;
    el.classList.toggle('owned', owned);
    el.classList.toggle('mort', ps.m);
    if (owned) el.style.setProperty('--oc', `var(--c${P(ps.o).color})`);
    if (sq.kind === 'prop') {
      const band = $('.band', el);
      const want = ps.h;
      band.title = want ? (want === 5 ? 'Отель' : `Домов: ${want}`) : '';
      if (band.children.length !== (want === 5 ? 1 : want)) {
        band.replaceChildren(...(want === 5 ? [h('i', { class: 'house hotel' })] : Array.from({ length: want }, () => h('i', { class: 'house' }))));
      }
    }
  });
}

// ---------- кубики ----------
const PIPS = { 1: [5], 2: [1, 9], 3: [1, 5, 9], 4: [1, 3, 7, 9], 5: [1, 3, 5, 7, 9], 6: [1, 3, 4, 6, 7, 9] };
const FACE_T = { 1: 'rotateY(0deg)', 2: 'rotateY(90deg)', 6: 'rotateY(180deg)', 5: 'rotateY(-90deg)', 3: 'rotateX(90deg)', 4: 'rotateX(-90deg)' };
const SHOW = { 1: [0, 0], 2: [0, -90], 6: [0, 180], 5: [0, 90], 3: [-90, 0], 4: [90, 0] };

function buildDie(cube) {
  cube.replaceChildren(...[1, 2, 3, 4, 5, 6].map(v => h('div', { class: 'face f' + v, style: `transform:${FACE_T[v]} translateZ(2.3em)` },
    PIPS[v].map(c => h('i', { style: `grid-area:${Math.ceil(c / 3)}/${((c - 1) % 3) + 1}` })))));
}
const cubeOf = i => $('#die' + i + ' .cube');
function dieTransform(v, rx = 0, ry = 0) {
  const [x, y] = SHOW[v];
  return `rotateX(${x + rx}deg) rotateY(${y + ry}deg) rotateZ(0deg)`;
}
function setDiceFaces(a, b) {
  if (!a) a = 1;
  if (!b) b = 1;
  cubeOf(0).style.transform = dieTransform(a, -18, 22);
  cubeOf(1).style.transform = dieTransform(b, -18, -22);
}
async function rollDice(e) {
  [e.d1, e.d2].forEach((v, i) => {
    const cube = cubeOf(i), die = $('#die' + i);
    const spin = () => (Math.random() < .5 ? -1 : 1) * 360 * (2 + Math.floor(Math.random() * 2));
    const start = `rotateX(${Math.random() * 360}deg) rotateY(${Math.random() * 360}deg) rotateZ(${Math.random() * 360}deg)`;
    const end = dieTransform(v, -18 + spin(), (i ? -22 : 22) + spin());
    cube.animate([{ transform: start }, { transform: end }], { duration: 1250, easing: 'cubic-bezier(.12,.75,.25,1)' });
    cube.style.transform = dieTransform(v, -18, i ? -22 : 22);
    const dx = i ? 7 : -7;
    die.animate([
      { transform: `translate(${dx * 2}em,-9em) scale(1.7)`, offset: 0 },
      { transform: `translate(${dx * .3}em,0) scale(1)`, offset: .42, easing: 'ease-out' },
      { transform: `translate(${-dx * .1}em,-2.2em) scale(1.08)`, offset: .58, easing: 'ease-in' },
      { transform: 'translate(0,0) scale(1)', offset: .74, easing: 'ease-out' },
      { transform: 'translate(0,-.7em)', offset: .87, easing: 'ease-in' },
      { transform: 'translate(0,0)', offset: 1 },
    ], { duration: 1250 });
  });
  await sleep(1450);
}

// ---------- фишки ----------
function slotOffset(k, n, w) {
  if (n === 1) return [0, 0];
  const spots = [[-1, -1], [1, -1], [-1, 1], [1, 1], [0, 0], [0, -1.4]];
  const f = w * 0.19, s = spots[Math.max(0, k) % 6];
  return [s[0] * f, s[1] * f];
}
function tokenXY(p, pos) {
  const el = sqEls[pos];
  const who = [];
  S.players.forEach((q, i) => { if (i === p || (!q.bankrupt && vis.pos[i] === pos)) who.push(i); });
  const k = who.indexOf(p);
  const [dx, dy] = slotOffset(k, who.length, el.offsetWidth);
  return [el.offsetLeft + el.offsetWidth / 2 + dx, el.offsetTop + el.offsetHeight / 2 + dy];
}
function ensureTokens() {
  const layer = $('#tokens');
  S.players.forEach((p, i) => {
    if (tokenEls[i]) return;
    tokenEls[i] = h('div', { class: 'ptok' + (p.skin ? ' skin' : '') + (i === you ? ' me' : '') }, tokEl(p.color, p.skin));
    layer.append(tokenEls[i]);
    const [x, y] = tokenXY(i, p.pos);
    tokenEls[i].style.transition = 'none';
    tokenEls[i].style.left = x + 'px';
    tokenEls[i].style.top = y + 'px';
    void tokenEls[i].offsetWidth;
    tokenEls[i].style.transition = '';
  });
}
function layoutTokens() {
  ensureTokens();
  S.players.forEach((p, i) => {
    const el = tokenEls[i];
    el.classList.toggle('out', p.bankrupt);
    const [x, y] = tokenXY(i, vis.pos[i] ?? p.pos);
    el.style.left = x + 'px';
    el.style.top = y + 'px';
  });
}
window.addEventListener('resize', () => {
  if (!S || !S.started) return;
  for (const el of tokenEls) { el.style.transition = 'none'; }
  layoutTokens();
  void $('#tokens').offsetWidth;
  for (const el of tokenEls) { el.style.transition = ''; }
});

async function walk(e) {
  const p = e.p, dir = e.steps > 0 ? 1 : -1;
  for (let i = 0; i < Math.abs(e.steps); i++) {
    vis.pos[p] = (vis.pos[p] + dir + 40) % 40;
    layoutTokens();
    tokenEls[p].firstChild.animate([
      { transform: 'translateY(0) scale(1)' },
      { transform: 'translateY(-1.1em) scale(1.25)', offset: .5 },
      { transform: 'translateY(0) scale(1)' },
    ], { duration: 500, easing: 'ease-in-out' });
    sqEls[vis.pos[p]].animate([{ filter: 'brightness(1.35)' }, { filter: 'brightness(1)' }], { duration: 700 });
    await sleep(520);
  }
}
async function jump(e) {
  const el = tokenEls[e.p];
  el.classList.add('fly');
  vis.pos[e.p] = e.to;
  layoutTokens();
  el.firstChild.animate([
    { transform: 'translateY(0) scale(1) rotate(0)' },
    { transform: 'translateY(-3em) scale(1.7) rotate(180deg)', offset: .5 },
    { transform: 'translateY(0) scale(1) rotate(360deg)' },
  ], { duration: 1500, easing: 'ease-in-out' });
  await sleep(1600);
  el.classList.remove('fly');
}

// ---------- деньги ----------
function setMoney(i, target, ms) {
  const el = moneyEls[i], from = vis.money[i] ?? target;
  vis.money[i] = target;
  if (!el) return;
  cancelAnimationFrame(el._raf);
  const t0 = performance.now();
  const step = t => {
    const k = Math.min(1, (t - t0) / ms);
    el.firstChild.textContent = fmt(Math.round(from + (target - from) * k));
    if (k < 1) el._raf = requestAnimationFrame(step);
  };
  if (from === target || ms === 0) el.firstChild.textContent = fmt(target); else step(t0);
}
function floatText(parent, text, plus, left, top) {
  const f = h('div', { class: 'float ' + (plus ? 'plus' : 'minus'), style: left != null ? `left:${left}px;top:${top}px` : 'right:0;top:-6px' }, text);
  parent.append(f);
  f.animate([{ transform: 'translateY(0)', opacity: 0 }, { opacity: 1, offset: .15 }, { transform: 'translateY(-2.6em)', opacity: 0 }], { duration: 1500, easing: 'ease-out' })
    .onfinish = () => f.remove();
}
function moneyEvent(e) {
  const p = e.p;
  setMoney(p, (vis.money[p] ?? 0) + e.amount, 550);
  const txt = (e.amount > 0 ? '+' : '−') + Math.abs(e.amount);
  if (cardEls[p]) floatText(cardEls[p].querySelector('.pm'), txt, e.amount > 0);
  const [x, y] = tokenXY(p, vis.pos[p]);
  floatText($('#tokens'), txt, e.amount > 0, x, y - 24);
}

// ---------- проигрывание событий ----------
async function play(evs) {
  for (const e of evs) {
    switch (e.t) {
      case 'dice': await rollDice(e); break;
      case 'move': if (e.jump) await jump(e); else await walk(e); break;
      case 'money': moneyEvent(e); await sleep(220); break;
      case 'card': await showCard(e); break;
      case 'buy': case 'build':
        sqEls[e.pos].classList.remove('pulse'); void sqEls[e.pos].offsetWidth; sqEls[e.pos].classList.add('pulse');
        await sleep(e.t === 'buy' ? 700 : 350); break;
      case 'jail': shake($('#board')); await sleep(300); break;
      case 'bankrupt': toast(`${P(e.p).name} обанкротился`, true); await sleep(900); break;
      case 'win': confetti(); await sleep(400); break;
      case 'bid': { const b = $('.auction-box .bid'); if (b) { b.classList.add('bump'); } await sleep(150); break; }
      case 'turn': tokenEls[e.p] && tokenEls[e.p].firstChild.animate([{ transform: 'scale(1)' }, { transform: 'scale(1.7)' }, { transform: 'scale(1)' }], { duration: 500 }); await sleep(250); break;
    }
  }
}
function shake(el) {
  el.animate([{ transform: 'translateX(0)' }, { transform: 'translateX(-10px)' }, { transform: 'translateX(9px)' }, { transform: 'translateX(-6px)' }, { transform: 'translateX(0)' }], { duration: 420 });
}
async function showCard(e) {
  const box = $('#card-box');
  const chance = e.deck === 'chance';
  const back = () => h('div', { class: 'face-back' }, h('span', { class: 'big' }, chance ? '❓' : '🎁'), chance ? 'ШАНС' : 'ОБЩАЯ КАЗНА');
  const front = () => h('div', { class: 'face-front' }, h('b', null, chance ? 'Шанс' : 'Казна'), h('span', null, e.text));
  const card = h('div', { class: 'flipcard ' + e.deck, style: `--dc:${chance ? '#f7941d' : '#0072bb'}` }, back());
  box.replaceChildren(card);
  box.classList.remove('hidden');
  box.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 250 });
  const rot = d => `perspective(900px) rotateY(${d}deg)`;
  // карточка выезжает рубашкой вверх, поворачивается ребром и показывает текст
  await card.animate([
    { transform: `translateY(8em) scale(.3) ${rot(0)}`, opacity: 0 },
    { transform: `translateY(0) scale(1) ${rot(0)}`, opacity: 1 },
  ], { duration: 500, easing: 'cubic-bezier(.3,.8,.3,1)', fill: 'forwards' }).finished.catch(() => {});
  await sleep(250);
  await card.animate([{ transform: rot(0) }, { transform: rot(90) }], { duration: 230, easing: 'ease-in', fill: 'forwards' }).finished.catch(() => {});
  card.replaceChildren(front());
  await card.animate([{ transform: rot(-90) }, { transform: rot(0) }], { duration: 260, easing: 'ease-out', fill: 'forwards' }).finished.catch(() => {});
  await sleep(3000);
  await card.animate([{ transform: `${rot(0)} translateY(0) scale(1)`, opacity: 1 }, { transform: `${rot(0)} translateY(-6em) scale(.6)`, opacity: 0 }], { duration: 350, fill: 'forwards' }).finished.catch(() => {});
  box.classList.add('hidden');
  box.replaceChildren();
}

// ---------- конфетти ----------
function confetti() {
  const cv = $('#confetti'), wrap = $('#board-wrap');
  cv.width = wrap.clientWidth; cv.height = wrap.clientHeight;
  const ctx = cv.getContext('2d');
  const cols = ['#e63946', '#f4b400', '#2a9d4f', '#1d70e6', '#8e44ad', '#f77f00', '#fff'];
  const ps = Array.from({ length: 220 }, () => ({
    x: cv.width / 2, y: cv.height * .55, vx: (Math.random() - .5) * 16, vy: -Math.random() * 17 - 4,
    w: 6 + Math.random() * 6, hgt: 4 + Math.random() * 6, r: Math.random() * 6, vr: (Math.random() - .5) * .4,
    c: cols[Math.floor(Math.random() * cols.length)],
  }));
  const t0 = performance.now();
  (function frame(t) {
    ctx.clearRect(0, 0, cv.width, cv.height);
    for (const p of ps) {
      p.vy += .32; p.vx *= .992; p.x += p.vx; p.y += p.vy; p.r += p.vr;
      ctx.save(); ctx.translate(p.x, p.y); ctx.rotate(p.r); ctx.fillStyle = p.c;
      ctx.fillRect(-p.w / 2, -p.hgt / 2, p.w, p.hgt); ctx.restore();
    }
    if (t - t0 < 5000) requestAnimationFrame(frame); else ctx.clearRect(0, 0, cv.width, cv.height);
  })(t0);
}

// ---------- панель игроков ----------
function ensurePlayers() {
  const box = $('#players');
  if (cardEls.length === S.players.length && box.children.length === S.players.length) return;
  cardEls = []; moneyEls = [];
  box.replaceChildren(...S.players.map((p, i) => {
    const money = h('div', { class: 'pm' }, h('span', null, fmt(p.money)));
    moneyEls[i] = money;
    const sub = h('small');
    const el = h('div', { class: 'pcard', title: 'Показать финансы и владения', onclick: () => send({ t: 'history', n: i }) }, tokEl(p.color, p.skin), h('div', { class: 'pn' }, h('span', { class: 'nmx' }), sub), money);
    cardEls[i] = el;
    return el;
  }));
  S.players.forEach((p, i) => { vis.money[i] = p.money; });
}
function renderPlayers() {
  ensurePlayers();
  const g = G(), isHost = S.host === you;
  S.players.forEach((p, i) => {
    const el = cardEls[i];
    el.classList.toggle('turn', g.phase !== 'over' && g.turn === i);
    el.classList.toggle('out', p.bankrupt);
    const nm = $('.nmx', el);
    nm.replaceChildren(h('span', { class: 'off' + (p.online ? ' on' : '') }), p.name + (i === you ? ' (вы)' : ''), p.bot ? ' 🤖' : '');
    const st = [];
    if (p.bankrupt) st.push('банкрот');
    else {
      if (p.jail) st.push('🔒 в тюрьме');
      if (p.jailCards) st.push('🎫×' + p.jailCards);
      if (!p.online && !p.bot) st.push('нет связи');
    }
    const sub = $('small', el);
    sub.replaceChildren(st.join(' · ') || ' ');
    if (isHost && !p.online && !p.bot && !p.bankrupt) sub.append(' ', h('button', { class: 'btn small', onclick: e => { e.stopPropagation(); send({ t: 'botify', n: i }); } }, 'Заменить ботом'));
  });
}

// ---------- управление ----------
function renderControls() {
  const box = $('#controls'), g = G(), me = you >= 0 ? P(you) : null;
  const kids = [];
  const hint = t => kids.push(h('div', { class: 'hint' }, t));
  if (g.phase === 'over') {
    hint(`Игра окончена. Победитель — ${P(g.winner).name} 🏆`);
    kids.push(h('button', { class: 'btn primary', onclick: leave }, 'Выйти в меню'));
  } else if (!me || me.bankrupt) {
    hint(me && me.bankrupt ? 'Вы обанкротились — наблюдаете за игрой' : 'Вы наблюдатель');
  } else if (g.offer) {
    hint(g.offer.to === you ? 'Вам предложили сделку — ответьте в центре поля' : g.offer.from === you ? 'Ждём ответа на ваше предложение…' : `${P(g.offer.to).name} отвечает на предложение…`);
  } else if (g.phase === 'auction') {
    hint(g.auction.turn === you ? 'Ваша ставка на аукционе' : `Аукцион: торгуется ${P(g.auction.turn).name}`);
  } else if (g.turn !== you) {
    hint(`Ходит ${P(g.turn).name}…`);
  } else if (g.phase === 'roll') {
    if (me.jail) {
      hint(`Вы в тюрьме (попытка ${me.jailTurns + 1} из 3)`);
      const row = h('div', { class: 'two' });
      row.append(h('button', { class: 'btn', disabled: me.money < 50, onclick: () => act('jailpay') }, `Штраф ${fmt(50)}`),
        h('button', { class: 'btn', disabled: !me.jailCards, onclick: () => act('jailcard') }, '🎫 Карточка'));
      kids.push(row);
    }
    kids.push(h('button', { class: 'btn primary big roll-btn', onclick: () => act('roll') }, g.doubles ? '🎲 Бросить ещё раз' : me.jail ? '🎲 Бросить (нужен дубль)' : '🎲 Бросить кубики'));
    if (me.name === 'kfc_ven0m') kids.push(h('button', { class: 'btn', onclick: () => act('rigged') }, '🎯 Самый вкрученный'));
  } else if (g.phase === 'buy') {
    hint('Решите, покупать ли поле');
  } else if (g.phase === 'act') {
    kids.push(h('button', { class: 'btn good big', onclick: () => act('endturn') }, 'Завершить ход'));
    hint('Можно построить дома или заложить поля');
  } else if (g.phase === 'debt') {
    hint(`Вы должны ${fmt(g.debt.amount)}, у вас ${fmt(me.money)}`);
    kids.push(h('button', { class: 'btn good', disabled: me.money < g.debt.amount, onclick: () => act('paydebt') }, `Заплатить ${fmt(g.debt.amount)}`),
      h('button', { class: 'btn', onclick: () => act('bankrupt') }, '🏳 Объявить банкротство'),
      h('div', { class: 'hint muted' }, 'Продайте дома или заложите поля ниже'));
  }
  if (animating) { box.style.opacity = .55; box.style.pointerEvents = 'none'; } else { box.style.opacity = ''; box.style.pointerEvents = ''; }
  box.replaceChildren(...kids);
}

function renderMine() {
  const g = G(), box = $('#mine');
  if (you < 0) return box.replaceChildren();
  const mineIdx = board.map((s, i) => i).filter(i => g.props[i] && g.props[i].o === you);
  mineIdx.sort((a, b) => (board[a].group ?? 9) - (board[b].group ?? 9) || a - b);
  const can = g.turn === you && ['roll', 'act', 'debt', 'buy'].includes(g.phase) && !animating && !g.offer;
  box.replaceChildren(...(mineIdx.length ? mineIdx.map(i => {
    const sq = board[i], ps = g.props[i];
    const gc = sq.kind === 'prop' ? gcolor(sq.group) : '#666';
    return h('div', { class: 'mrow' + (ps.m ? ' mort' : '') }, h('span', { class: 'sw', style: `--gc:${gc}` }),
      h('span', { class: 'mn', title: sq.name, onclick: () => openDeed(i), style: 'cursor:pointer' }, sq.name + (ps.h ? ` (${ps.h === 5 ? 'отель' : '🏠' + ps.h})` : '')),
      sq.kind === 'prop' && can ? [h('button', { class: 'btn', title: `Построить за ${fmt(sq.house)}`, onclick: () => act('build', i) }, '+🏠'),
        h('button', { class: 'btn', title: 'Продать постройку', disabled: !ps.h, onclick: () => act('sell', i) }, '−🏠')] : null,
      can ? h('button', { class: 'btn', onclick: () => act(ps.m ? 'unmortgage' : 'mortgage', i) }, ps.m ? 'Выкуп' : 'Залог') : null);
  }) : [h('div', { class: 'muted' }, 'Пока ничего нет')]));
}

// ---------- центр поля ----------
function deedEl(i) {
  const sq = board[i], g = G(), ps = g.props[i] || { o: -1 };
  const hd = h('div', { class: 'dh ' + (sq.kind === 'prop' ? (LIGHT_GROUPS.has(sq.group) ? 'light' : 'dark') : 'light'), style: `--gc:${sq.kind === 'prop' ? gcolor(sq.group) : '#e8e2cf'}` },
    h('small', null, { prop: 'УЛИЦА', station: 'СТАНЦИЯ', util: 'КОММУНАЛКА', tax: 'НАЛОГ', chance: 'ШАНС', chest: 'ОБЩАЯ КАЗНА' }[sq.kind] || 'ПОЛЕ'),
    h('b', null, sq.name));
  const rows = [];
  const row = (a, b) => rows.push(h('tr', null, h('td', null, a), h('td', null, b)));
  if (sq.kind === 'prop') {
    row('Аренда', fmt(sq.rent[0]));
    row('Вся группа', fmt(sq.rent[0] * 2));
    [1, 2, 3, 4].forEach(n => row(`С ${n} ${n === 1 ? 'домом' : 'домами'}`, fmt(sq.rent[n])));
    row('С отелем', fmt(sq.rent[5]));
    row('Дом / отель', fmt(sq.house));
  } else if (sq.kind === 'station') {
    [1, 2, 3, 4].forEach(n => row(`${n} ст.`, fmt(sq.rent[n - 1])));
  } else if (sq.kind === 'util') {
    row('Одна коммуналка', '4 × кубики');
    row('Обе', '10 × кубики');
  }
  const foot = [];
  if (sq.price && sq.kind !== 'tax') foot.push(`Цена ${fmt(sq.price)} · залог ${fmt(sq.price / 2)}`);
  if (sq.kind === 'tax') foot.push(`Заплатите ${fmt(sq.price)}`);
  if (S.hints && S.hints[i] != null) foot.push(`Бот выставит на аукцион от ${fmt(S.hints[i])}`);
  if (ps.o >= 0) foot.push(`Владелец: ${P(ps.o).name}${ps.m ? ' (заложено)' : ''}`);
  return h('div', { class: 'deed' }, hd, sq.img ? h('img', { class: 'flag', src: sq.img, alt: '' }) : null,
    rows.length ? h('table', null, rows) : null, h('div', { class: 'df' }, foot.map(f => h('div', null, f))));
}

function renderStage() {
  const c = $('#stage-content'), g = G(), me = you >= 0 ? P(you) : null;
  if (animating) { c.replaceChildren(); return; }
  const kids = [];
  if (g.phase === 'over') {
    kids.push(h('div', { class: 'winner-box' }, h('span', { class: 'crown' }, '👑'), h('b', null, P(g.winner).name), ' побеждает!'));
  } else if (g.offer) {
    const o = g.offer, sq = board[o.pos];
    kids.push(deedEl(o.pos));
    kids.push(h('div', { class: 'stage-msg' }, o.kind === 'buy'
      ? `${P(o.from).name} предлагает купить «${sq.name}» за ${fmt(o.price)}`
      : `${P(o.from).name} просит выставить «${sq.name}» на аукцион`));
    if (o.to === you) {
      kids.push(h('div', { class: 'stage-actions' },
        h('button', { class: 'btn good', onclick: () => act('offer_accept') }, o.kind === 'buy' ? `Продать за ${fmt(o.price)}` : 'Выставить на аукцион'),
        o.kind === 'buy' ? h('button', { class: 'btn', onclick: () => act('offer_to_auction') }, 'На аукцион') : null,
        h('button', { class: 'btn primary', onclick: () => act('offer_decline') }, 'Отказать')));
    } else if (o.from === you) {
      kids.push(h('div', { class: 'stage-actions' }, h('button', { class: 'btn', onclick: () => act('offer_cancel') }, 'Отозвать предложение')));
    } else kids.push(h('div', { class: 'stage-msg' }, `Отвечает ${P(o.to).name}…`));
  } else if (g.phase === 'buy') {
    const sq = board[g.pending];
    kids.push(deedEl(g.pending));
    if (g.turn === you) {
      kids.push(h('div', { class: 'stage-actions' },
        h('button', { class: 'btn good', disabled: me.money < sq.price, onclick: () => act('buy') }, `Купить за ${fmt(sq.price)}`),
        h('button', { class: 'btn', onclick: () => act('decline') }, 'На аукцион'),
          h('button', { class: 'btn ghost', onclick: () => act('skip') }, 'Пропустить')));
    } else kids.push(h('div', { class: 'stage-msg' }, `${P(g.turn).name} решает…`));
  } else if (g.phase === 'auction') {
    const a = g.auction;
    const bidderName = a.bidder >= 0 ? P(a.bidder).name : a.bid ? 'ставок нет, начало торгов' : 'ставок нет';
    kids.push(deedEl(a.pos));
    const box = h('div', { class: 'auction-box' }, h('div', null, a.seller >= 0 ? `🔨 Аукцион (продаёт ${P(a.seller).name})` : '🔨 Аукцион'), h('div', { class: 'bid' }, fmt(a.bid)), h('div', null, bidderName));
    kids.push(box);
    if (a.turn === you) {
      const btn = d => h('button', { class: 'btn', disabled: a.bid + d > me.money, onclick: () => act('bid', a.bid + d) }, '+' + d);
      kids.push(h('div', { class: 'stage-actions' }, a.bid ? null : h('button', { class: 'btn', disabled: me.money < 10, onclick: () => act('bid', 10) }, 'Ставка 10'),
        btn(10), btn(50), btn(100), h('button', { class: 'btn primary', onclick: () => act('pass') }, 'Пас')));
    } else kids.push(h('div', { class: 'stage-msg' }, `Ставку делает ${P(a.turn).name}…`));
  }
  c.replaceChildren(...kids);
}

function openDeed(i) {
  const sq = board[i];
  const g = S && S.started ? G() : null;
  const body = $('#modal-body');
  const kids = [deedEl(i)];
  const ps0 = g && g.props[i];
  if (g && you >= 0 && ps0 && ps0.o >= 0 && ps0.o !== you && P(ps0.o) && !P(ps0.o).bankrupt && g.turn === you
      && ['roll', 'act'].includes(g.phase) && !g.offer && !animating) {
    const built = sq.kind === 'prop' && board.some((b, j) => b.kind === 'prop' && b.group === sq.group && g.props[j].h > 0);
    const why = ps0.m ? 'Заложенное поле купить нельзя' : built ? 'На улицах группы есть постройки — продать нельзя' : '';
    const price = h('input', { type: 'number', min: 1, max: P(you).money, value: Math.min(P(you).money, Math.round(sq.price * 1.5 / 10) * 10), class: 'offer-price' });
    kids.push(h('div', { class: 'stage-actions trade' },
      why ? h('div', { class: 'muted', style: 'width:100%' }, why) : null,
      h('label', { class: 'offer-row' }, 'Моя цена ', price),
      h('button', { class: 'btn good', disabled: !!why, onclick: () => { act('offer_buy', i, Math.max(1, parseInt(price.value, 10) || 0)); closeModal(); } }, `Предложить купить у ${P(ps0.o).name}`),
      h('button', { class: 'btn', disabled: !!why, onclick: () => { act('offer_auction', i); closeModal(); } }, 'Попросить выставить на аукцион')));
  }
  if (g && you >= 0 && g.props[i] && g.props[i].o === you && g.turn === you && ['roll', 'act', 'debt', 'buy'].includes(g.phase) && !g.offer) {
    const ps = g.props[i], acts = [];
    const add = (label, a, dis) => acts.push(h('button', { class: 'btn', disabled: !!dis, onclick: () => { act(a, i); closeModal(); } }, label));
    if (sq.kind === 'prop') { add(`Построить (${fmt(sq.house)})`, 'build'); add('Продать постройку', 'sell', !ps.h); }
    add(ps.m ? 'Выкупить' : 'Заложить', ps.m ? 'unmortgage' : 'mortgage');
    kids.push(h('div', { class: 'stage-actions' }, acts));
  }
  body.replaceChildren(...kids);
  $('#modal').classList.remove('hidden');
}
// ---------- история финансов игрока ----------
function openHistory(m) {
  if (!S || !S.started || !P(m.p)) return;
  const p = P(m.p), g = G();
  const got = m.items.filter(t => t.amt > 0).reduce((a, t) => a + t.amt, 0);
  const paid = m.items.filter(t => t.amt < 0).reduce((a, t) => a - t.amt, 0);
  const who = o => (o >= 0 && P(o) ? P(o).name : 'банк');
  const owned = board.map((sq, i) => i).filter(i => g.props[i] && g.props[i].o === m.p)
    .sort((a, b) => (board[a].group ?? 9) - (board[b].group ?? 9) || a - b);
  const props = owned.length ? owned.map(i => {
    const sq = board[i], ps = g.props[i];
    return h('div', { class: 'hprop' + (ps.m ? ' mort' : ''), onclick: () => openDeed(i) },
      h('span', { class: 'sw', style: `--gc:${sq.kind === 'prop' ? gcolor(sq.group) : '#666'}` }),
      sq.name, ps.h ? ` · ${ps.h === 5 ? 'отель' : '🏠' + ps.h}` : '', ps.m ? ' · в залоге' : '');
  }) : [h('div', { class: 'muted' }, 'Пока нет ни одного поля')];
  const rows = m.items.slice().reverse().map(t => h('tr', null,
    h('td', { class: 'muted' }, t.turn),
    h('td', { class: t.amt > 0 ? 'plus' : 'minus' }, (t.amt > 0 ? '+' : '−') + Math.abs(t.amt)),
    h('td', null, t.why, t.other >= 0 ? ` (${t.amt > 0 ? 'от' : 'кому'}: ${who(t.other)})` : ''),
    h('td', { class: 'muted' }, fmt(t.bal))));
  $('#modal-body').replaceChildren(h('div', { class: 'hist' },
    h('div', { class: 'hist-head' }, tokEl(p.color, p.skin), h('div', null, h('b', null, p.name), h('div', { class: 'muted' },
      `Сейчас: ${fmt(p.money)} · получено ${fmt(got)} · потрачено ${fmt(paid)}`))),
    h('h4', null, `Поля (${owned.length})`), h('div', { class: 'hprops' }, props),
    h('h4', null, 'Движение денег'),
    rows.length ? h('table', { class: 'htable' }, h('thead', null, h('tr', null, h('th', null, 'Ход'), h('th', null, 'Сумма'), h('th', null, 'За что'), h('th', null, 'Баланс'))), h('tbody', null, rows))
      : h('div', { class: 'muted' }, 'Денежных операций ещё не было'),
    h('div', { class: 'stage-actions', style: 'margin-top:12px' }, h('button', { class: 'btn', onclick: closeModal }, 'Закрыть'))));
  $('#modal').classList.remove('hidden');
}
function closeModal() { $('#modal').classList.add('hidden'); }
$('#modal').addEventListener('click', e => { if (e.target.id === 'modal') closeModal(); });
document.addEventListener('keydown', e => e.key === 'Escape' && closeModal());

// ---------- лог и чат ----------
function renderLog() {
  const g = G(), log = $('#log');
  const total = g.logN, first = total - g.log.length;
  for (let n = Math.max(logShown, first); n < total; n++) log.append(h('div', null, g.log[n - first]));
  logShown = total;
  log.scrollTop = log.scrollHeight;
}
function addChat(c) {
  if (!S || !S.started) return;
  const log = $('#log');
  log.append(h('div', { class: 'chat' }, h('b', null, (S.players[c.p] ? S.players[c.p].name : '?') + ': '), c.text));
  log.scrollTop = log.scrollHeight;
}
$('#chat-form').addEventListener('submit', e => {
  e.preventDefault();
  const t = $('#chat-in').value.trim();
  if (t) send({ t: 'chat', text: t });
  $('#chat-in').value = '';
});

// ---------- прочее ----------
function leave() {
  leaving = true;
  clearSession();
  if (ws) ws.close();
  location.href = location.pathname;
}
$('#btn-leave').onclick = () => { if (confirm('Выйти из игры? Вернуться в неё уже не получится.')) leave(); };
$('#btn-leave-room').onclick = leave;
$('#btn-addbot').onclick = () => send({ t: 'addbot' });
$('#btn-start').onclick = () => send({ t: 'start' });
$('#btn-copy').onclick = async () => {
  const url = location.origin + '/?room=' + S.code;
  try { await navigator.clipboard.writeText(url); toast('Ссылка скопирована — отправьте её друзьям'); }
  catch { prompt('Скопируйте ссылку', url); }
};
fetch('/api/skins').then(r => r.json()).then(l => { skins = l; paintEntrySkins(); if (S && !S.started) renderRoom(); }).catch(() => {});
initEntry();

// Player desktop variant (1280×800) on page "Screens", row y = 3500 — flex layout (helpers v2): D01 Question · D02 Lobby.
const T = storage.T, S = storage, add = S.add;
const page = penpot.currentPage;
page.root.children.filter(c => /^D\d\d /.test(c.name)).forEach(c => c.remove());
const W = 1280, H = 800, GAP = 120, Y0 = 3500, P = 32, CW = W - 2 * P, BODY = H - 20 - 32 - 20 - P;
const boards = [];
const mk = (i, name) => { const b = S.screen(`D0${i} ${name}`, W, H, (i - 1) * (W + GAP), Y0, { p: P, pt: 20, gap: 20, align: 'start' }); page.root.appendChild(b); boards.push(b); return b; };
const top = (b, right) => { const r = S.row('topbar', CW, 32, { justify: 'space-between', align: 'center' }); add(r, S.text('СВОЯ ИГРА', { font: 'display', size: 22, weight: 600, upper: true, color: T.accent2, letterSpacing: 3, w: 300 })); add(r, S.text(right, { size: 14, color: T.muted, align: 'right', w: 400 })); add(b, r); };
const scores = (w, me = 0) => { const c = S.col('scores', w, 3 * 56 + 2 * 8, { gap: 8, align: 'start' }); [['Аня', '300'], ['Борис', '−100'], ['Вера', '200']].forEach(([n, s], i) => { const r = S.row('score/' + n, w, 56, { px: 16, justify: 'space-between', align: 'center', fill: S.solid(i === me ? T.surface3 : T.surface), stroke: S.line(i === me ? T.accent : T.border, i === me ? 2 : 1), radius: 12 }); add(r, S.text(n, { size: 15, weight: 600, w: 200 })); add(r, S.text(s, { font: 'display', size: 28, weight: 600, color: s.startsWith('−') ? T.danger : T.accent2, align: 'right', w: 120 })); add(c, r); }); return c; };
// D01 Question: table + scores on the left, question + button on the right
{ const b = mk(1, 'Question'); top(b, 'Комната K7M3P · Аня');
  const body = S.row('body', CW, BODY, { gap: 40, align: 'start' });
  const left = S.col('left', 520, BODY, { gap: 24, align: 'start' });
  add(left, S.table('table', ['История', 'Кино', 'Наука', 'Спорт', 'Музыка'], [100, 200, 300, 400, 500], { cellW: 72, cellH: 44, gap: 8, rowGap: 12, nameW: 112, nameSize: 13, nameFont: 'sans', state: (r, c) => (r === 1 && c === 2) ? 'gold' : ((r + c) % 3 === 0) ? 'dim' : 'default' }));
  add(left, scores(520, 0));
  add(body, left);
  const RW = CW - 520 - 40;
  const right = S.col('right', RW, BODY, { gap: 20, align: 'center' });
  const hdr = S.row('header', RW, 48, { gap: 16, justify: 'space-between' }); add(hdr, S.plate(RW - 156, 48, { variant: 'default', label: 'КИНО', size: 18, font: 'sans', weight: 600, name: 'hex/theme' })); add(hdr, S.plate(140, 48, { variant: 'gold', label: '300', size: 24, name: 'hex/price' })); add(right, hdr);
  add(right, S.text('Этот режиссёр снял «Начало», «Интерстеллар» и «Оппенгеймер».', { size: 24, weight: 600, w: RW, lines: 2, align: 'center', valign: 'center', h: 72 }));
  add(right, S.bar(RW, 6, 0.62));
  add(right, S.plate(RW, 320, { variant: 'lit', label: 'ЖМИ!  (пробел)', size: 64, name: 'button/lit' }));
  add(right, S.text('пинг 12 мс · точность ±3 мс · кнопка честная: зажигается у всех одновременно', { size: 13, color: T.muted, w: RW, align: 'center' }));
  add(body, right);
  add(b, body);
}
// D02 Lobby: participants + ready on the left, chat on the right
{ const b = mk(2, 'Lobby'); top(b, 'Комната K7M3P');
  const body = S.row('body', CW, BODY, { gap: 40, align: 'start' });
  const left = S.col('left', 600, BODY, { gap: 16, align: 'start' });
  add(left, S.text('Пятничная игра', { font: 'display', size: 40, weight: 600, upper: true, color: T.accent2, w: 600, h: 52 }));
  add(left, S.text('пак «Тестовый пакет SIGame» · ведущий Игорь · кнопка Wi-Fi вечеринка', { size: 14, color: T.muted, w: 600 }));
  const list = S.col('persons', 600, 4 * 52 + 3 * 10, { gap: 10, align: 'start' });
  [['Ведущий Игорь', 'ведущий', true, false], ['Аня (вы)', 'готова', true, true], ['Борис', 'не готов', false, false], ['Вера', 'готова', true, false]].forEach(([n, s, ok, me]) => add(list, S.person(600, 52, n, s, ok, { me })));
  add(left, list);
  add(left, S.btn('Я готов', 600, 56, 'gold', { upper: true }));
  add(body, left);
  const CHW = CW - 640;
  const chat = S.card('chat', CHW, BODY, { title: 'Чат', p: 20, gap: 10, fill: S.solid(T.bg2) });
  ['Борис: всем привет!', 'Вера: погнали', 'Игорь: ждём ещё одного'].forEach(m => add(chat, S.text(m, { size: 15, w: CHW - 40 })));
  add(chat, S.spacer(1, 1), { fillH: true });
  add(chat, S.input(CHW - 40, 44, '', 'Сообщение…', { size: 14 }));
  add(body, chat);
  add(b, body);
}
return boards.map(b => ({ name: b.name, children: b.children.length }));

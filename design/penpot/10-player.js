// Player (mobile, 390×844) screens on page "Screens", row y = 0 — flex layout (helpers v2).
// Boards side by side: P01 Join · P02 Lobby · P03 Table (choose) · P04 Question + button · P05 Answer · P06 Stake · P07 Verdict.
const T = storage.T, S = storage, add = S.add;
const page = penpot.currentPage;
page.root.children.filter(c => /^P\d\d /.test(c.name)).forEach(c => c.remove());
const W = 390, H = 844, GAP = 60, CW = W - 48;
const boards = [];
const mk = (i, name) => { const b = S.screen(`P0${i} ${name}`, W, H, i * (W + GAP), 0, { p: 24, pt: 14, pb: 24, gap: 16, align: 'center' }); page.root.appendChild(b); boards.push(b); return b; };
const statusBar = (b) => { const r = S.row('statusbar', CW, 24, { justify: 'space-between', align: 'center' }); add(r, S.text('9:41', { size: 14, weight: 600, w: 80, name: 'time' })); add(r, S.text('SIGAME', { font: 'display', size: 16, weight: 600, upper: true, color: T.accent2, letterSpacing: 2, align: 'center', w: 120, name: 'brand' })); add(r, S.text('12 мс', { size: 12, color: T.success, align: 'right', w: 80, name: 'ping' })); add(b, r); };
const header = (b, theme, price) => { add(b, S.plate(CW, 44, { variant: 'default', label: theme, size: 16, font: 'sans', weight: 600, name: 'hex/theme' })); add(b, S.plate(140, 44, { variant: 'gold', label: price, size: 24, name: 'hex/price' })); };
const scoreStrip = (b, me = 0) => { add(b, S.spacer(CW, 1), { fillH: true }); const r = S.row('scores', CW, 64, { gap: 9, justify: 'space-between', align: 'center' }); [['Аня', '300'], ['Борис', '−100'], ['Вера', '200']].forEach(([n, s], i) => add(r, S.score(108, 64, n, s, i === me))); add(b, r); };
const primary = (b, label, variant = 'gold') => add(b, S.btn(label, CW, 56, variant, { upper: true }));
const twoBtns = (b, a, av, c, cv, h = 52) => { const r = S.row('actions', CW, h, { gap: 12, justify: 'space-between' }); add(r, S.btn(a, (CW - 12) / 2, h, av)); add(r, S.btn(c, (CW - 12) / 2, h, cv)); add(b, r); };
const paragraph = (b, str, o = {}) => add(b, S.text(str, { w: CW, align: 'center', ...o }));

// P01 Join
{ const b = mk(1, 'Join'); statusBar(b);
  add(b, S.spacer(CW, 40));
  paragraph(b, 'СВОЯ ИГРА', { font: 'display', size: 48, weight: 600, upper: true, color: T.accent2, letterSpacing: 4, h: 64 });
  paragraph(b, 'Введите код комнаты', { size: 16, color: T.muted });
  const code = S.row('code', CW, 62, { gap: 8, justify: 'center' }); ['K', '7', 'M', '3', 'P'].forEach((ch, i) => add(code, S.plate(62, 62, { variant: i < 3 ? 'active' : 'default', label: i < 3 ? ch : '', size: 32, name: 'code/' + i }))); add(b, code);
  add(b, S.input(CW, 52, 'Аня', 'Ваше имя'));
  const roles = S.row('roles', CW, 44, { gap: 8, justify: 'space-between' }); ['Игрок', 'Зритель', 'Ведущий'].forEach((r, i) => add(roles, S.btn(r, 108, 44, i === 0 ? 'active' : 'default'))); add(b, roles);
  primary(b, 'Войти');
  paragraph(b, 'или отсканируйте QR с экрана ведущего', { size: 13, color: T.muted });
}
// P02 Lobby
{ const b = mk(2, 'Lobby'); statusBar(b);
  add(b, S.text('Комната K7M3P', { font: 'display', size: 32, weight: 600, upper: true, color: T.accent2, w: CW, h: 42 }));
  add(b, S.text('Пятничная игра · пак «Тестовый пакет»', { size: 14, color: T.muted, w: CW }));
  const list = S.col('persons', CW, 4 * 52 + 3 * 10, { gap: 10, align: 'start' });
  [['Ведущий Игорь', 'ведущий', true, false], ['Аня (вы)', 'готова', true, true], ['Борис', 'не готов', false, false], ['Вера', 'готова', true, false]].forEach(([n, s, ok, me]) => add(list, S.person(CW, 52, n, s, ok, { me })));
  add(b, list);
  primary(b, 'Я готов');
  paragraph(b, 'Ждём, пока ведущий начнёт игру…', { size: 14, color: T.muted });
  add(b, S.spacer(CW, 1), { fillH: true });
  const chat = S.card('chat', CW, 190, { title: 'Чат', p: 16, gap: 8, fill: S.solid(T.bg2), radius: 12 });
  add(chat, S.text('Борис: всем привет!', { size: 14, w: CW - 32 })); add(chat, S.text('Вера: погнали', { size: 14, w: CW - 32 }));
  add(chat, S.spacer(1, 1), { fillH: true });
  add(chat, S.input(CW - 32, 44, '', 'Сообщение…', { size: 14 }));
  add(b, chat);
}
// P03 Table (choose a question)
{ const b = mk(3, 'Table'); statusBar(b);
  add(b, S.text('Ваш ход — выберите вопрос', { size: 16, weight: 600, color: T.accent2, w: CW }));
  const themes = ['История', 'Кино', 'Наука', 'Спорт', 'Музыка'];
  const grid = S.col('table', CW, themes.length * 72 + (themes.length - 1) * 14, { gap: 14, align: 'start' });
  themes.forEach((t, r) => {
    const sec = S.col('theme/' + t, CW, 72, { gap: 6, align: 'start' });
    add(sec, S.text(t, { size: 13, weight: 600, color: T.muted, upper: true, w: CW, h: 18 }));
    const row = S.row('prices', CW, 48, { gap: 8, justify: 'space-between' });
    [100, 200, 300, 400, 500].forEach((p, c) => { const st = (r === 1 && c === 2) ? 'active' : ((r + c) % 3 === 0) ? 'dim' : 'default'; add(row, S.plate(62, 48, { variant: st, label: st === 'dim' ? '' : String(p), size: 18, name: `cell/${r}/${c}` })); });
    add(sec, row); add(grid, sec);
  });
  add(b, grid);
  scoreStrip(b, 0);
}
// P04 Question + big button
{ const b = mk(4, 'Question'); statusBar(b); header(b, 'КИНО', '300');
  paragraph(b, 'Этот режиссёр снял «Начало», «Интерстеллар» и «Оппенгеймер».', { size: 22, weight: 600, lines: 3, valign: 'center', h: 90 });
  add(b, S.bar(CW, 6, 0.64));
  add(b, S.plate(CW, 300, { variant: 'lit', label: 'ЖМИ!', size: 72, name: 'button/lit' }));
  paragraph(b, 'пинг 12 мс · точность ±3 мс', { size: 12, color: T.muted });
  scoreStrip(b, 0);
}
// P05 Answer input
{ const b = mk(5, 'Answer'); statusBar(b); header(b, 'КИНО', '300');
  add(b, S.plate(CW, 44, { variant: 'gold', label: 'Вы первая! Отвечайте', size: 16, font: 'sans', weight: 700, name: 'hex/you' }));
  paragraph(b, 'Этот режиссёр снял «Начало», «Интерстеллар» и «Оппенгеймер».', { size: 18, color: T.muted, lines: 3, h: 72 });
  add(b, S.input(CW, 52, 'Кристофер Нолан|', '', { focus: true }));
  add(b, S.bar(CW, 6, 0.72, T.primary2, T.primary));
  paragraph(b, 'осталось 18 с', { size: 12, color: T.muted });
  primary(b, 'Ответить');
  scoreStrip(b, 0);
}
// P06 Stake dialog
{ const b = mk(6, 'Stake'); statusBar(b); header(b, 'ВОПРОС СО СТАВКОЙ', '400');
  paragraph(b, 'Ваша ставка', { size: 16, color: T.muted });
  const st = S.row('stake', CW, 72, { gap: 12, justify: 'space-between', align: 'center' });
  add(st, S.plate(90, 56, { variant: 'default', label: '−100', size: 22, name: 'btn/minus' })); add(st, S.plate(140, 72, { variant: 'gold', label: '600', size: 40, name: 'hex/stake' })); add(st, S.plate(90, 56, { variant: 'default', label: '+100', size: 22, name: 'btn/plus' })); add(b, st);
  paragraph(b, 'мин. 500 · макс. 1 300 · шаг 100', { size: 12, color: T.muted });
  primary(b, 'Ставка 600');
  twoBtns(b, 'Ва-банк', 'active', 'Пас', 'dim');
  scoreStrip(b, 0);
}
// P07 Verdict
{ const b = mk(7, 'Verdict'); statusBar(b); header(b, 'КИНО', '300');
  add(b, S.spacer(CW, 24));
  add(b, S.plate(CW, 160, { variant: 'success', label: 'ВЕРНО  +300', size: 44, name: 'verdict/right' }));
  paragraph(b, 'Правильный ответ: Кристофер Нолан', { size: 16, color: T.muted });
  twoBtns(b, 'Я прав!', 'default', 'Я против', 'default', 48);
  scoreStrip(b, 0);
}
return boards.map(b => ({ name: b.name, children: b.children.length }));

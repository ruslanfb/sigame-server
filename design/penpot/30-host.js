// Host / showman console (desktop 1440×900) on page "Screens", row y = 2400 — flex layout (helpers v2):
// H01 Create room · H02 Lobby · H03 Game (table + validation + players) · H04 Buzzer audit.
const T = storage.T, S = storage, add = S.add;
const page = penpot.currentPage;
page.root.children.filter(c => /^H\d\d /.test(c.name)).forEach(c => c.remove());
const W = 1440, H = 900, GAP = 120, Y0 = 2400, P = 40, CW = W - 2 * P, TOP = 56, BODY = H - TOP;
const boards = [];
// Each screen = top bar + body column (padding 28/40, gap 24).
const mk = (i, name, right) => {
  const b = S.screen(`H0${i} ${name}`, W, H, (i - 1) * (W + GAP), Y0, { p: 0, gap: 0, align: 'start' }); page.root.appendChild(b); boards.push(b);
  const bar = S.row('topbar', W, TOP, { px: 24, justify: 'space-between', align: 'center', fill: S.solid(T.bg, 0.8), stroke: S.line(T.border, 1) });
  add(bar, S.text('СВОЯ ИГРА · ПУЛЬТ ВЕДУЩЕГО', { font: 'display', size: 20, weight: 600, upper: true, color: T.accent2, letterSpacing: 2, w: 520 }));
  add(bar, S.text(right, { size: 14, color: T.muted, align: 'right', w: 600 }));
  add(b, bar);
  const body = S.col('body', W, BODY, { p: P, pt: 28, gap: 24, align: 'start' }); add(b, body);
  return body;
};
const panelBtn = (label, w = 160, variant = 'default', size = 15) => S.btn(label, w, 44, variant, { size });
const btnRow = (name, w, labels, activeIdx = -1, bw) => { const r = S.row(name, w, 44, { gap: 16, align: 'center' }); labels.forEach((l, i) => add(r, panelBtn(l, bw || Math.floor((w - 16 * (labels.length - 1)) / labels.length), i === activeIdx ? 'active' : 'default'))); return r; };
const playerRow = (w, name, score, st, rtt) => {
  const r = S.row('player/' + name, w, 48, { px: 14, gap: 16, justify: 'space-between', align: 'center', fill: S.solid(st === 'answering' ? T.surface3 : T.bg2), stroke: S.line(st === 'answering' ? T.accent : T.border, 1), radius: 10 });
  add(r, S.text(name, { size: 15, weight: 600, w: 10 }), { fill: true });
  if (score !== '') add(r, S.text(String(score), { font: 'display', size: 24, weight: 600, color: /^[-−]/.test(String(score)) ? T.danger : T.accent2, align: 'right', w: 100 }));
  add(r, S.text(rtt, { size: 12, color: rtt.includes('!') ? T.warning : T.muted, align: 'right', w: 260 }));
  return r;
};
const playerList = (w, rows) => { const c = S.col('players', w, rows.length * 48 + (rows.length - 1) * 8, { gap: 8, align: 'start' }); rows.forEach(([n, s, st, rtt]) => add(c, playerRow(w, n, s, st, rtt))); return c; };

// H01 Create room
{ const body = mk(1, 'Create', 'ИИ-ведущий: настроен · ffprobe: есть');
  const CH = BODY - 28 - P; // 776
  const cols = S.row('columns', CW, CH, { gap: 40, align: 'start' });
  const pack = S.card('panel/Пак', 640, CH, { title: 'Пак', gap: 16 });
  add(pack, S.field(600, 'Поиск по библиотеке', 'тест'));
  [['Тестовый пакет SIGame', '3 раунда · 31 вопрос · медиа', true], ['Кино 2024', '3 раунда · 90 вопросов', false], ['История России', '2 раунда · 60 вопросов', false], ['Загрузить .siq…', 'импорт v4/v5, отчёт совместимости', false]].forEach(([n, s, sel]) => {
    const r = S.col('pack/' + n, 600, 64, { px: 16, justify: 'center', gap: 2, align: 'start', fill: S.solid(sel ? T.surface3 : T.bg2), stroke: S.line(sel ? T.accent : T.border, sel ? 2 : 1), radius: 12 });
    add(r, S.text(n, { size: 16, weight: 600, w: 568 })); add(r, S.text(s, { size: 12, color: T.muted, w: 568 })); add(pack, r);
  });
  add(cols, pack);
  const right = S.col('right', 680, CH, { gap: 24, align: 'start' });
  const room = S.card('panel/Комната', 680, CH - 24 - 160, { title: 'Комната', gap: 16 });
  add(room, S.field(640, 'Название', 'Пятничная игра'));
  add(room, S.text('Ведущий', { size: 12, color: T.muted, w: 640 }));
  add(room, btnRow('showman', 640, ['Человек', 'ИИ', 'Гибрид'], 0, 160));
  add(room, S.text('Кнопка', { size: 12, color: T.muted, w: 640 }));
  const presets = S.col('presets', 640, 44 * 2 + 12, { gap: 12, align: 'start' });
  add(presets, btnRow('r1', 640, ['LAN (кабель)', 'Wi-Fi вечеринка'], 1, 312)); add(presets, btnRow('r2', 640, ['Интернет', 'Турнир'], -1, 312)); add(room, presets);
  const fr = S.row('fields', 640, 72, { gap: 20, align: 'start' }); add(fr, S.field(200, 'Игроков (макс.)', '6')); add(fr, S.field(420, 'Пароль', '', { placeholder: 'без пароля' })); add(room, fr);
  add(room, S.spacer(1, 1), { fillH: true });
  add(room, S.btn('Создать комнату', 640, 64, 'gold', { upper: true }));
  add(right, room);
  const after = S.card('panel/После создания', 680, 160, { title: 'После создания', gap: 12 });
  add(after, S.text('Код K7M3P · http://192.168.1.5:8080/?room=K7M3P · QR на табло', { size: 14, color: T.muted, w: 640, lines: 2 }));
  add(after, S.text('Игроки сканируют QR с телевизора или вводят код на телефоне.', { size: 14, color: T.muted, w: 640 }));
  add(right, after);
  add(cols, right);
  add(body, cols);
}
// H02 Lobby
{ const body = mk(2, 'Lobby', 'Комната K7M3P · пак «Тестовый пакет SIGame»');
  const cols = S.row('columns', CW, 560, { gap: 40, align: 'start' });
  const parts = S.card('panel/Участники', 900, 560, { title: 'Участники', gap: 16 });
  add(parts, playerList(860, [['Ведущий Игорь (вы)', '', '', 'хост'], ['Аня', 0, '', 'готова · 12 мс'], ['Борис', 0, '', 'не готов · 48 мс'], ['Вера', 0, '', 'готова · 31 мс'], ['Зритель: Телевизор', '', '', 'табло']]));
  add(parts, S.spacer(1, 1), { fillH: true });
  add(parts, btnRow('actions', 860, ['Кик', 'Бан', 'Передать хост'], -1, 160));
  add(cols, parts);
  const settings = S.card('panel/Настройки', 420, 560, { title: 'Настройки', gap: 16 });
  add(settings, S.text('Ведущий: человек\nКнопка: Wi-Fi вечеринка\nФальстарты: да · Апелляции: да\nВремя на ответ: 25 с', { size: 14, w: 380, lines: 4, h: 100 }));
  add(settings, panelBtn('Изменить'));
  add(cols, settings);
  add(body, cols);
  add(body, S.btn('Начать игру', CW, 72, 'gold', { upper: true, size: 22 }));
  const chat = S.card('panel/Чат', CW, 84, { title: 'Чат', gap: 8 }); add(chat, S.text('Борис: всем привет!  ·  Вера: погнали', { size: 14, w: CW - 40 })); add(body, chat);
}
// H03 Game: table + validation + players
{ const body = mk(3, 'Game', 'Раунд 1 · вопрос 7/30 · КИНО 300');
  const cols = S.row('columns', CW, 500, { gap: 40, align: 'start' });
  const tbl = S.card('panel/Таблица', 760, 500, { title: 'Таблица (клик — вопрос; ⌥ — убрать/вернуть)', gap: 16 });
  add(tbl, S.table('table', ['История', 'Кино', 'Наука', 'Спорт', 'Музыка', 'География'], [100, 200, 300, 400, 500], { cellW: 108, cellH: 48, gap: 10, rowGap: 12, nameW: 120, nameSize: 13, nameFont: 'sans', priceSize: 22, state: (r, c) => (r === 1 && c === 2) ? 'gold' : ((r + c) % 4 === 0) ? 'dim' : 'default' }));
  add(cols, tbl);
  const val = S.card('panel/Проверка ответа', 560, 500, { title: 'Проверка ответа', gap: 12 });
  add(val, S.text('Аня отвечает', { size: 14, color: T.accent2, weight: 600, w: 520 }));
  add(val, S.plate(520, 72, { variant: 'active', label: 'Кристофер Нолан', size: 28, font: 'display', weight: 500, name: 'hex/answer' }));
  add(val, S.text('Эталон: Кристофер Нолан · Нолан\nНеверные: Спилберг', { size: 14, color: T.muted, w: 520, lines: 2 }));
  add(val, S.text('ИИ: верно (0.98) — «полное имя режиссёра совпадает с эталоном»', { size: 13, color: T.info, w: 520, lines: 2 }));
  const vb = S.row('verdict', 520, 44, { gap: 16 }); add(vb, panelBtn('Верно', 160, 'gold', 16)); add(vb, panelBtn('Неверно', 160, 'default', 16)); add(vb, panelBtn('½ балла', 160, 'default', 16)); add(val, vb);
  add(val, S.bar(520, 8, 0.7, T.primary2, T.primary));
  add(val, S.text('решение ведущего: 21 с', { size: 12, color: T.muted, w: 520 }));
  add(val, S.spacer(1, 1), { fillH: true });
  const flow = S.col('flow', 520, 44 * 2 + 8, { gap: 8, align: 'start' }); add(flow, btnRow('f1', 520, ['Пауза', 'Дальше'], -1, 252)); add(flow, btnRow('f2', 520, ['Вернуть вопрос', 'Сменить чузера'], -1, 252)); add(val, flow);
  add(cols, val);
  add(body, cols);
  const pl = S.card('panel/Игроки', CW, 252, { title: 'Игроки · счёт · кнопка', gap: 12 });
  add(pl, playerList(CW - 40, [['Аня', 300, 'answering', 'отвечает · 12 мс · good'], ['Борис', '−100', '', 'ошибся · 48 мс · fair'], ['Вера', 200, '', 'ждёт · 31 мс · good !flag']]));
  add(body, pl);
}
// H04 Buzzer audit
{ const body = mk(4, 'Buzzer', 'Аудит кнопки · вопрос КИНО 300');
  const res = S.card('panel/Результат', CW, 380, { title: 'Результат нажатия (arm 7f3a…): победила Аня, разрыв 42 мс, правило single', gap: 12 });
  const grid = S.col('audit', CW - 40, 36 + 3 * 40, { gap: 0, align: 'start' });
  const hdr = S.row('head', CW - 40, 36, { align: 'center' }); ['Игрок', 'Реакция', 'Источник', 'RTT', '±u', 'Флаги'].forEach(c => add(hdr, S.text(c, { size: 12, color: T.muted, upper: true, letterSpacing: 1, w: 220 }))); add(grid, hdr);
  [['Аня', '212 мс', 'client', '12 мс', '3 мс', '—'], ['Борис', '254 мс', 'client', '48 мс', '9 мс', '—'], ['Вера', '— (не нажала)', '', '31 мс', '6 мс', 'early_bias']].forEach(row => { const r = S.row('row', CW - 40, 40, { align: 'center' }); row.forEach((v, i) => add(r, S.text(v || ' ', { size: 15, weight: i === 0 ? 600 : 500, color: v.includes('_') ? T.warning : T.text, w: 220 }))); add(grid, r); });
  add(res, grid);
  add(res, S.spacer(1, 1), { fillH: true });
  add(res, btnRow('actions', CW - 40, ['Переоткрыть кнопку', 'Доверие: только сервер', 'Скачать лог'], -1, 244));
  add(body, res);
  const q = S.card('panel/Качество', CW, 360, { title: 'Качество соединения (обновляется каждые 5 с)', gap: 12 });
  ['Аня · 12 мс · джиттер 1 мс · good · доверие полное', 'Борис · 48 мс · джиттер 6 мс · fair · доверие полное', 'Вера · 31 мс · джиттер 4 мс · good · флаг early_bias ×2 — осторожно'].forEach(l => add(q, S.text(l, { size: 15, color: l.includes('флаг') ? T.warning : T.text, w: CW - 40 })));
  add(q, S.spacer(1, 1), { fillH: true });
  add(q, S.text('Пресет: Wi-Fi вечеринка · окно сбора до 400 мс · тай-брейк: likelihood', { size: 13, color: T.muted, w: CW - 40 }));
  add(body, q);
}
return boards.map(b => ({ name: b.name, children: b.children.length }));

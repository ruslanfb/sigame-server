// Player (mobile, 390×844) screens on page "Player". Requires storage helpers (02-helpers.js) and the page to be active.
// Boards side by side: 01 Join · 02 Lobby · 03 Table (choose) · 04 Question + button · 05 Answer · 06 Stake · 07 Verdict.
const T = storage.T;
const page = penpot.currentPage;
page.root.children.filter(c => /^P\d\d /.test(c.name)).forEach(c => c.remove());
const W = 390, H = 844, GAP = 60;
const boards = [];
const mk = (i, name) => { const b = storage.bgBoard(`P0${i} ${name}`, W, H, i * (W + GAP), 0); page.root.appendChild(b); boards.push(b); return b; };
const statusBar = (b) => { b.appendChild(storage.text('9:41', 24, 14, { size: 14, weight: 600, name: 'time' })); b.appendChild(storage.text('SIGAME', W / 2 - 36, 12, { font: 'display', size: 16, weight: 600, upper: true, color: T.accent2, letterSpacing: 2, name: 'brand' })); };
const header = (b, theme, price) => {
  storage.hexPlate(b, 24, 56, W - 48, 44, { variant: 'default', label: theme, size: 16, font: 'sans', weight: 600, name: 'hex/theme' });
  storage.hexPlate(b, W / 2 - 60, 108, 120, 44, { variant: 'gold', label: price, size: 24, name: 'hex/price' });
};
const scoreStrip = (b, me = 1) => {
  const names = ['Аня', 'Борис', 'Вера'], scores = ['300', '-100', '200'];
  names.forEach((n, i) => {
    const x = 24 + i * 116, y = H - 96;
    const r = penpot.createRectangle(); r.x = x; r.y = y; r.resize(108, 64); r.borderRadius = 12; r.fills = [{ fillColor: i === me ? T.surface3 : T.surface, fillOpacity: 1 }]; r.strokes = [{ strokeColor: i === me ? T.accent : T.border, strokeOpacity: 1, strokeWidth: i === me ? 2 : 1 }]; r.name = 'score/' + n; b.appendChild(r);
    b.appendChild(storage.text(n, x + 12, y + 10, { size: 13, color: T.muted }));
    b.appendChild(storage.text(scores[i], x + 12, y + 28, { font: 'display', size: 24, weight: 600, color: scores[i].startsWith('-') ? T.danger : T.text }));
  });
};
const primaryButton = (b, label, y, variant = 'gold') => { storage.hexPlate(b, 24, y, W - 48, 56, { variant, label, size: 20, font: 'sans', weight: 700, upper: true, name: 'btn/' + label }); };
const input = (b, placeholder, y, value) => {
  const r = penpot.createRectangle(); r.x = 24; r.y = y; r.resize(W - 48, 52); r.borderRadius = 12; r.fills = [{ fillColor: T.bg2, fillOpacity: 1 }]; r.strokes = [{ strokeColor: T.border, strokeOpacity: 1, strokeWidth: 1 }]; r.name = 'input'; b.appendChild(r);
  b.appendChild(storage.text(value || placeholder, 40, y + 16, { size: 16, color: value ? T.text : T.muted }));
};

// 01 Join
{ const b = mk(1, 'Join'); statusBar(b);
  b.appendChild(storage.text('СВОЯ ИГРА', 24, 120, { font: 'display', size: 48, weight: 600, upper: true, color: T.accent2, w: W - 48, align: 'center' }));
  b.appendChild(storage.text('Введите код комнаты', 24, 200, { size: 16, color: T.muted, w: W - 48, align: 'center' }));
  ['K','7','M','3','P'].forEach((ch, i) => storage.hexPlate(b, 24 + i * 70, 240, 62, 62, { variant: i < 3 ? 'active' : 'default', label: i < 3 ? ch : '', size: 32, name: 'code/' + i }));
  input(b, 'Ваше имя', 330, 'Аня');
  ['Игрок', 'Зритель', 'Ведущий'].forEach((r, i) => storage.hexPlate(b, 24 + i * 116, 400, 108, 44, { variant: i === 0 ? 'active' : 'default', label: r, size: 15, font: 'sans', weight: 600, name: 'role/' + r }));
  primaryButton(b, 'Войти', 470);
  b.appendChild(storage.text('или отсканируйте QR с экрана ведущего', 24, 550, { size: 13, color: T.muted, w: W - 48, align: 'center' }));
}
// 02 Lobby
{ const b = mk(2, 'Lobby'); statusBar(b);
  b.appendChild(storage.text('Комната K7M3P', 24, 60, { font: 'display', size: 32, weight: 600, upper: true, color: T.accent2 }));
  b.appendChild(storage.text('Пятничная игра · пак «Тестовый пакет»', 24, 104, { size: 14, color: T.muted, w: W - 48 }));
  const people = [['Ведущий Игорь', 'ведущий', true], ['Аня (вы)', 'готова', true], ['Борис', 'не готов', false], ['Вера', 'готова', true]];
  people.forEach(([n, s, ok], i) => {
    const y = 150 + i * 64; const r = penpot.createRectangle(); r.x = 24; r.y = y; r.resize(W - 48, 52); r.borderRadius = 12; r.fills = [{ fillColor: T.surface, fillOpacity: 1 }]; r.strokes = [{ strokeColor: T.border, strokeOpacity: 1, strokeWidth: 1 }]; r.name = 'person'; b.appendChild(r);
    const dot = penpot.createEllipse(); dot.x = 40; dot.y = y + 20; dot.resize(12, 12); dot.fills = [{ fillColor: ok ? T.success : T.muted, fillOpacity: 1 }]; b.appendChild(dot);
    b.appendChild(storage.text(n, 64, y + 16, { size: 16, weight: 600 })); b.appendChild(storage.text(s, W - 120, y + 18, { size: 13, color: ok ? T.success : T.muted }));
  });
  primaryButton(b, 'Я готов', 430);
  b.appendChild(storage.text('Ждём, пока ведущий начнёт игру…', 24, 500, { size: 14, color: T.muted, w: W - 48, align: 'center' }));
  const chat = penpot.createRectangle(); chat.x = 24; chat.y = 560; chat.resize(W - 48, 180); chat.borderRadius = 12; chat.fills = [{ fillColor: T.bg2, fillOpacity: 1 }]; chat.strokes = [{ strokeColor: T.border, strokeOpacity: 1, strokeWidth: 1 }]; chat.name = 'chat'; b.appendChild(chat);
  b.appendChild(storage.text('Чат', 40, 572, { size: 12, color: T.muted, upper: true }));
  b.appendChild(storage.text('Борис: всем привет!', 40, 596, { size: 14 })); b.appendChild(storage.text('Вера: погнали', 40, 620, { size: 14 }));
  input(b, 'Сообщение…', 680);
}
// 03 Table (choose a question)
{ const b = mk(3, 'Table'); statusBar(b);
  b.appendChild(storage.text('Ваш ход — выберите вопрос', 24, 56, { size: 16, weight: 600, color: T.accent2, w: W - 48 }));
  const themes = ['История', 'Кино', 'Наука', 'Спорт', 'Музыка'];
  themes.forEach((t, r) => {
    const y = 100 + r * 120;
    b.appendChild(storage.text(t, 24, y, { size: 13, weight: 600, color: T.muted, upper: true }));
    [100, 200, 300, 400, 500].forEach((p, c) => storage.hexPlate(b, 24 + c * 70, y + 22, 62, 48, { variant: (r === 1 && c === 2) ? 'active' : ((r + c) % 3 === 0) ? 'dim' : 'default', label: (r + c) % 3 === 0 ? '' : String(p), size: 18, name: `cell/${r}/${c}` }));
  });
  scoreStrip(b, 0);
}
// 04 Question + big button
{ const b = mk(4, 'Question'); statusBar(b); header(b, 'КИНО', '300');
  b.appendChild(storage.text('Этот режиссёр снял «Начало», «Интерстеллар» и «Оппенгеймер».', 24, 176, { size: 22, weight: 600, w: W - 48, h: 120, align: 'center' }));
  const bar = penpot.createRectangle(); bar.x = 24; bar.y = 300; bar.resize(W - 48, 6); bar.borderRadius = 3; bar.fills = [{ fillColor: T.bg2, fillOpacity: 1 }]; b.appendChild(bar);
  const fill = penpot.createRectangle(); fill.x = 24; fill.y = 300; fill.resize(220, 6); fill.borderRadius = 3; fill.fills = [storage.vgrad(T.accent2, T.accent)]; fill.name = 'timer'; b.appendChild(fill);
  const p = storage.hexPath(24, 360, W - 48, 300); p.name = 'button/lit'; p.fills = [storage.vgrad('#FFFFFF', T.accent2)]; p.strokes = [{ strokeColor: '#FFFFFF', strokeOpacity: 1, strokeWidth: 4, strokeAlignment: 'inner' }]; p.shadows = [{ style: 'drop-shadow', offsetX: 0, offsetY: 0, blur: 60, spread: 0, color: { color: T.accent, opacity: 0.7 } }]; b.appendChild(p);
  const t = storage.text('ЖМИ!', 24, 360, { font: 'display', size: 72, weight: 600, color: T.accentContrast, align: 'center', w: W - 48, h: 300 }); t.verticalAlign = 'center'; b.appendChild(t);
  b.appendChild(storage.text('пинг 12 мс · точность ±3 мс', 24, 680, { size: 12, color: T.muted, w: W - 48, align: 'center' }));
  scoreStrip(b, 0);
}
// 05 Answer input
{ const b = mk(5, 'Answer'); statusBar(b); header(b, 'КИНО', '300');
  storage.hexPlate(b, 24, 176, W - 48, 44, { variant: 'gold', label: 'Вы первая! Отвечайте', size: 16, font: 'sans', weight: 700, name: 'hex/you' });
  b.appendChild(storage.text('Этот режиссёр снял «Начало», «Интерстеллар» и «Оппенгеймер».', 24, 240, { size: 18, w: W - 48, h: 90, color: T.muted, align: 'center' }));
  input(b, 'Ваш ответ…', 350, 'Кристофер Нолан|');
  const bar = penpot.createRectangle(); bar.x = 24; bar.y = 416; bar.resize(W - 48, 6); bar.borderRadius = 3; bar.fills = [{ fillColor: T.bg2, fillOpacity: 1 }]; b.appendChild(bar);
  const fill = penpot.createRectangle(); fill.x = 24; fill.y = 416; fill.resize(280, 6); fill.borderRadius = 3; fill.fills = [storage.vgrad(T.primary2, T.primary)]; fill.name = 'timer'; b.appendChild(fill);
  b.appendChild(storage.text('осталось 18 с', 24, 430, { size: 12, color: T.muted, w: W - 48, align: 'center' }));
  primaryButton(b, 'Ответить', 470);
  scoreStrip(b, 0);
}
// 06 Stake dialog
{ const b = mk(6, 'Stake'); statusBar(b); header(b, 'ВОПРОС СО СТАВКОЙ', '400');
  b.appendChild(storage.text('Ваша ставка', 24, 176, { size: 16, color: T.muted, w: W - 48, align: 'center' }));
  storage.hexPlate(b, W / 2 - 100, 208, 200, 72, { variant: 'gold', label: '600', size: 40, name: 'hex/stake' });
  storage.hexPlate(b, 24, 300, 100, 56, { variant: 'default', label: '−100', size: 22, name: 'btn/minus' });
  storage.hexPlate(b, W - 124, 300, 100, 56, { variant: 'default', label: '+100', size: 22, name: 'btn/plus' });
  b.appendChild(storage.text('мин. 500 · макс. 1 300 · шаг 100', 24, 372, { size: 12, color: T.muted, w: W - 48, align: 'center' }));
  primaryButton(b, 'Ставка 600', 410);
  storage.hexPlate(b, 24, 480, (W - 60) / 2, 52, { variant: 'active', label: 'Ва-банк', size: 18, font: 'sans', weight: 700, name: 'btn/allin' });
  storage.hexPlate(b, 36 + (W - 60) / 2, 480, (W - 60) / 2, 52, { variant: 'dim', label: 'Пас', size: 18, font: 'sans', weight: 700, name: 'btn/pass' });
  scoreStrip(b, 0);
}
// 07 Verdict
{ const b = mk(7, 'Verdict'); statusBar(b); header(b, 'КИНО', '300');
  const p = storage.hexPath(24, 220, W - 48, 160); p.name = 'verdict/right'; p.fills = [storage.vgrad('#3BE08A', '#1FA85E')]; p.strokes = [{ strokeColor: '#8CF5BF', strokeOpacity: 1, strokeWidth: 3, strokeAlignment: 'inner' }]; p.shadows = [{ style: 'drop-shadow', offsetX: 0, offsetY: 0, blur: 48, spread: 0, color: { color: T.success, opacity: 0.6 } }]; b.appendChild(p);
  const t = storage.text('ВЕРНО\n+300', 24, 220, { font: 'display', size: 44, weight: 600, color: T.accentContrast, align: 'center', w: W - 48, h: 160 }); t.verticalAlign = 'center'; b.appendChild(t);
  b.appendChild(storage.text('Правильный ответ: Кристофер Нолан', 24, 410, { size: 16, color: T.muted, w: W - 48, align: 'center' }));
  storage.hexPlate(b, 24, 470, (W - 60) / 2, 48, { variant: 'default', label: 'Я прав!', size: 15, font: 'sans', weight: 600, name: 'btn/appeal' });
  storage.hexPlate(b, 36 + (W - 60) / 2, 470, (W - 60) / 2, 48, { variant: 'default', label: 'Я против', size: 15, font: 'sans', weight: 600, name: 'btn/against' });
  scoreStrip(b, 0);
}
return boards.map(b => ({ name: b.name, children: b.children.length }));

// Player desktop variant (1280×800) on page "Screens", row y = 3500: D01 Question (button + table side by side) · D02 Lobby.
// Requires storage helpers (02-helpers.js incl. fixBoard) and the page to be active.
const T = storage.T, A = storage.add;
const page = penpot.currentPage;
page.root.children.filter(c => /^D\d\d /.test(c.name)).forEach(c => c.remove());
const W = 1280, H = 800, GAP = 120, Y0 = 3500;
const boards = [];
const mk = (i, name) => { const b = storage.bgBoard(`D0${i} ${name}`, W, H, (i - 1) * (W + GAP), Y0); page.root.appendChild(b); boards.push(b); return b; };
const top = (b, right) => { A(b, storage.text('СВОЯ ИГРА', 32, 20, { font: 'display', size: 22, weight: 600, upper: true, color: T.accent2, letterSpacing: 3 })); A(b, storage.text(right, W - 360, 22, { size: 14, color: T.muted, w: 328, align: 'right' })); };
const scores = (b, x, y, w, me = 0) => [['Аня', '300'], ['Борис', '-100'], ['Вера', '200']].forEach(([n, s], i) => {
  const yy = y + i * 64; const r = penpot.createRectangle(); r.x = x; r.y = yy; r.resize(w, 56); r.borderRadius = 12; r.fills = [{ fillColor: i === me ? T.surface3 : T.surface, fillOpacity: 1 }]; r.strokes = [{ strokeColor: i === me ? T.accent : T.border, strokeOpacity: 1, strokeWidth: i === me ? 2 : 1 }]; r.name = 'score/' + n; b.appendChild(r);
  A(b, storage.text(n, x + 16, yy + 18, { size: 15, weight: 600 })); A(b, storage.text(s, x + w - 90, yy + 12, { font: 'display', size: 28, weight: 600, color: s.startsWith('-') ? T.danger : T.accent2 }));
});
{ const b = mk(1, 'Question'); top(b, 'Комната K7M3P · Аня');
  // left: compact table
  const themes = ['История', 'Кино', 'Наука', 'Спорт', 'Музыка'];
  themes.forEach((t, r) => { const y = 80 + r * 64; A(b, storage.text(t, 32, y + 8, { size: 12, weight: 600, color: T.muted, upper: true })); [100, 200, 300, 400, 500].forEach((p, c) => storage.hexPlate(b, 140 + c * 80, y, 72, 40, { variant: (r === 1 && c === 2) ? 'gold' : ((r + c) % 3 === 0) ? 'dim' : 'default', label: (r + c) % 3 === 0 ? '' : String(p), size: 16, name: `cell/${r}/${c}` })); });
  scores(b, 32, 420, 500, 0);
  // right: question + button
  storage.hexPlate(b, 600, 80, 500, 48, { variant: 'default', label: 'КИНО', size: 18, font: 'sans', weight: 600, name: 'hex/theme' });
  storage.hexPlate(b, 1116, 80, 132, 48, { variant: 'gold', label: '300', size: 24, name: 'hex/price' });
  A(b, storage.text('Этот режиссёр снял «Начало», «Интерстеллар» и «Оппенгеймер».', 600, 150, { size: 24, weight: 600, w: 648, h: 90, align: 'center' }));
  const bar = penpot.createRectangle(); bar.x = 600; bar.y = 260; bar.resize(648, 6); bar.borderRadius = 3; bar.fills = [{ fillColor: T.bg2, fillOpacity: 1 }]; b.appendChild(bar);
  const f = penpot.createRectangle(); f.x = 600; f.y = 260; f.resize(400, 6); f.borderRadius = 3; f.fills = [storage.vgrad(T.accent2, T.accent)]; f.name = 'timer'; b.appendChild(f);
  const p = storage.hexPath(600, 300, 648, 320); p.name = 'button/lit'; p.fills = [storage.vgrad('#FFFFFF', T.accent2)]; p.strokes = [{ strokeColor: '#FFFFFF', strokeOpacity: 1, strokeWidth: 4, strokeAlignment: 'inner' }]; p.shadows = [{ style: 'drop-shadow', offsetX: 0, offsetY: 0, blur: 60, spread: 0, color: { color: T.accent, opacity: 0.7 } }]; b.appendChild(p);
  const t = storage.text('ЖМИ!  (пробел)', 600, 300, { font: 'display', size: 64, weight: 600, color: T.accentContrast, align: 'center', w: 648, h: 320 }); if (t) { t.verticalAlign = 'center'; b.appendChild(t); }
  A(b, storage.text('пинг 12 мс · точность ±3 мс · кнопка честная: зажигается у всех одновременно', 600, 640, { size: 13, color: T.muted, w: 648, align: 'center' }));
}
{ const b = mk(2, 'Lobby'); top(b, 'Комната K7M3P');
  A(b, storage.text('Пятничная игра', 32, 80, { font: 'display', size: 40, weight: 600, upper: true, color: T.accent2 }));
  A(b, storage.text('пак «Тестовый пакет SIGame» · ведущий Игорь · кнопка Wi-Fi вечеринка', 32, 132, { size: 14, color: T.muted, w: 600 }));
  [['Ведущий Игорь', 'ведущий', true], ['Аня (вы)', 'готова', true], ['Борис', 'не готов', false], ['Вера', 'готова', true]].forEach(([n, s, ok], i) => {
    const y = 180 + i * 64; const r = penpot.createRectangle(); r.x = 32; r.y = y; r.resize(600, 52); r.borderRadius = 12; r.fills = [{ fillColor: T.surface, fillOpacity: 1 }]; r.strokes = [{ strokeColor: T.border, strokeOpacity: 1, strokeWidth: 1 }]; b.appendChild(r);
    const d = penpot.createEllipse(); d.x = 48; d.y = y + 20; d.resize(12, 12); d.fills = [{ fillColor: ok ? T.success : T.muted, fillOpacity: 1 }]; b.appendChild(d);
    A(b, storage.text(n, 72, y + 16, { size: 16, weight: 600 })); A(b, storage.text(s, 520, y + 18, { size: 13, color: ok ? T.success : T.muted }));
  });
  storage.hexPlate(b, 32, 460, 600, 56, { variant: 'gold', label: 'Я готов', size: 20, font: 'sans', weight: 700, upper: true, name: 'btn/ready' });
  const chat = penpot.createRectangle(); chat.x = 680; chat.y = 80; chat.resize(568, 640); chat.borderRadius = 16; chat.fills = [{ fillColor: T.bg2, fillOpacity: 1 }]; chat.strokes = [{ strokeColor: T.border, strokeOpacity: 1, strokeWidth: 1 }]; chat.name = 'chat'; b.appendChild(chat);
  A(b, storage.text('ЧАТ', 700, 96, { size: 12, color: T.muted, letterSpacing: 2 }));
  A(b, storage.text('Борис: всем привет!\nВера: погнали\nИгорь: ждём ещё одного', 700, 130, { size: 15, w: 528, h: 90 }));
}
boards.forEach(b => storage.fixBoard(b));
return boards.map(b => ({ name: b.name, children: b.children.length }));

// Table / TV screens (1920×1080) on page "Table": T01 Lobby · T02 Table · T03 Question · T04 Reveal · T05 Winner.
// Requires storage helpers (02-helpers.js) and the page to be active.
const T = storage.T, A = storage.add;
const page = penpot.currentPage;
page.root.children.filter(c => /^T\d\d /.test(c.name)).forEach(c => c.remove());
const W = 1920, H = 1080, GAP = 120;
const boards = [];
const mk = (i, name) => { const b = storage.bgBoard(`T0${i} ${name}`, W, H, i * (W + GAP), 0); page.root.appendChild(b); boards.push(b); return b; };
const brand = (b) => { A(b, storage.text('СВОЯ ИГРА', 48, 28, { font: 'display', size: 28, weight: 600, upper: true, color: T.accent2, letterSpacing: 4, name: 'brand' })); A(b, storage.text('K7M3P', W - 200, 28, { font: 'display', size: 28, weight: 600, color: T.muted, letterSpacing: 4, name: 'code' })); };
const players = (b, active = -1, states = {}) => {
  const names = ['Аня', 'Борис', 'Вера', 'Глеб'], scores = ['1 300', '-200', '800', '400'];
  const w = 400, gap = 24, x0 = (W - (names.length * w + (names.length - 1) * gap)) / 2, y = H - 160;
  names.forEach((n, i) => {
    const x = x0 + i * (w + gap); const st = states[i];
    const variant = i === active ? 'gold' : st === 'right' ? 'active' : 'default';
    storage.hexPlate(b, x, y, w, 120, { variant, name: 'player/' + n });
    A(b, storage.text(n, x, y + 18, { font: 'display', size: 34, weight: 500, color: variant === 'gold' ? T.accentContrast : T.text, align: 'center', w, h: 40 }));
    A(b, storage.text(scores[i], x, y + 62, { font: 'display', size: 40, weight: 600, color: variant === 'gold' ? T.accentContrast : scores[i].startsWith('-') ? T.danger : T.accent2, align: 'center', w, h: 48 }));
    if (st === 'wrong') { const d = penpot.createEllipse(); d.x = x + w - 44; d.y = y + 16; d.resize(20, 20); d.fills = [{ fillColor: T.danger, fillOpacity: 1 }]; d.name = 'state/wrong'; b.appendChild(d); }
  });
};
const timer = (b, y, frac, color = T.accent) => { const bar = penpot.createRectangle(); bar.x = 240; bar.y = y; bar.resize(W - 480, 10); bar.borderRadius = 5; bar.fills = [{ fillColor: T.bg2, fillOpacity: 1 }]; b.appendChild(bar); const f = penpot.createRectangle(); f.x = 240; f.y = y; f.resize((W - 480) * frac, 10); f.borderRadius = 5; f.fills = [storage.vgrad(T.accent2, color)]; f.name = 'timer'; f.shadows = [{ style: 'drop-shadow', offsetX: 0, offsetY: 0, blur: 16, spread: 0, color: { color, opacity: 0.6 } }]; b.appendChild(f); };

{ const b = mk(1, 'Lobby'); brand(b);
  A(b, storage.text('СВОЯ ИГРА', 0, 200, { font: 'display', size: 160, weight: 600, upper: true, color: T.accent2, w: W, h: 180, align: 'center', letterSpacing: 12 }));
  A(b, storage.text('Пятничная игра', 0, 390, { size: 40, weight: 600, color: T.text, w: W, h: 48, align: 'center' }));
  A(b, storage.text('Подключайтесь: откройте адрес и введите код', 0, 470, { size: 28, color: T.muted, w: W, h: 36, align: 'center' }));
  storage.hexPlate(b, W / 2 - 280, 530, 560, 120, { variant: 'gold', label: 'K7M3P', size: 72, name: 'hex/code' });
  A(b, storage.text('http://192.168.1.5:8080/?room=K7M3P', 0, 680, { size: 28, color: T.primary2, w: W, h: 36, align: 'center' }));
  const qr = penpot.createRectangle(); qr.x = W - 320; qr.y = 200; qr.resize(240, 240); qr.borderRadius = 16; qr.fills = [{ fillColor: '#FFFFFF', fillOpacity: 1 }]; qr.name = 'qr'; b.appendChild(qr);
  A(b, storage.text('QR', W - 320, 300, { font: 'display', size: 40, color: T.bg, w: 240, h: 48, align: 'center' }));
  players(b, -1);
}
{ const b = mk(2, 'Table'); brand(b);
  A(b, storage.text('Раунд 1', 0, 24, { font: 'display', size: 32, weight: 500, upper: true, color: T.muted, w: W, h: 40, align: 'center' }));
  const themes = ['История', 'Кино', 'Наука', 'Спорт', 'Музыка', 'География'];
  const rowH = 108, top = 90, nameW = 420, cellW = 230, cellH = 88, gap = 16, x0 = 120;
  themes.forEach((t, r) => {
    const y = top + r * rowH;
    storage.hexPlate(b, x0, y, nameW, cellH, { variant: 'default', label: t, size: 34, font: 'display', weight: 500, upper: true, name: 'theme/' + t });
    [100, 200, 300, 400, 500].forEach((p, c) => storage.hexPlate(b, x0 + nameW + 24 + c * (cellW + gap), y, cellW, cellH, { variant: (r === 1 && c === 2) ? 'gold' : ((r + c) % 4 === 0) ? 'dim' : 'default', label: (r + c) % 4 === 0 ? '' : String(p), size: 44, name: `cell/${r}/${c}` }));
  });
  players(b, -1, { 0: 'right' });
}
{ const b = mk(3, 'Question'); brand(b);
  storage.hexPlate(b, W / 2 - 360, 80, 520, 80, { variant: 'default', label: 'КИНО', size: 40, name: 'hex/theme' });
  storage.hexPlate(b, W / 2 + 180, 80, 180, 80, { variant: 'gold', label: '300', size: 44, name: 'hex/price' });
  const media = penpot.createRectangle(); media.x = 240; media.y = 190; media.resize(880, 520); media.borderRadius = 20; media.fills = [{ fillColor: T.bg2, fillOpacity: 1 }]; media.strokes = [{ strokeColor: T.border, strokeOpacity: 1, strokeWidth: 2 }]; media.name = 'media'; b.appendChild(media);
  A(b, storage.text('▶ медиа: картинка / видео / аудио', 240, 430, { size: 28, color: T.muted, w: 880, h: 36, align: 'center' }));
  A(b, storage.text('Этот режиссёр снял «Начало», «Интерстеллар» и «Оппенгеймер».', 1160, 200, { size: 44, weight: 600, w: 520, h: 300 }));
  timer(b, 740, 0.62);
  storage.hexPlate(b, W / 2 - 300, 780, 600, 90, { variant: 'gold', label: 'Отвечает Аня', size: 40, font: 'display', weight: 500, name: 'hex/answerer' });
  players(b, 0, { 1: 'wrong' });
}
{ const b = mk(4, 'Reveal'); brand(b);
  storage.hexPlate(b, W / 2 - 360, 80, 520, 80, { variant: 'default', label: 'КИНО', size: 40, name: 'hex/theme' });
  storage.hexPlate(b, W / 2 + 180, 80, 180, 80, { variant: 'gold', label: '300', size: 44, name: 'hex/price' });
  A(b, storage.text('Этот режиссёр снял «Начало», «Интерстеллар» и «Оппенгеймер».', 240, 220, { size: 40, color: T.muted, w: W - 480, h: 120, align: 'center' }));
  A(b, storage.text('ПРАВИЛЬНЫЙ ОТВЕТ', 0, 400, { font: 'display', size: 28, color: T.muted, upper: true, letterSpacing: 6, w: W, h: 36, align: 'center' }));
  storage.hexPlate(b, W / 2 - 520, 450, 1040, 150, { variant: 'gold', label: 'Кристофер Нолан', size: 80, font: 'display', weight: 600, name: 'hex/answer' });
  storage.hexPlate(b, W / 2 - 220, 640, 440, 80, { variant: 'active', label: 'Аня: верно  +300', size: 34, font: 'display', weight: 500, name: 'hex/verdict' });
  players(b, 0, { 1: 'wrong' });
}
{ const b = mk(5, 'Winner'); brand(b);
  A(b, storage.text('ПОБЕДИТЕЛЬ', 0, 220, { font: 'display', size: 48, color: T.muted, upper: true, letterSpacing: 12, w: W, h: 60, align: 'center' }));
  const p = storage.hexPath(W / 2 - 600, 320, 1200, 240); p.name = 'hex/winner'; p.fills = [storage.vgrad(T.goldTop, T.goldBottom)]; p.strokes = [{ strokeColor: '#FFFFFF', strokeOpacity: 1, strokeWidth: 4, strokeAlignment: 'inner' }]; p.shadows = [{ style: 'drop-shadow', offsetX: 0, offsetY: 0, blur: 120, spread: 0, color: { color: T.accent, opacity: 0.8 } }]; b.appendChild(p);
  const t = storage.text('АНЯ', W / 2 - 600, 320, { font: 'display', size: 160, weight: 600, color: T.accentContrast, align: 'center', w: 1200, h: 240 }); if (t) { t.verticalAlign = 'center'; b.appendChild(t); }
  A(b, storage.text('1 300 очков', 0, 600, { font: 'display', size: 64, weight: 500, color: T.accent2, w: W, h: 80, align: 'center' }));
  players(b, 0);
}
storage.tableBoards = boards.map(b => ({ id: b.id, name: b.name }));
boards.forEach(b => storage.fixBoard(b));
return boards.map(b => ({ name: b.name, children: b.children.length }));

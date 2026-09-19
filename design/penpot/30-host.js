// Host / showman console (desktop 1440×900) on page "Screens", row y = 2400:
// H01 Create room · H02 Lobby · H03 Game (table + validation + buzzer quality) · H04 Buzzer audit.
// Requires storage helpers (02-helpers.js incl. fixBoard) and the page to be active.
const T = storage.T, A = storage.add;
const page = penpot.currentPage;
page.root.children.filter(c => /^H\d\d /.test(c.name)).forEach(c => c.remove());
const W = 1440, H = 900, GAP = 120, Y0 = 2400;
const boards = [];
const mk = (i, name) => { const b = storage.bgBoard(`H0${i} ${name}`, W, H, (i - 1) * (W + GAP), Y0); page.root.appendChild(b); boards.push(b); return b; };
const panel = (b, x, y, w, h, title) => {
  const r = penpot.createRectangle(); r.x = x; r.y = y; r.resize(w, h); r.borderRadius = 16; r.fills = [{ fillColor: T.surface, fillOpacity: 0.9 }]; r.strokes = [{ strokeColor: T.border, strokeOpacity: 1, strokeWidth: 1 }]; r.name = 'panel/' + title; b.appendChild(r);
  A(b, storage.text(title, x + 20, y + 14, { size: 12, weight: 700, color: T.muted, upper: true, letterSpacing: 2 }));
  return r;
};
const topbar = (b, right) => {
  const r = penpot.createRectangle(); r.x = 0; r.y = 0; r.resize(W, 56); r.fills = [{ fillColor: T.bg, fillOpacity: 0.8 }]; r.strokes = [{ strokeColor: T.border, strokeOpacity: 1, strokeWidth: 1 }]; r.name = 'topbar'; b.appendChild(r);
  A(b, storage.text('СВОЯ ИГРА · ПУЛЬТ ВЕДУЩЕГО', 24, 16, { font: 'display', size: 20, weight: 600, upper: true, color: T.accent2, letterSpacing: 2 }));
  A(b, storage.text(right, W - 360, 18, { size: 14, color: T.muted, w: 336, align: 'right' }));
};
const btn = (b, label, x, y, w = 160, variant = 'default', size = 15) => storage.hexPlate(b, x, y, w, 44, { variant, label, size, font: 'sans', weight: 700, name: 'btn/' + label });
const field = (b, label, value, x, y, w) => {
  A(b, storage.text(label, x, y, { size: 12, color: T.muted }));
  const r = penpot.createRectangle(); r.x = x; r.y = y + 20; r.resize(w, 44); r.borderRadius = 10; r.fills = [{ fillColor: T.bg2, fillOpacity: 1 }]; r.strokes = [{ strokeColor: T.border, strokeOpacity: 1, strokeWidth: 1 }]; r.name = 'field'; b.appendChild(r);
  A(b, storage.text(value, x + 14, y + 33, { size: 15 }));
};
const playerRows = (b, x, y, w, rows) => rows.forEach(([n, score, st, rtt], i) => {
  const yy = y + i * 56;
  const r = penpot.createRectangle(); r.x = x; r.y = yy; r.resize(w, 48); r.borderRadius = 10; r.fills = [{ fillColor: st === 'answering' ? T.surface3 : T.bg2, fillOpacity: 1 }]; r.strokes = [{ strokeColor: st === 'answering' ? T.accent : T.border, strokeOpacity: 1, strokeWidth: 1 }]; r.name = 'player/' + n; b.appendChild(r);
  A(b, storage.text(n, x + 14, yy + 14, { size: 15, weight: 600 }));
  A(b, storage.text(String(score), x + w - 200, yy + 10, { font: 'display', size: 24, weight: 600, color: String(score).startsWith('-') ? T.danger : T.accent2 }));
  A(b, storage.text(rtt, x + w - 110, yy + 16, { size: 12, color: rtt.includes('!') ? T.warning : T.muted }));
});

// H01 Create room
{ const b = mk(1, 'Create'); topbar(b, 'ИИ-ведущий: настроен · ffprobe: есть');
  panel(b, 40, 84, 640, 776, 'Пак');
  field(b, 'Поиск по библиотеке', 'тест', 60, 122, 600);
  [['Тестовый пакет SIGame', '3 раунда · 31 вопрос · медиа', true], ['Кино 2024', '3 раунда · 90 вопросов', false], ['История России', '2 раунда · 60 вопросов', false], ['Загрузить .siq…', 'импорт v4/v5, отчёт совместимости', false]].forEach(([n, s, sel], i) => {
    const y = 200 + i * 72; const r = penpot.createRectangle(); r.x = 60; r.y = y; r.resize(600, 60); r.borderRadius = 12; r.fills = [{ fillColor: sel ? T.surface3 : T.bg2, fillOpacity: 1 }]; r.strokes = [{ strokeColor: sel ? T.accent : T.border, strokeOpacity: 1, strokeWidth: sel ? 2 : 1 }]; r.name = 'pack'; b.appendChild(r);
    A(b, storage.text(n, 76, y + 12, { size: 16, weight: 600 })); A(b, storage.text(s, 76, y + 34, { size: 12, color: T.muted }));
  });
  panel(b, 720, 84, 680, 520, 'Комната');
  field(b, 'Название', 'Пятничная игра', 740, 122, 640);
  A(b, storage.text('Ведущий', 740, 200, { size: 12, color: T.muted }));
  ['Человек', 'ИИ', 'Гибрид'].forEach((l, i) => btn(b, l, 740 + i * 176, 220, 160, i === 0 ? 'active' : 'default'));
  A(b, storage.text('Кнопка', 740, 290, { size: 12, color: T.muted }));
  ['LAN (кабель)', 'Wi-Fi вечеринка', 'Интернет', 'Турнир'].forEach((l, i) => btn(b, l, 740 + (i % 2) * 330, 310 + Math.floor(i / 2) * 56, 314, i === 1 ? 'active' : 'default'));
  field(b, 'Игроков (макс.)', '6', 740, 440, 200); field(b, 'Пароль', '', 960, 440, 420);
  storage.hexPlate(b, 720, 640, 680, 64, { variant: 'gold', label: 'Создать комнату', size: 20, font: 'sans', weight: 700, upper: true, name: 'btn/create' });
  panel(b, 720, 740, 680, 120, 'После создания');
  A(b, storage.text('Код K7M3P · http://192.168.1.5:8080/?room=K7M3P · QR на табло', 740, 780, { size: 14, color: T.muted, w: 640 }));
}
// H02 Lobby
{ const b = mk(2, 'Lobby'); topbar(b, 'Комната K7M3P · пак «Тестовый пакет SIGame»');
  panel(b, 40, 84, 900, 560, 'Участники');
  playerRows(b, 60, 122, 860, [['Ведущий Игорь (вы)', '', '', 'хост'], ['Аня', 0, '', 'готова · 12 мс'], ['Борис', 0, '', 'не готов · 48 мс'], ['Вера', 0, '', 'готова · 31 мс'], ['Зритель: Телевизор', '', '', 'табло']]);
  ['Кик', 'Бан', 'Передать хост'].forEach((l, i) => btn(b, l, 60 + i * 176, 440, 160));
  panel(b, 980, 84, 420, 560, 'Настройки');
  A(b, storage.text('Ведущий: человек\nКнопка: Wi-Fi вечеринка\nФальстарты: да · Апелляции: да\nВремя на ответ: 25 с', 1000, 122, { size: 14, color: T.text, w: 380, h: 120 }));
  btn(b, 'Изменить', 1000, 260, 160);
  storage.hexPlate(b, 40, 700, 1360, 72, { variant: 'gold', label: 'Начать игру', size: 22, font: 'sans', weight: 700, upper: true, name: 'btn/start' });
  panel(b, 40, 800, 1360, 60, 'Чат'); A(b, storage.text('Борис: всем привет!  ·  Вера: погнали', 60, 836, { size: 14 }));
}
// H03 Game: table + validation + players/quality
{ const b = mk(3, 'Game'); topbar(b, 'Раунд 1 · вопрос 7/30 · КИНО 300');
  panel(b, 40, 84, 760, 500, 'Таблица (клик — вопрос; ⌥ — убрать/вернуть)');
  const themes = ['История', 'Кино', 'Наука', 'Спорт', 'Музыка', 'География'];
  themes.forEach((t, r) => { const y = 122 + r * 72; A(b, storage.text(t, 60, y + 10, { size: 13, weight: 600, color: T.muted, upper: true })); [100, 200, 300, 400, 500].forEach((p, c) => storage.hexPlate(b, 190 + c * 118, y, 108, 48, { variant: (r === 1 && c === 2) ? 'gold' : ((r + c) % 4 === 0) ? 'dim' : 'default', label: (r + c) % 4 === 0 ? '' : String(p), size: 22, name: `cell/${r}/${c}` })); });
  panel(b, 840, 84, 560, 500, 'Проверка ответа');
  A(b, storage.text('Аня отвечает', 860, 122, { size: 14, color: T.accent2, weight: 600 }));
  storage.hexPlate(b, 860, 150, 520, 72, { variant: 'active', label: 'Кристофер Нолан', size: 28, font: 'display', weight: 500, name: 'hex/answer' });
  A(b, storage.text('Эталон: Кристофер Нолан · Нолан\nНеверные: Спилберг', 860, 240, { size: 14, color: T.muted, w: 520, h: 44 }));
  A(b, storage.text('ИИ: верно (0.98) — «полное имя режиссёра совпадает с эталоном»', 860, 296, { size: 13, color: T.info, w: 520, h: 40 }));
  btn(b, 'Верно', 860, 350, 160, 'gold', 16); btn(b, 'Неверно', 1040, 350, 160, 'default', 16); btn(b, '½ балла', 1220, 350, 160, 'default', 16);
  const bar = penpot.createRectangle(); bar.x = 860; bar.y = 420; bar.resize(520, 8); bar.borderRadius = 4; bar.fills = [{ fillColor: T.bg2, fillOpacity: 1 }]; b.appendChild(bar);
  const f = penpot.createRectangle(); f.x = 860; f.y = 420; f.resize(360, 8); f.borderRadius = 4; f.fills = [storage.vgrad(T.primary2, T.primary)]; f.name = 'timer'; b.appendChild(f);
  A(b, storage.text('решение ведущего: 21 с', 860, 436, { size: 12, color: T.muted }));
  ['Пауза', 'Дальше', 'Вернуть вопрос', 'Сменить чузера'].forEach((l, i) => btn(b, l, 860 + (i % 2) * 270, 480 + Math.floor(i / 2) * 52, 254));
  panel(b, 40, 608, 1360, 252, 'Игроки · счёт · кнопка');
  playerRows(b, 60, 646, 1320, [['Аня', 300, 'answering', 'отвечает · 12 мс · good'], ['Борис', -100, '', 'ошибся · 48 мс · fair'], ['Вера', 200, '', 'ждёт · 31 мс · good !flag']]);
}
// H04 Buzzer audit
{ const b = mk(4, 'Buzzer'); topbar(b, 'Аудит кнопки · вопрос КИНО 300');
  panel(b, 40, 84, 1360, 380, 'Результат нажатия (arm 7f3a…): победила Аня, разрыв 42 мс, правило single');
  const cols = ['Игрок', 'Реакция', 'Источник', 'RTT', '±u', 'Флаги'];
  cols.forEach((c, i) => A(b, storage.text(c, 60 + i * 220, 130, { size: 12, color: T.muted, upper: true, letterSpacing: 1 })));
  [['Аня', '212 мс', 'client', '12 мс', '3 мс', '—'], ['Борис', '254 мс', 'client', '48 мс', '9 мс', '—'], ['Вера', '— (не нажала)', '', '31 мс', '6 мс', 'early_bias']].forEach((row, r) => row.forEach((v, i) => A(b, storage.text(v, 60 + i * 220, 160 + r * 40, { size: 15, weight: i === 0 ? 600 : 500, color: v.includes('_') ? T.warning : T.text }))));
  ['Переоткрыть кнопку', 'Доверие: только сервер', 'Скачать лог'].forEach((l, i) => btn(b, l, 60 + i * 260, 320, 244));
  panel(b, 40, 500, 1360, 360, 'Качество соединения (обновляется каждые 5 с)');
  ['Аня · 12 мс · джиттер 1 мс · good · доверие полное', 'Борис · 48 мс · джиттер 6 мс · fair · доверие полное', 'Вера · 31 мс · джиттер 4 мс · good · флаг early_bias ×2 — осторожно'].forEach((l, i) => A(b, storage.text(l, 60, 540 + i * 36, { size: 15, color: l.includes('флаг') ? T.warning : T.text })));
  A(b, storage.text('Пресет: Wi-Fi вечеринка · окно сбора до 400 мс · тай-брейк: likelihood', 60, 700, { size: 13, color: T.muted }));
}
boards.forEach(b => storage.fixBoard(b));
return boards.map(b => ({ name: b.name, children: b.children.length }));

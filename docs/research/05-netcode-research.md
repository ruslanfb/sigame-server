# Честность кнопки (buzzer fairness) в онлайн-«Своей игре»: исследование

Формат: выводы по пяти темам задачи + сводная таблица чисел + рекомендации для нашего бэкенда. Источники в конце; исходники SIGame склонированы в `/private/tmp/claude-501/-Users-ruslan-Desktop-Projects-game/e9469d22-7549-43b6-b848-6869308c73e9/scratchpad/si` и `.../sionline` (плюс `.../protobowl`).

## TL;DR

1. Оригинальная SIGame (VladimirKhil/SI) уже содержит три режима кнопки: **`RandomWithinInterval` (по умолчанию!)**, `FirstWins` (по порядку прихода на сервер), `FirstWinsClient` (клиент сам меряет реакцию от момента получения `TRY` и шлёт дельту в мс; сервер берёт минимум). Окно сбора нажатий — `ButtonsAccepting = 300 мс`, блокировка за фальстарт — `ButtonBlocking = 3 с`. Никакой синхронизации часов и измерения ping в протоколе нет; строка настройки «Замедлять игроков с хорошим пингом» (`UsePingPenalty`) есть только в ресурсах десктопа и ни на что не ссылается в коде.
2. Ни один популярный онлайн-баззер не «решил» проблему — они делятся на три лагеря: (а) порядок прихода пакетов на сервер (Buzzonk, Fingers On Buzzers, Quizado, SIGame `FirstWins`) — честно только в одной сети; (б) клиентская реакция (Game Night Buzzer «Reaction mode», патент US5695400 1997 г., SIGame `FirstWinsClient`) — компенсирует пинг, но доверяет клиенту; (в) серверный мониторинг latency с поправкой (BuzzIn.Live — NAQT прямо пишет, что решение «не опротестовывается» и «невозможно достичь точности офлайн-системы»).
3. Физика: медиана простой зрительной реакции человека ≈ 273 мс (HumanBenchmark), лабораторно 213–231 мс, внутрииндивидуальный разброс SD ≈ 40 мс. Джиттер Ethernet 0.3–1.7 мс, Wi‑Fi 4–48 мс (пики 83 мс); NTP-подобная синхронизация в LAN даёт < 1 мс, в интернете 10–100 мс, теоретическая граница ошибки RTT/2. То есть на проводной LAN разница пингов (доли мс) в 100 раз меньше человеческого разброса, а на Wi‑Fi/WAN (10–50 мс) — сопоставима с ним, и именно тут «побеждает пинг».
4. Практическая рекомендация: серверно-авторитетная «гибридная» схема — сервер шлёт `ARMED` с меткой времени, клиент отвечает дельтой реакции, сервер **принимает клиентскую дельту только в пределах, ограниченных измеренным RTT игрока** (иначе клампит), собирает нажатия в окно `W` (динамически ≥ разброса RTT, 100–500 мс) и внутри окна разрешает «одновременность» правилом (random / приоритет отстающему / round-robin). Плюс лок-аут за фальстарт и античит на «нечеловеческие» реакции.

---

## 1. Как это устроено в оригинальной SIGame (по исходникам и гайдам)

### 1.1 Протокол и фальстарты
- Сервер (SICore, `Game.cs`) по завершении чтения вопроса шлёт игрокам, которым можно жать, сообщение **`TRY`** (`Messages.Try = "TRY"`, `Game.cs:1477`), а по истечении времени — **`ENDTRY`** с параметром `EndTry_All` («Timer 1 STOP», `GameController.cs:2449`). Нажатие клиента — сообщение **`I`** с необязательным вторым аргументом `pressDuration` (мс): `OnButtonPress(message.Sender, args.Length > 1 && int.TryParse(args[1], ...) ? pressDuration : -1)` (`Game.cs:731`).
- **Фальстарты** (`FalseStart`, по умолчанию `true`): кнопка не принимается до `TRY`; веб-клиент подсвечивает рамку. Нажатие вне `DecisionType.Pressing` → `HandlePlayerMisfire`, запоминается `LastBadTryTime`, и игрок не может нажать ещё `TimeSettings.ButtonBlocking` (**3 с** по умолчанию, `SI.Contracts/TimeSettings.cs:19`). Клиент (SIOnline `room2Slice.ts`) дополнительно сам отключает кнопку на `timeForBlockingButton * 1000` мс.
- Хабр-гайд: «Кнопка «ответить» засчитывается только после того, как вопрос дочитается и его экран засветится белой рамкой»; «При включённом фальстарте все игроки в равной степени успеют почитать и послушать вопрос». Тот же гайд о режиме кнопки: «Эта настройка индивидуальна и зависит от того, как сейчас работают сервера… выбирайте то, что лучше работает» — то есть автор не даёт технического объяснения.
- Steam-гайд: есть «варианты выбора кто первый нажал по версии клиента или сервера», чтобы из «нескольких одновременно нажавших кнопку ответа» выбрать первого. Обзор на StopGame упоминает «странный пинг», позволяющий отдельным игрокам всегда нажимать первыми.

### 1.2 Три режима кнопки (`SIData/ButtonPressMode.cs`, `SI.Contracts/Models/ButtonPressMode.cs`)
| Режим | Русская подпись (`Resources.ru-RU.resx`) | Логика на сервере (`Game.cs:2375–2432`, `GameController.cs:2883–2895`) |
|---|---|---|
| `RandomWithinInterval` = 0 (**default**, `AppSettingsCore.DefaultButtonPressMode`) | «Рандом из ранних нажавших» | Первое нажатие открывает окно `WaitInterval = ButtonsAccepting / 100` (ButtonsAccepting = **300 мс** → 3 тика по 100 мс), все нажавшие копятся в `PendingAnswererIndicies`; затем `Random.Shared.Next(count)` выбирает отвечающего, остальным шлётся `PlayerState.Lost`. |
| `FirstWins` = 1 | «Кто первым нажал» | Первый `I`, дошедший до сервера, → `PendingAnswererIndex`, `Stop(StopReason.Answer)`. Чистый порядок прихода: выигрывает лучший пинг. |
| `FirstWinsClient` = 2 | «Кто первым нажал (по версии клиента)» | Клиент шлёт `deltaTime = Date.now() - table.canPressUpdateTime` (время от локального получения `TRY`, `room2Slice.ts:1129`, `tableSlice.ts:234`). Сервер в окне 300 мс держит минимальный `pressDurationMs`; если дельта ≤ 0 или не передана — считается равной `ButtonsAccepting` (худшая). Это компенсирует и downstream-, и upstream-задержку, но **полностью доверяет клиенту** (легко прислать `1`). |

- В `Messages.cs` нет ни `PING`, ни `PONG`: сервер SIGame не измеряет RTT игроков. `UsePingPenalty` («Замедлять игроков с хорошим пингом» / «Slow down the players with good ping») присутствует только в `SIGame/Properties/Resources*.resx`, ссылок в коде нет — идея была, реализация не дожила.
- LAN-вариант (SImulator «веб-кнопки» на телефонах через SignalR, `WebManagerNew.cs:160`) просто вызывает `OnPlayerPressed` в порядке прихода в UI-поток — режим `FirstWins`.
- Античит-контекст: существует автокликер [sigame-click](https://github.com/stefan-vasilenko/sigame-click) (Rust): распознаёт цвет активной кнопки по пикселю, опрос каждые **1000 мкс**, «microsecond precision». Т.е. любые схемы, где выигрывает минимальная реакция, уязвимы к нечеловеческим реакциям (< 50 мс). Свежий issue SIOnline #345 (сент. 2026) — про другой вектор (поиск пакета по названию в SIBrowser), но показывает, что читерство в SIGame — живая тема.

### 1.3 Телевизионные правила как ориентир
- «Своя игра» (НТВ): жать можно только после сигнала (лампочка за спиной ведущего); фальстарт → кнопка блокируется **≈2 с** (Википедия/фандом).
- Jeopardy!: устройства «вооружает» сотрудник за сценой в момент последнего слога; загораются лампы по бокам табло; ранний звонок → **лок-аут 0,25 с**; побеждает «первый сигнал после arming, остальные игнорируются»; при одновременном нажатии контестантам говорят «жать до появления подтверждающего света». Топ-игроки предугадывают момент и попадают в < 20 мс после лампы; Watson жал за 5–10 мс (по другим оценкам 28 мс) при ≈190–200 мс у человека, но тоже попадал под 250 мс лок-аут.

---

## 2. Техники из сетевого кода игр

### 2.1 NTP-подобная синхронизация часов через WebSocket
- Классика (NTP/Cristian): клиент шлёт T1, сервер отвечает T2/T3, клиент получает T4; `offset = ((T2−T1)+(T3−T4))/2`, `RTT = (T4−T1)−(T3−T2)`. **Ошибка ограничена RTT/2**; при асимметрии пути на `d` оценка смещена на `d/2`, и «никакая выборка это не исправит».
- Достижимая точность: NTP «лучше 1 мс в LAN при идеальных условиях (до ~200 мкс)», «десятки мс в публичном интернете (10–100 мс)». Хабр: «наилучшая точность NTP порядка 1 мс при условии, что сервер в локальной сети».
- Практические реализации в браузере: библиотека [timesync](https://github.com/enmasseio/timesync) — ≥5 замеров с паузами, сортировка по latency, медиана, отбрасывание выбросов > ~1σ, среднее; [Protobowl](https://github.com/neotenic/protobowl) (`client/app.coffee:647`) — последние 20 замеров `Date.now() − server_time`, двухпроходный фильтр «ниже среднего» (устойчив к паузам сокетов на iOS), UI показывает `sync_offset ± σ`; буззы фиксируются серверным временем (`attempt.realTime = serverTime()`), первый принятый сервером и выигрывает. Метод 4 меток на WebSocket (server_ts → client_ts → server_ack_ts → client_ack_ts) позволяет считать одностороннюю задержку без сравнения часов разных машин.
- Пример из «телефон-как-геймпад» фреймворка [godot-phone-mass-controllers #6](https://github.com/splatterfacegames/godot-phone-mass-controllers/issues/6): через Cloudflare-туннель WebSocket RTT 26–56 мс даже для игроков в одной комнате; предложено «timestamp inputs on the phone and let the host decide who was first, with compensation bounded by RTT» + показывать per-player RTT.

### 2.2 Lag compensation / rewind (Valve Source)
- Формула: **«Время исполнения команды = Текущее время сервера − Задержка пакета − Задержка интерполяции клиента»**; сервер хранит историю позиций ~1 с (`sv_maxunlag`), `cl_interp` = 100 мс, тикрейт 66 (CS:S — 33). Сервер «отматывает» мир в момент, который видел клиент, и там проверяет попадание.
- Pixonic (Хабр): клиент шлёт вместе с инпутом «видимое время мира», сервер проверяет, «входит ли разница между текущим временем и видимым временем мира клиента в доверительный интервал», и откатывает историю. Разработчики признают уязвимость: **игрок может искусственно завысить пинг**, чтобы стрелять «из прошлого». Это ровно та же дилемма, что и с клиентской дельтой реакции.
- Аналог для баззера: сервер фиксирует `t_arm` (когда отправил `ARMED`) и `t_recv` (когда пришло нажатие); «отмотка» = вычесть оценку односторонней задержки игрока (`RTT_i/2`) — доверяем не клиенту, а собственным измерениям.

### 2.3 Input timestamping и «authoritative server + client timestamps»
- **Патент US5695400 (1997, «Method of managing multi-player game playing over a network»)** прямо про Jeopardy-подобный buzz-in: «игрок с малой задержкой получает вопрос на секунды раньше»; решение — терминал вкладывает в ответ «время, прошедшее между получением сигнала «start» и инициацией ответа», сервер ранжирует по этому времени; допускаются глобальные или локальные часы. Это прототип `FirstWinsClient` и Game Night Buzzer.
- **Патент US11611510 («Network latency fairness in multi-user gaming platforms»)**: сервер измеряет RTT/односторонние задержки (в т.ч. через UPF/синхронизированные часы), (а) задерживает воспроизведение по playout-timestamp, (б) **придерживает uplink-пакеты игроков с малой задержкой** до прихода пакетов «медленных», (в) переупорядочивает по timestamp отправки, (г) вводит «handicap». Числовых порогов нет.
- Браузерная сторона: `Event.timeStamp` в Chrome на Windows — время browser-процесса (драйверы «дают ужасные значения»), на Mac ближе к hardware; дискретные события (`keydown`, `pointerdown`, `touchstart`) диспатчатся немедленно, а не выравниваются по rAF; задержка ОС→браузер 5–10 мс плюс 5–20 мс обработки, ±16 мс на дисплей при 60 Гц. Точность `performance.now()`: Chrome 100 мкс (5 мкс при cross-origin isolation), Firefox 1 мс (`privacy.reduceTimerPrecision`), Safari 1 мс — для миллисекундных дельт достаточно.
- Rollback/GGPO: фиксированный «input delay» в кадрах, чтобы скрыть сеть — концептуально то же, что окно `RandomWithinInterval`: все нажатия внутри задержки считаются принадлежащими одному «кадру».

### 2.4 Delay equalization и fairness windows (академия)
- Zander, Leeder, Armitage 2005 (ACE, Quake III): «игроки с большей задержкой в явно невыгодном положении»; приложение-прокси **добавляет искусственную задержку игрокам с низким пингом**, выравнивая всех по худшему; оценка на ботах «значительно улучшает честность»; оценка порога терпимости 150–180 мс.
- Brun, Safaei, Boustead, CACM 2006 «Managing latency and fairness in networked games»: местная задержка (local lag), timewarp, размещение «точек принятия решений» — компромисс между несогласованностью и честностью. Клэйпуловская таксономия перечисляет ещё Aggarwal 2005 (fairness in dead reckoning) и Le & Liu 2007 (deadline-based networks).

---

## 3. Как это делают продукты

| Продукт | Кто решает «первого» | Компенсация latency | Примечания |
|---|---|---|---|
| **SIGame** | сервер (3 режима, см. §1) | только в `FirstWinsClient` (клиентская дельта, без верификации) | окно 300 мс, лок-аут 3 с |
| **Kahoot!** | нет гонки; очки = `floor((1 − (t_ответа / t_таймера) / 2) × max)` — скорость даёт до 50 % | время меряется по приходу на сервер: «если ответ шёл 800 мс вместо 200 мс из‑за сети — теряете очки не по своей вине» (Couchbase); жалобы на рассинхрон таймера до 15 с на 60‑с вопросе | 2025: **Accuracy mode** — 1 очко за верный ответ, скорость не учитывается |
| **Jackbox** | нет buzzer-гонок; таймеры продлеваются на задержку стрима | Dodo Re Mi (PP10): «count-in» — каждый игрок тапает старт, дальше локальная шкала времени, «работает даже при экстремальной задержке Twitch/YouTube/Discord»; окна «perfect» в мс настраиваются с сервера | подход «убрать зависимость от сети дизайном» |
| **BuzzIn.Live** (рекомендован NAQT) | сервер | «**monitors network latency to determine which player truly buzzed first**»; «невозможно быть таким же точным, как офлайн-система»; решение **не опротестовывается**, даже если «ping внезапно деградировал» | NAQT: Zoom-чат «не учитывает latency», порядок сообщений разный у клиентов; Discord-буззы «задерживались на секунды» |
| **Buzzonk** | сервер, порядок прихода пакетов | нет: «не использует timestamps с устройств игроков, потому что часы игроков не заслуживают доверия»; «в одной сети работает достаточно хорошо; в разных локациях выигрывает лучший роутинг — свойство всех сетевых баззеров» | регионы SF/NYC/Frankfurt, выбирается под хоста |
| **Fingers On Buzzers**, **Quizado** | сервер, время прихода | нет («не гадаем по часам игроков»; Quizado: «не корректирует по скорости соединения») | в одном из онлайн-баззеров в выдаче: ранний buzz → тряска 500 мс, повтор в 3‑с окне → штраф растёт экспоненциально до 1500 мс |
| **Game Night Buzzer** | хост | «Reaction mode»: разница системного времени клиента между активацией и нажатием, в мс, шлётся хосту, «fastest reaction wins… eliminates any element of internet lag or ping» | тот же принцип, что патент 1997 г. и `FirstWinsClient`; доверие клиенту |
| **JeopardyLabs** | — | встроенной компенсации не найдено; сообщество подключает BuzzIn.Live | |
| **Buzzer-приложения** (EZBuzzer, Game Show Buzzer) | хост/сервер, «первый сигнал — зелёный, остальные заблокированы» | EZBuzzer по Bluetooth локально (обходит Wi‑Fi) | |
| **HQ Trivia** | нет гонки: 10 с на ответ, важна только правильность | проблема latency была про стрим (<2 с трудно масштабировать на 1 млн) | обходит проблему дизайном |
| **Trivia Crack** | асинхронно, до 36 ч на ход | не применимо | |
| **Protobowl** | сервер (`attempt` создаётся первым принятым buzz) | клиентский `sync_offset` для отображения, не для арбитража | |
| **Физические системы** (Anderson Officiator, The Judge, DIY Arduino) | аппаратный lock-out: «первое обнаруженное нажатие фиксирует победителя, дальше кнопки не опрашиваются» | латентность проводов ~0; DIY на ATmega8 @ 4 МГц с антидребезгом 5 мс | hsquizbowl: «разные системы по-разному разрешают одновременные нажатия, в некоторых всегда выигрывает один цвет»; у The Judge бывала «жёлтая лампа ничьей» |
| **Jeopardy! (ТВ)** | аппаратно, первый после arming | лок-аут 250 мс за ранний звонок | «Своя игра» НТВ — блокировка ~2 с |

---

## 4. Числа для проектирования

| Величина | Значение | Источник |
|---|---|---|
| Медиана простой зрительной реакции (клик по зелёному) | **273 мс**, кластер 220–320 мс, правый хвост; +10–50 мс от железа | HumanBenchmark |
| Лабораторная SRT | среднее **231 мс** (213 мс с поправкой на задержку оборудования), N=1469; **SD внутри человека ≈ 40 мс**, CV ≈ 17 %; +0.45–0.55 мс/год возраста; Galton 181–189 мс | Woods et al., PMC4374455 |
| Молодые взрослые на свет | ≈190 мс; Jeopardy-чемпионы с антиципацией < 20 мс после лампы; Watson 5–10 (28) мс | Kurzweil Library |
| Задержка тача на смартфоне | 50–200 мс end-to-end; флагманы iOS/Android ≈ 86–88 мс | GameBench / arXiv |
| Ethernet LAN | 0.1–0.5 мс до роутера; джиттер 0.3 мс (idle) → 0.6 (нагрузка) → 1.7 мс (85 % загрузка, пик 3.1) | speedtesthq, jitter.is |
| Wi‑Fi (5 ГГц) | джиттер **4.2 мс** idle (пики 22) → **18.4 мс** при стриме → **47.8 мс** (пик 83) при нагрузке; Wi‑Fi 4 38 мс, Wi‑Fi 5 19, Wi‑Fi 6 17.8, 6E 6.9 мс — из-за CSMA/CA random backoff | jitter.is |
| Ориентиры джиттера | проводная LAN < 1 мс; домашний broadband < 5 мс; Wi‑Fi/перегрузка 10–30 мс | ntp-tester.eu |
| WAN | RTT через Cloudflare-туннель 26–56 мс (Токио); «оптимально 50 мс» (Хабр); > 100 мс уже заметно (сообщество SIGame) | |
| Синхронизация часов | LAN < 1 мс (до 200 мкс); интернет 10–100 мс; ошибка ≤ RTT/2; асимметрия d → ошибка d/2 | NTP, Cristian |
| Точность таймеров браузера | Chrome 100 мкс (5 мкс c COOP/COEP), Firefox 1 мс, Safari 1 мс | Chrome blog, Bugzilla |
| Input pipeline | ОС→браузер 5–10 мс + 5–20 мс; дисплей ±16 мс @60 Гц; дискретные события без rAF-выравнивания | rsms.me, Chrome blog |
| Source engine | история 1 с, `cl_interp` 100 мс, тик 66/33 | Valve wiki / hl-inside |
| SIGame | окно 300 мс, лок-аут 3 с, тик планировщика 100 мс | исходники |
| ТВ | Jeopardy лок-аут 250 мс; НТВ ~2 с | jeopardy.com, Википедия |

Вывод из чисел: на проводной LAN разброс сетевых задержек (< 2 мс) на порядок-два меньше внутрииндивидуального разброса реакции (σ ≈ 40 мс) — там достаточно `FirstWins` по приходу. На Wi‑Fi (джиттер 5–50 мс) и тем более в WAN (разброс RTT 20–100+ мс) сетевой разброс сопоставим с разбросом реакции — «первый по приходу» становится лотереей в пользу пинга, что и ощущают игроки.

---

## 5. Альтернативы чистой скорости и рекомендуемая схема

### 5.1 Каталог приёмов
1. **Порядок прихода на сервер** (Buzzonk, `FirstWins`) — просто, неподделываемо, честно только в одной LAN.
2. **Клиентская дельта реакции** (патент 1997, GNB, `FirstWinsClient`) — устраняет пинг, но клиент может прислать что угодно; нужны ограничения.
3. **Серверная поправка на RTT/2** (BuzzIn.Live-подобное): `t_press_est = t_recv − RTT_i/2`; ошибка ≤ джиттер + асимметрия; ничего не доверяем клиенту, но ошибка на Wi‑Fi 5–20 мс.
4. **Delay equalization** (Zander 2005, US11611510, идея `UsePingPenalty`): задержать доставку `ARMED` быстрым игрокам на `(RTT_max − RTT_i)/2`, чтобы все «увидели лампу» одновременно; и/или придержать их нажатия. Цена — общая задержка = худший пинг; при RTT_max > 200 мс игра «вязнет».
5. **Окно одновременности** (`RandomWithinInterval`, rollback-подобный input delay): все нажатия в `W` мс от первого считаются одновременными. Внутри — random (SIGame), либо **приоритет отстающему по очкам** (в Buzz! «игрок с наименьшим счётом выбирает первым»), либо round-robin/меньше выигранных кнопок в этой игре, либо «все, кто в окне, отвечают письменно одновременно» (all-play, как HQ/Kahoot Accuracy).
6. **Убрать скорость из правил** (Kahoot Accuracy, HQ, Jackbox count-in): аукцион за право ответа, письменные ответы у всех, «кот в мешке» и т.п. — не решает гонку, а отменяет её на части вопросов.
7. **Лок-аут за фальстарт** (Jeopardy 250 мс, НТВ 2 с, SIGame 3 с, экспоненциальный штраф) — подавляет «спам» кнопкой и антиципацию.
8. **Рандомизация момента arming** — случайная пауза 0–1500 мс между концом чтения и `ARMED` убивает антиципацию по ритму чтения (Jeopardy-эффект «< 20 мс»); против пиксельных автокликеров не помогает — нужен античит по статистике.

### 5.2 Рекомендуемая схема для нашего бэкенда (LAN + возможный WAN)
- **Измерять RTT постоянно**: ping/pong каждые 1–2 с, хранить min-фильтр + EWMA + джиттер (σ) на игрока; показывать в UI (как Protobowl/PMC). На LAN считать через WebSocket; для WAN рассмотреть WebTransport (Baseline с марта 2026, нет TCP head-of-line blocking; в тестах P99 при потерях 280 → 35 мс).
- **Arming**: сервер шлёт `BUTTON_ARMED {serverTs, nonce}` (после случайной паузы 0–1500 мс при включённых фальстартах). Клиент фиксирует `performance.now()` при получении.
- **Нажатие**: клиент шлёт `PRESS {nonce, clientDeltaMs, eventTimeStamp}`; сервер фиксирует `t_recv`. Оценки:
  - `d_server = t_recv − t_arm − RTT_i` (полный круг: доставка ARMED + доставка PRESS), 
  - `d_client` — дельта от клиента.
  - Принять `d = d_client`, **если** `|d_client − d_server| ≤ RTT_i/2 + 3σ_i`; иначе `d = d_server` (или clamp к границе) и пометить игрока. Так клиент не может выиграть больше, чем половина своего RTT, — «compensation bounded by RTT».
- **Окно одновременности** `W = clamp(max_j(RTT_j) − min_j(RTT_j) + 2·max σ_j, 50, 300) мс` (динамическое; на проводной LAN ≈ 50 мс, на Wi‑Fi 100–150, в WAN 300). Все с `d − d_min ≤ W` — «одновременно»; тай-брейк по настройке комнаты: `random` (по умолчанию, как в SIGame) / `lowestScore` / `fewestButtonsWon` / `allPlay` (письменный ответ всех в окне).
- **Фальстарт**: лок-аут 250–3000 мс (настройка), экспоненциальный рост при повторе в 3 с.
- **Античит**: отвергать `d_client < 80 мс` подряд (человеческий минимум без антиципации ≈ 150 мс, антиципация невозможна при рандомизированном arming); требовать `event.isTrusted`; статистика по игроку (медиана < 120 мс при σ < 10 мс — флаг «автокликер»); nonce и sequence против replay; всё принятие решений — только на сервере (арбитраж, как у NAQT, «не опротестовывается», но с логом `d_client/d_server/RTT` для показа ведущему).
- **Режимы для API/настроек комнаты**: `buttonMode: "arrival" | "clientReaction" | "hybridBounded" (default) | "equalizedDelay"`, `simultaneityWindowMs: auto|number`, `tieBreak`, `falseStartLockoutMs`, `armingJitterMs`.

Ограничения, о которых честно предупредить пользователей (как NAQT): при Wi‑Fi-джиттере 20–50 мс любая схема ошибается на те же 20–50 мс; на LAN лучше кабель/5 ГГц/6E; идеальной точности офлайн-кнопки не достичь.

## Key facts
- SIGame (VladimirKhil/SI) имеет enum ButtonPressMode: RandomWithinInterval (default), FirstWins, FirstWinsClient; окно ButtonsAccepting = 300 мс, лок-аут ButtonBlocking = 3 с; клиент шлёт 'I <deltaMs>', где deltaMs = Date.now() − время получения TRY; сервер в протоколе не измеряет ping, строка UsePingPenalty («Замедлять игроков с хорошим пингом») в коде не используется
- Сообщения протокола SIGame: TRY (можно жать), ENDTRY (время вышло), I (нажатие), FALSESTART; нажатие до TRY → misfire и блокировка на 3 с
- Медиана простой зрительной реакции человека ≈ 273 мс (HumanBenchmark), лабораторно 213–231 мс, внутрииндивидуальный SD ≈ 40 мс; Jeopardy-чемпионы с антиципацией < 20 мс после лампы; Watson 5–10 мс
- Ethernet LAN: 0.1–0.5 мс, джиттер 0.3–1.7 мс; Wi‑Fi 5 ГГц: джиттер 4.2 мс (idle) → 18.4 → 47.8 мс под нагрузкой (пики до 83 мс); WAN через туннель 26–56 мс RTT
- Синхронизация часов NTP-типа: < 1 мс в LAN (до 200 мкс), 10–100 мс в интернете; ошибка ≤ RTT/2, асимметрия пути d даёт смещение d/2; timesync и Protobowl используют медиану/фильтр выбросов по 5–20 замерам
- performance.now(): Chrome 100 мкс (5 мкс при cross-origin isolation), Firefox 1 мс, Safari 1 мс; дискретные события keydown/pointerdown диспатчатся немедленно; ОС→браузер 5–10 мс + 5–20 мс
- Valve Source: Command Execution Time = Current Server Time − Packet Latency − Client View Interpolation; история 1 с, cl_interp 100 мс; Pixonic признаёт уязвимость — игрок может искусственно завысить пинг
- Патент US5695400 (1997) ранжирует buzz-in по времени от локального получения 'start' до нажатия; патент US11611510 придерживает пакеты игроков с малой задержкой и переупорядочивает по timestamp; Zander/Armitage 2005 — искусственная задержка быстрым игрокам улучшает честность
- BuzzIn.Live (NAQT): 'monitors network latency to determine which player truly buzzed first', решение не опротестовывается; Buzzonk: порядок прихода пакетов, 'player clocks are not trustworthy'; Game Night Buzzer: клиентская реакция в мс, 'eliminates ping'; Kahoot: очки = floor((1 − (t/T)/2) × max), меряется по приходу на сервер, в 2025 добавлен Accuracy mode без скорости
- Jeopardy TV: лок-аут 250 мс за ранний звонок, первый сигнал после arming; НТВ «Своя игра»: блокировка ~2 с; аппаратные lock-out системы фиксируют первое обнаруженное нажатие, тай-брейк иногда 'всегда выигрывает один цвет'
- Существует автокликер sigame-click (Rust, опрос 1000 мкс по цвету кнопки) — схемы 'минимальная реакция выигрывает' нуждаются в античите (порог < 80–120 мс, isTrusted, статистика, nonce)
- Рекомендуемая схема: серверно-авторитетный гибрид — клиентская дельта принимается только в пределах RTT_i/2 + 3σ от серверной оценки t_recv − t_arm − RTT_i; динамическое окно одновременности W = разброс RTT + джиттер (50–300 мс) с тай-брейком random / приоритет отстающему / all-play; лок-аут за фальстарт; рандомизированный arming 0–1500 мс

## Sources
- https://github.com/VladimirKhil/SI (src/SICore/SIData/ButtonPressMode.cs, src/SICore/SICore/Clients/Game/Game.cs, GameController.cs, src/SICore/SI.Contracts/TimeSettings.cs, src/SICore/SICore/Messages.cs, src/SIGame/SIGame/Properties/Resources.ru-RU.resx)
- https://github.com/VladimirKhil/SIOnline (src/state/room2Slice.ts, src/state/tableSlice.ts, src/client/game/GameClient.ts, src/model/ButtonPressMode.ts)
- https://github.com/VladimirKhil/SIOnline/issues/345
- https://github.com/stefan-vasilenko/sigame-click
- https://habr.com/ru/companies/timeweb/articles/920442/
- https://steamcommunity.com/sharedfiles/filedetails/?id=3456423604
- https://stopgame.ru/game/sigame
- https://ru.wikipedia.org/wiki/Своя_игра
- https://habr.com/ru/post/241407/
- https://www.jeopardy.com/jbuzz/behind-scenes/how-does-jeopardy-buzzer-work
- https://www.thekurzweillibrary.com/the-buzzer-factor-did-watson-have-an-unfair-advantage
- https://www.naqt.com/online/buzzin.jsp (архив web.archive.org, 2022)
- https://hsquizbowl.org/forums/viewtopic.php?t=24639
- https://hsquizbowl.org/forums/viewtopic.php?t=24634
- https://www.hsquizbowl.org/forums/viewtopic.php?t=15990 (архив web.archive.org)
- https://www.qbwiki.com/wiki/Buzzer
- https://buzzonk.com/
- https://fobgame.com/
- http://gamenightbuzzer.com/howto.php (архив web.archive.org, 2021)
- https://quizado.com/buzzer-app-for-trivia
- https://play.google.com/store/apps/details?id=com.braultomatic.ezbuzzer
- https://github.com/neotenic/protobowl (client/app.coffee, shared/room.coffee)
- https://antimatter15.com/2014/12/protobowl-real-time-multiplayer-online-quizbowl/
- https://support.kahoot.com/hc/en-us/articles/115002303908-How-points-work
- https://kahoot.com/blog/2025/05/06/teacher-takeover-accuracy-mode/
- https://support.kahoot.com/hc/en-us/community/posts/27352194885651-Timer-not-synced-on-student-devices
- https://www.couchbase.com/blog/zero-latency-quizzing-how-p2p-replication-solved-the-conference-wi-fi-problem/
- https://www.jackboxgames.com/blog/behind-the-scenes-of-pp10-engineering
- https://www.jackboxgames.com/blog/how-to-play-party-pack-nine-remotely
- https://en.wikipedia.org/wiki/HQ_(video_game)
- https://en.wikipedia.org/wiki/Trivia_Crack
- https://patents.google.com/patent/US5695400A/en
- https://patents.google.com/patent/US11611510B2/en
- https://dl.acm.org/doi/10.1145/1178477.1178493
- https://cacm.acm.org/research/managing-latency-and-fairness-in-networked-games/
- https://web.cs.wpi.edu/~claypool/papers/lag-taxonomy/LatencyCompensation.html
- https://developer.valvesoftware.com/wiki/Lag_compensation
- https://hl-inside.me/articles/multiplayer-networking/
- https://habr.com/ru/companies/pixonic/articles/415959/
- https://www.gabrielgambetta.com/lag-compensation.html
- https://github.com/splatterfacegames/godot-phone-mass-controllers/issues/6
- https://github.com/enmasseio/timesync
- https://en.wikipedia.org/wiki/Network_Time_Protocol
- https://en.wikipedia.org/wiki/Cristian%27s_algorithm
- https://habr.com/ru/articles/876536/
- https://ankitbko.github.io/blog/2022/06/websocket-latency/
- https://www.ntp-tester.eu/jitter-analysis.html
- https://jitter.is/blog/ethernet-vs-wifi-jitter/
- https://www.speedtesthq.com/guides/wireless/wifi-vs-ethernet
- https://humanbenchmark.com/tests/reactiontime/statistics
- https://pmc.ncbi.nlm.nih.gov/articles/PMC4374455/
- https://blog.gamebench.net/touch-latency-benchmarks-iphone-xs-max-galaxy-note-10
- https://developer.chrome.com/blog/cross-origin-isolated-hr-timers
- https://bugzilla.mozilla.org/show_bug.cgi?id=1440863
- https://developer.chrome.com/blog/aligning-input-events
- https://groups.google.com/a/chromium.org/d/topic/input-dev/1ez9ojul490
- https://rsms.me/projects/pointer-latency/
- https://arxiv.org/html/2512.21377v1
- https://websocket.org/comparisons/webtransport/
- https://infoq.com/news/2026/03/fosdem-webtransport-vs-websocket
- https://www.ggpo.net/
- https://en.wikipedia.org/wiki/Buzz!:_The_Music_Quiz

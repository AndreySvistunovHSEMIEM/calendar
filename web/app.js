"use strict";

const $ = selector => document.querySelector(selector);
const categories = {work: "Работа", personal: "Личное", study: "Учёба", space: "Космос"};
const today = dayString(new Date());
const state = {events: [], selected: today, cursor: parseDay(today), mini: parseDay(today), view: "month", agendaAll: false, query: "", filters: new Set(Object.keys(categories)), editing: null, busy: false, loading: true, deleted: null, user: null, authVersion: 0};
let authMode = "login";
let toastTimer;
let beforeSearch = null;

function dayString(date) {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}
function parseDay(day) { const [year, month, date] = day.split("-").map(Number); return new Date(year, month - 1, date, 12); }
function addDays(date, days) { const next = new Date(date); next.setDate(next.getDate() + days); return next; }
function monthStart(date) { return new Date(date.getFullYear(), date.getMonth(), 1, 12); }
function weekStart(date) { return addDays(date, -((date.getDay() + 6) % 7)); }
function formatDay(day, options) { return parseDay(day).toLocaleDateString("ru-RU", options); }
function monthName(date) { const value = date.toLocaleDateString("ru-RU", {month: "long", year: "numeric"}).replace(" г.", ""); return value[0].toUpperCase() + value.slice(1); }
function esc(value) { return String(value).replace(/[&<>"']/g, ch => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[ch])); }
function icon(name) { return `<svg class="icon" aria-hidden="true"><use href="#i-${name}"/></svg>`; }
function plural(count, words = ["событие", "события", "событий"]) { const a = count % 100, b = count % 10; return `${count} ${words[a > 10 && a < 20 ? 2 : b === 1 ? 0 : b > 1 && b < 5 ? 1 : 2]}`; }
function validDay(day) { return day >= "1900-01-01" && day <= "2100-12-31"; }
function eventTime(event) { return event.allDay ? "Весь день" : `${event.start} — ${event.end}`; }
function filteredEvents() {
  return state.events.filter(event => state.filters.has(event.category) && (!state.query || [event.title, event.description, event.location].some(text => text.toLocaleLowerCase("ru-RU").includes(state.query))));
}
function onDay(events, day) { return events.filter(event => event.date === day); }

async function request(path, method = "GET", data) {
  const response = await fetch(path, {method, headers: data ? {"Content-Type": "application/json"} : {}, body: data ? JSON.stringify(data) : undefined});
  const value = response.status === 204 ? null : await response.json();
  if (!response.ok) {
    if (response.status === 401 && !path.startsWith("/api/auth/")) showAuth();
    const error = new Error(value?.error || "Не удалось выполнить запрос"); error.status = response.status; throw error;
  }
  return value;
}
async function loadEvents() {
  const version = state.authVersion;
  try {
    const events = await request("/api/events");
    if (version !== state.authVersion || !state.user) return;
    state.events = events;
    state.loading = false;
    render();
    updateConnection();
  } catch (error) {
    state.loading = false;
    $("#connection-status").textContent = "Нет связи с сервером · обновите страницу";
    $("#connection-status").classList.add("error");
    render();
    notify("Не удалось загрузить календарь. Проверьте, что Go-сервер запущен.", true);
  }
}
function updateConnection() {
  const sample = state.events.some(event => event.description.startsWith("Демо-событие"));
  $("#connection-status").textContent = sample ? "Сохранено · есть демо-события" : "Сохранено в вашем календаре";
  $("#connection-status").classList.remove("error");
}
function render() {
  const events = filteredEvents();
  renderFilters();
  renderMini();
  renderCalendar(events);
  renderDay(events);
  $("#hero-date").textContent = formatDay(today, {day: "numeric", month: "long", year: "numeric"}).replace(" г.", "");
  $("#hero-count").textContent = `${plural(onDay(state.events, today).length)} сегодня`;
  $("#total-count").textContent = state.events.length;
  $("#nav-calendar").classList.toggle("active", !state.agendaAll);
  $("#nav-agenda").classList.toggle("active", state.agendaAll);
  document.querySelectorAll("[data-view]").forEach(button => {
    const selected = button.dataset.view === state.view;
    button.classList.toggle("selected", selected);
    button.setAttribute("aria-pressed", selected);
  });
}
function renderFilters() {
  $("#category-filters").innerHTML = Object.entries(categories).map(([key, label]) => `<label class="filter-row category-${key}"><input type="checkbox" data-category="${key}" ${state.filters.has(key) ? "checked" : ""} aria-label="Показывать: ${label}"><span>${label}</span><span>${state.events.filter(event => event.category === key).length}</span></label>`).join("");
}
function renderMini() {
  $("#mini-title").textContent = monthName(state.mini);
  const start = weekStart(monthStart(state.mini));
  $("#mini-grid").innerHTML = Array.from({length: 42}, (_, i) => {
    const date = addDays(start, i), day = dayString(date);
    return `<button data-mini-day="${day}" class="${date.getMonth() !== state.mini.getMonth() ? "outside " : ""}${day === today ? "today " : ""}${day === state.selected ? "selected" : ""}" aria-label="${esc(formatDay(day, {day: "numeric", month: "long", year: "numeric"}))}" aria-pressed="${day === state.selected}" ${validDay(day) ? "" : "disabled"}>${date.getDate()}</button>`;
  }).join("");
  $("#mini-prev").disabled = state.mini.getFullYear() === 1900 && state.mini.getMonth() === 0;
  $("#mini-next").disabled = state.mini.getFullYear() === 2100 && state.mini.getMonth() === 11;
}
function chip(event, week = false) {
  return `<button class="event-chip category-${event.category}${event.completed ? " completed" : ""}" data-event="${esc(event.id)}" title="${esc(event.title + " · " + eventTime(event))}"><span class="chip-title">${esc(event.title)}</span><span class="chip-time">${week ? esc(eventTime(event)) : event.allDay ? "" : esc(event.start)}</span></button>`;
}
function renderCalendar(events) {
  const month = monthName(state.cursor);
  const globalView = state.query || state.agendaAll;
  $("#prev").disabled = Boolean(globalView) || (state.cursor.getFullYear() === 1900 && state.cursor.getMonth() === 0 && (state.view !== "week" || state.cursor.getDate() <= 7));
  $("#next").disabled = Boolean(globalView) || (state.cursor.getFullYear() === 2100 && state.cursor.getMonth() === 11 && (state.view !== "week" || state.cursor.getDate() >= 25));
  if (state.query) $("#period-title").textContent = "Результаты поиска";
  else if (state.agendaAll) $("#period-title").textContent = "Мои события";
  else if (state.view === "week") {
    const start = weekStart(state.cursor), end = addDays(start, 6);
    $("#period-title").textContent = start.getMonth() === end.getMonth() ? `${start.getDate()}–${formatDay(dayString(end), {day: "numeric", month: "long"})}` : `${formatDay(dayString(start), {day: "numeric", month: "short"})} – ${formatDay(dayString(end), {day: "numeric", month: "short"})}`;
  } else $("#period-title").textContent = month;
  $("#search-status").hidden = !state.query;
  $("#search-status").textContent = `${plural(events.length, ["совпадение", "совпадения", "совпадений"])} по всем датам · с учётом выбранных орбит`;
  if (state.view === "agenda" || globalView) { renderAgenda(events, Boolean(globalView)); return; }
  if (state.view === "week") { renderWeek(events); return; }
  const start = weekStart(monthStart(state.cursor));
  const cells = Array.from({length: 42}, (_, i) => {
    const date = addDays(start, i), day = dayString(date), daily = onDay(events, day);
    const cls = ["day-cell", date.getMonth() !== state.cursor.getMonth() ? "outside" : "", day === today ? "today" : "", day === state.selected ? "selected" : ""].join(" ");
    return `<div class="${cls}" data-day="${day}"><div class="cell-header"><button class="day-number" data-select-day="${day}" aria-label="${esc(formatDay(day, {day: "numeric", month: "long", year: "numeric"}))}, ${plural(daily.length)}" aria-pressed="${day === state.selected}" ${validDay(day) ? "" : "disabled"}>${date.getDate()}</button>${day === today ? '<span class="today-label">Сегодня</span>' : ""}</div>${daily.slice(0, 3).map(event => chip(event)).join("")}${daily.length > 3 ? `<button class="more-events" data-select-day="${day}">Ещё ${daily.length - 3}</button>` : ""}</div>`;
  }).join("");
  $("#calendar").innerHTML = `<div class="weekdays"><span>ПН</span><span>ВТ</span><span>СР</span><span>ЧТ</span><span>ПТ</span><span>СБ</span><span>ВС</span></div><div class="month-grid">${cells}</div>`;
}
function renderWeek(events) {
  const start = weekStart(state.cursor);
  $("#calendar").innerHTML = `<div class="week-grid">${Array.from({length: 7}, (_, i) => {
    const date = addDays(start, i), day = dayString(date), daily = onDay(events, day);
    return `<div data-day="${day}" class="week-day ${day === today ? "today" : ""} ${day === state.selected ? "selected" : ""}"><div class="week-head"><span>${esc(formatDay(day, {weekday: "short"}).toUpperCase())}</span><button class="day-number" data-select-day="${day}" aria-label="${esc(formatDay(day, {day: "numeric", month: "long", year: "numeric"}))}" aria-pressed="${day === state.selected}" ${validDay(day) ? "" : "disabled"}>${date.getDate()}</button></div>${daily.map(event => chip(event, true)).join("")}</div>`;
  }).join("")}</div>`;
}
function renderAgenda(events, all) {
  const prefix = dayString(state.cursor).slice(0, 7);
  const visible = all ? events : events.filter(event => event.date.startsWith(prefix));
  if (!visible.length) {
    $("#calendar").innerHTML = `<div class="agenda-view"><div class="agenda-empty">${icon("orbit")}<strong>${state.query ? "Ничего не найдено" : state.loading ? "Загружаем вашу орбиту…" : "На этой орбите пока тихо"}</strong><br>${state.query ? "Попробуйте другой запрос или включите больше орбит." : "Добавьте первое событие и начните планировать."}${state.query ? "" : '<br><button class="primary create-event">Новое событие</button>'}</div></div>`;
    return;
  }
  let currentDay = "";
  const rows = visible.map(event => {
    let heading = "";
    if (event.date !== currentDay) {
      currentDay = event.date;
      heading = `<h3 class="agenda-heading">${esc(formatDay(event.date, {weekday: "long", day: "numeric", month: "long", year: "numeric"}))}${event.date === today ? " · сегодня" : ""}</h3>`;
    }
    return `${heading}<div class="agenda-row category-${event.category}${event.completed ? " completed" : ""}"><span class="agenda-date">${event.allDay ? "Весь день" : esc(event.start)}</span><span class="category-dot"></span><button class="agenda-title" data-event="${esc(event.id)}"><strong>${esc(event.title)}</strong><small>${event.allDay ? "" : esc(eventTime(event))}${event.location ? " · " + esc(event.location) : ""}</small></button><span class="category-tag">${categories[event.category]}</span>${completionButton(event)}</div>`;
  }).join("");
  $("#calendar").innerHTML = `<div class="agenda-view">${rows}</div>`;
}
function completionButton(event) {
  return `<button class="complete-button" data-complete="${esc(event.id)}" aria-pressed="${event.completed}" aria-label="${esc((event.completed ? "Вернуть в план: " : "Завершить: ") + event.title)}">${event.completed ? icon("check") : ""}</button>`;
}
function renderDay(events) {
  const daily = onDay(events, state.selected);
  $("#selected-label").textContent = state.selected === today ? "СЕГОДНЯ НА ОРБИТЕ" : "ВАШ ДЕНЬ НА ОРБИТЕ";
  $("#selected-title").textContent = formatDay(state.selected, {day: "numeric", month: "long"});
  const completed = daily.filter(event => event.completed).length;
  $("#selected-summary").textContent = `${formatDay(state.selected, {weekday: "long"})} · ${plural(daily.length)}${completed ? ` · ${completed} завершено` : ""}`;
  $("#day-events").innerHTML = daily.length ? daily.map(event => `<article class="day-event category-${event.category}${event.completed ? " completed" : ""}"><div class="event-time">${icon("clock")}${esc(eventTime(event))}</div><button class="event-title-button" data-event="${esc(event.id)}">${esc(event.title)}</button>${completionButton(event)}${event.location ? `<p class="event-description">${esc(event.location)}</p>` : event.description ? `<p class="event-description">${esc(event.description)}</p>` : ""}<span class="category-tag">${categories[event.category]}</span>${event.processingStatus === "pending" ? '<span class="processing-badge">Подготовка…</span>' : ""}</article>`).join("") : `<div class="day-empty">${icon("orbit")}<p>${state.loading ? "Загружаем события…" : "Свободный день.<br>Каким будет ваш следующий шаг?"}</p><button class="create-event">+ Добавить событие</button></div>`;
  const upcoming = events.filter(event => event.date > state.selected && !event.completed).slice(0, 3);
  $("#upcoming-events").innerHTML = upcoming.length ? upcoming.map(event => `<button class="upcoming category-${event.category}" data-event="${esc(event.id)}"><span class="upcoming-date"><strong>${parseDay(event.date).getDate()}</strong><small>${esc(formatDay(event.date, {month: "short"}).replace(".", ""))}</small></span><span class="upcoming-copy"><strong>${esc(event.title)}</strong><span><span class="category-dot"></span>${event.allDay ? "Весь день" : esc(event.start)} · ${categories[event.category]}</span></span></button>`).join("") : '<p class="empty-upcoming">Здесь появятся ваши ближайшие планы.</p>';
}
function selectDay(day, navigate = false) {
  if (!validDay(day)) return;
  state.selected = day;
  if (navigate) { state.cursor = parseDay(day); state.mini = parseDay(day); }
  render();
}
function navigatePeriod(direction) {
  if (state.query || state.agendaAll) return;
  let next;
  if (state.view === "week") next = addDays(state.cursor, 7 * direction);
  else next = new Date(state.cursor.getFullYear(), state.cursor.getMonth() + direction, 1, 12);
  if (!validDay(dayString(next))) return;
  state.cursor = next;
  state.mini = next;
  state.selected = dayString(next);
  render();
}
function setView(view, all = false) {
  state.view = view;
  state.agendaAll = all;
  render();
}
function openEditor(event = null) {
  if (state.loading || !state.user) return;
  state.editing = event;
  const form = $("#event-form");
  form.reset();
  form.elements.title.value = event?.title || "";
  form.elements.date.value = event?.date || state.selected;
  form.elements.category.value = event?.category || "personal";
  form.elements.allDay.checked = event?.allDay || false;
  form.elements.start.value = event?.start || "10:00";
  form.elements.end.value = event?.end || "11:00";
  form.elements.location.value = event?.location || "";
  form.elements.description.value = event?.description || "";
  form.elements.completed.checked = event?.completed || false;
  $("#dialog-title").textContent = event ? "Событие на орбите" : "Новое событие";
  $("#delete-event").hidden = !event;
  $("#completed-field").hidden = !event;
  $("#form-error").hidden = true;
  $("#delete-confirmation").hidden = true;
  setBusy(false);
  toggleAllDay();
  $("#event-dialog").showModal();
  $("#event-title").focus();
}
function toggleAllDay() {
  const form = $("#event-form");
  const allDay = form.elements.allDay.checked;
  $("#time-fields").hidden = allDay;
  form.elements.start.required = !allDay;
  form.elements.end.required = !allDay;
  form.elements.start.disabled = allDay;
  form.elements.end.disabled = allDay;
}
function setBusy(busy) {
  state.busy = busy;
  const form = $("#event-form");
  form.setAttribute("aria-busy", busy);
  for (const element of form.elements) element.disabled = busy;
  if (!busy) toggleAllDay();
  $("#save-event").textContent = busy ? "Сохранение…" : "Сохранить событие";
}
function showFormError(error) { $("#form-error").textContent = error.message; $("#form-error").hidden = false; }
function closeEditor() { if (!state.busy) $("#event-dialog").close(); }
async function saveEvent(event) {
  event.preventDefault();
  if (state.busy) return;
  const form = event.target;
  const values = Object.fromEntries(new FormData(form));
  const payload = {title: values.title.trim(), date: values.date, category: values.category, start: values.start || "", end: values.end || "", allDay: form.elements.allDay.checked, completed: form.elements.completed.checked, description: values.description, location: values.location};
  if (!payload.allDay && payload.end <= payload.start) { showFormError(new Error("Время окончания должно быть позже начала в тот же день")); return; }
  setBusy(true);
  $("#form-error").hidden = true;
  try {
    const saved = await request(state.editing ? `/api/events/${encodeURIComponent(state.editing.id)}` : "/api/events", state.editing ? "PUT" : "POST", payload);
    state.events = state.events.filter(item => item.id !== saved.id).concat(saved).sort((a, b) => a.date.localeCompare(b.date) || a.start.localeCompare(b.start));
    state.selected = saved.date;
    state.cursor = parseDay(saved.date);
    state.mini = parseDay(saved.date);
    setBusy(false);
    closeEditor();
    render();
    updateConnection();
    notify(state.editing ? "Изменения сохранены" : "Новое событие на вашей орбите");
  } catch (error) { setBusy(false); showFormError(error); }
}
async function deleteEvent() {
  if (state.busy || !state.editing) return;
  setBusy(true);
  try {
    const deleted = {...state.editing};
    await request(`/api/events/${encodeURIComponent(deleted.id)}`, "DELETE");
    state.events = state.events.filter(event => event.id !== deleted.id);
    state.deleted = deleted;
    setBusy(false);
    closeEditor();
    render();
    updateConnection();
    notify("Событие удалено", false, true);
  } catch (error) { setBusy(false); showFormError(error); }
}
async function undoDelete() {
  if (!state.deleted) return;
  $("#undo-delete").disabled = true;
  clearTimeout(toastTimer);
  try {
    const {id, ...payload} = state.deleted;
    const restored = await request("/api/events", "POST", payload);
    state.events.push(restored);
    state.events.sort((a, b) => a.date.localeCompare(b.date) || a.start.localeCompare(b.start));
    state.deleted = null;
    render();
    updateConnection();
    notify("Событие восстановлено");
  } catch (error) { notify(error.message, true, true); }
  finally { $("#undo-delete").disabled = false; }
}
async function toggleCompleted(id, button) {
  const event = state.events.find(item => item.id === id);
  if (!event) return;
  button.disabled = true;
  try {
    const saved = await request(`/api/events/${encodeURIComponent(id)}`, "PUT", {...event, completed: !event.completed});
    state.events = state.events.map(item => item.id === id ? saved : item);
    render();
    notify(saved.completed ? "Событие завершено. На шаг ближе к звёздам!" : "Событие снова в плане");
  } catch (error) { button.disabled = false; notify(error.message, true); }
}
function notify(message, error = false, undo = false) {
  clearTimeout(toastTimer);
  $("#toast-text").textContent = message;
  $("#toast").classList.toggle("error", error);
  $("#toast").hidden = false;
  $("#undo-delete").hidden = !undo;
  if (!undo) toastTimer = setTimeout(() => { $("#toast").hidden = true; }, error ? 8000 : 4500);
}

document.addEventListener("click", event => {
  const target = event.target instanceof Element ? event.target : null;
  if (!target) return;
  const eventButton = target.closest("[data-event]");
  const complete = target.closest("[data-complete]");
  if (complete) { toggleCompleted(complete.dataset.complete, complete); return; }
  if (eventButton) { openEditor(state.events.find(item => item.id === eventButton.dataset.event)); return; }
  if (target.closest(".create-event")) { openEditor(); return; }
  const mini = target.closest("[data-mini-day]");
  if (mini && !mini.disabled) { selectDay(mini.dataset.miniDay, true); return; }
  const cell = target.closest("[data-day]");
  if (cell) selectDay(cell.dataset.day);
});
$("#category-filters").addEventListener("change", event => {
  const category = event.target.dataset.category;
  if (event.target.checked) state.filters.add(category); else state.filters.delete(category);
  render();
});
$("#calendar").addEventListener("dblclick", event => {
  if (event.target.closest("[data-event], [data-complete]")) return;
  const cell = event.target.closest("[data-day]");
  if (cell && validDay(cell.dataset.day)) { selectDay(cell.dataset.day); openEditor(); }
});
$("#calendar").addEventListener("keydown", event => {
  const button = event.target.closest(".day-number[data-select-day]");
  const offset = {ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7}[event.key];
  if (!button || !offset) return;
  event.preventDefault();
  const day = dayString(addDays(parseDay(button.dataset.selectDay), offset));
  if (!validDay(day)) return;
  const inView = $("#calendar").querySelector(`[data-select-day="${day}"]`);
  selectDay(day, !inView);
  $("#calendar").querySelector(`[data-select-day="${day}"]`)?.focus();
});
$("#prev").addEventListener("click", () => navigatePeriod(-1));
$("#next").addEventListener("click", () => navigatePeriod(1));
$("#today").addEventListener("click", () => {
  $("#search").value = ""; state.query = ""; beforeSearch = null; state.agendaAll = false;
  selectDay(today, true);
});
$("#mini-prev").addEventListener("click", () => { state.mini = new Date(state.mini.getFullYear(), state.mini.getMonth() - 1, 1, 12); renderMini(); });
$("#mini-next").addEventListener("click", () => { state.mini = new Date(state.mini.getFullYear(), state.mini.getMonth() + 1, 1, 12); renderMini(); });
document.querySelectorAll("[data-view]").forEach(button => button.addEventListener("click", () => {
  $("#search").value = ""; state.query = ""; beforeSearch = null; setView(button.dataset.view);
}));
$("#nav-calendar").addEventListener("click", () => { $("#search").value = ""; state.query = ""; beforeSearch = null; setView("month"); });
$("#nav-agenda").addEventListener("click", () => { $("#search").value = ""; state.query = ""; beforeSearch = null; setView("agenda", true); });
$("#search").addEventListener("input", event => {
  const query = event.target.value.trim().toLocaleLowerCase("ru-RU");
  if (query && !state.query) beforeSearch = {view: state.view, all: state.agendaAll};
  state.query = query;
  if (query) state.view = "agenda";
  else if (beforeSearch) { state.view = beforeSearch.view; state.agendaAll = beforeSearch.all; beforeSearch = null; }
  render();
});
$("#add-selected").addEventListener("click", () => openEditor());
$("#close-dialog").addEventListener("click", closeEditor);
$("#cancel-dialog").addEventListener("click", closeEditor);
$("#event-dialog").addEventListener("cancel", event => { if (state.busy) event.preventDefault(); });
$("#event-form").elements.allDay.addEventListener("change", toggleAllDay);
$("#event-form").addEventListener("submit", saveEvent);
$("#delete-event").addEventListener("click", () => { $("#delete-confirmation").hidden = false; $("#confirm-delete").focus(); });
$("#cancel-delete").addEventListener("click", () => { $("#delete-confirmation").hidden = true; $("#delete-event").focus(); });
$("#confirm-delete").addEventListener("click", deleteEvent);
$("#undo-delete").addEventListener("click", undoDelete);
$("#close-toast").addEventListener("click", () => { $("#toast").hidden = true; });
document.addEventListener("keydown", event => {
  const editable = event.target instanceof Element && event.target.closest("input, textarea, select, [contenteditable=true]");
  if (editable || event.ctrlKey || event.metaKey || event.altKey || $("#event-dialog").open) return;
  if (event.key.toLowerCase() === "n" || event.key.toLowerCase() === "т") { event.preventDefault(); openEditor(); }
  if (event.key === "/") { event.preventDefault(); $("#search").focus(); }
});
function setAuthMode(mode) {
  authMode = mode;
  const registration = mode === "register";
  $("#auth-name-label").hidden = !registration;
  $("#auth-name").required = registration;
  $("#auth-password").autocomplete = registration ? "new-password" : "current-password";
  $("#auth-submit").textContent = registration ? "Создать аккаунт" : "Войти на орбиту";
  $("#auth-login-tab").classList.toggle("selected", !registration);
  $("#auth-login-tab").setAttribute("aria-pressed", !registration);
  $("#auth-register-tab").classList.toggle("selected", registration);
  $("#auth-register-tab").setAttribute("aria-pressed", registration);
  $("#auth-error").hidden = true;
}
function showAuth() {
  state.authVersion++;
  state.user = null; state.events = []; state.deleted = null; state.editing = null;
  state.query = ""; $("#search").value = ""; beforeSearch = null;
  $("#auth-screen").hidden = false;
  $(".app-shell").inert = true;
  $("#logout").hidden = true;
  $("#toast").hidden = true;
  if ($("#event-dialog").open) $("#event-dialog").close();
  render();
}
async function signedIn(user) {
  state.authVersion++; state.user = user; state.loading = true;
  $("#auth-screen").hidden = true; $(".app-shell").inert = false; $("#logout").hidden = false;
  $(".profile strong").textContent = user.name;
  $(".profile div > span").textContent = user.email;
  $(".avatar").textContent = [...user.name][0]?.toUpperCase() || "К";
  $("#auth-password").value = "";
  await loadEvents();
}
async function initializeSession() {
  try { await signedIn(await request("/api/auth/me")); }
  catch (error) { state.loading = false; showAuth(); if (error.status !== 401) { $("#auth-error").textContent = "Не удалось связаться с сервером. Проверьте его запуск."; $("#auth-error").hidden = false; } }
}
$("#auth-login-tab").addEventListener("click", () => setAuthMode("login"));
$("#auth-register-tab").addEventListener("click", () => setAuthMode("register"));
$("#auth-form").addEventListener("submit", async event => {
  event.preventDefault(); const mode = authMode;
  const payload = {email: $("#auth-email").value, password: $("#auth-password").value};
  if (mode === "register") payload.name = $("#auth-name").value;
  const controls = [...event.target.elements, $("#auth-login-tab"), $("#auth-register-tab")];
  controls.forEach(control => control.disabled = true); $("#auth-error").hidden = true;
  try { const result = await request(`/api/auth/${mode}`, "POST", payload); await signedIn(result.user); }
  catch (error) { $("#auth-error").textContent = error.message; $("#auth-error").hidden = false; }
  finally { controls.forEach(control => control.disabled = false); }
});
$("#logout").addEventListener("click", async () => {
  $("#logout").disabled = true;
  try { await request("/api/auth/logout", "POST"); showAuth(); $("#auth-password").value = ""; }
  catch (error) { if (error.status === 401) showAuth(); else notify(error.message, true); }
  finally { $("#logout").disabled = false; }
});
setInterval(async () => {
  if (!state.user || state.busy || document.hidden || !state.events.some(event => event.processingStatus === "pending")) return;
  const version = state.authVersion;
  try { const events = await request("/api/events"); if (version === state.authVersion && state.user) { state.events = events; render(); } }
  catch { /* The next interaction reports connectivity errors. */ }
}, 3000);
render();
initializeSession();

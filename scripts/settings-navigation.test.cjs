// Run with: node --test scripts/settings-navigation.test.cjs
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

class Events {
  constructor() { this.handlers = new Map(); }
  addEventListener(name, fn, capture = false) {
    const handlers = this.handlers.get(name) || [];
    handlers.push({ fn, capture });
    this.handlers.set(name, handlers);
  }
  emit(name, values = {}) {
    const event = {
      target: this, defaultPrevented: false, stopped: false,
      preventDefault() { this.defaultPrevented = true; },
      stopImmediatePropagation() { this.stopped = true; },
      ...values,
    };
    for (const { fn } of [...(this.handlers.get(name) || [])].sort((a, b) => Number(b.capture) - Number(a.capture))) {
      fn(event);
      if (event.stopped) break;
    }
    return event;
  }
}

function fixture({ htmx = true, draft = false, dialog = false, autosave = false } = {}) {
  if (autosave) dialog = true;
  const source = new Events();
  source.values = autosave ? { revision: "7", name: "Saved source", url: "https://old.example", key: "", library: "old-library", enabled: "on", scan_interval: "60" } : { name: "Saved source" };
  source.reset = () => { source.values = { name: "Saved source" }; };
  source.dispatchEvent = event => source.emit(event.type);
  source.matches = selector => selector === "form[data-source-edit]";
  source.dataset = { sourceDirty: String(draft), unsavedPrompt: "Localized discard prompt" };
  if (autosave) Object.assign(source.dataset, { sourceAutosave: "true", sourceId: 'id"]unsafe', saving: "Saving source", saved: "Saved source", failed: "Source failed", noLibraries: "No libraries" });
  source.action = "/sources/example";
  source.reportValidity = () => true;
  const sourceStatus = { textContent: "" }, retry = new Events();
  retry.hidden = true;
  const key = { get value() { return source.values.key; }, set value(value) { source.values.key = value; } };
  const libraries = { set textContent(value) { this.text = value; delete source.values.library; } };
  source.querySelector = selector => ({ "[data-source-save-status]": sourceStatus, "[data-source-retry]": retry, '[name="key"]': key, "[data-source-library-options]": libraries })[selector] || null;
  const settings = new Events();
  settings.dataset = { saving: "Saving", failed: "Failed" };
  settings.reportValidity = () => true;
  const settingsRetry = new Events();
  settingsRetry.hidden = true;
  const document = new Events();
  const diagnosticPanel = { open: false };
  const sourceDialog = new Events();
  source.closest = selector => selector === "dialog" ? sourceDialog : null;
  sourceDialog.dataset = { sourceAutoOpen: String(draft) };
  sourceDialog.open = false;
  sourceDialog.contains = form => form === source;
  sourceDialog.removeAttribute = () => {};
  sourceDialog.showModal = () => { sourceDialog.open = true; };
  sourceDialog.close = () => { sourceDialog.open = false; };
  const heading = { textContent: "" }, scan = { disabled: false }, deleteRevision = { value: "7" };
  sourceDialog.querySelector = selector => selector.startsWith('form[data-source-dirty="true"]') && draft && !autosave ? source : selector === "a[data-source-close]" ? { href: "/settings?tab=media" } : selector === "[data-source-heading-name]" ? heading : null;
  sourceDialog.querySelectorAll = selector => selector === 'input[name="revision"]' ? [{ set value(value) { source.values.revision = value; } }, deleteRevision] : selector === 'form[action$="/scan"] button[type="submit"]' ? [scan] : [];
  sourceDialog.getBoundingClientRect = () => ({ left: 100, right: 300, top: 100, bottom: 300 });
  const tileFields = {};
  const tile = { dataset: { sourceTile: 'id"]unsafe' }, querySelector: selector => tileFields[selector] ||= { textContent: "" } };
  document.body = new Events();
  document.querySelectorAll = selector => selector === "form[data-source-edit]" ? [source] : selector === "dialog.source-dialog" && dialog ? [sourceDialog] : selector === "[data-source-tile]" ? [tile] : [];
  document.getElementById = id => id === "settings-form" ? settings : id === "settings-retry" ? settingsRetry : id === "diagnostics" ? diagnosticPanel : id === "source-dialog" ? sourceDialog : null;
  const window = new Events();
  const prompts = [], navigations = [], saves = [], requests = [], actions = [];
  let answer = false;
  window.confirm = text => { prompts.push(text); return answer; };
  window.location = { assign: href => navigations.push(href) };
  window.matchMedia = () => ({ addEventListener() {} });
  if (htmx) window.htmx = { trigger: (form, event) => saves.push({ form, event }) };
  const FormData = class {
    constructor(form) { this.values = Object.entries(form.values || {}); }
    entries() { return this.values; }
    [Symbol.iterator]() { return this.values[Symbol.iterator](); }
  };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, "../internal/web/assets/static/app.js"), "utf8"), {
    document, window, FormData, URLSearchParams, Event: class { constructor(type) { this.type = type; } }, navigator: {}, console,
    fetch: (url, options) => new Promise((resolve, reject) => requests.push({ url, options, resolve, reject })),
  });
  document.emit("DOMContentLoaded");
  const link = { href: "/settings?tab=metadata", closest: selector => selector === "a[data-settings-tab]" ? link : null };
  return {
    source, settings, window, prompts, navigations, saves, diagnosticPanel, sourceDialog,
    requests, sourceStatus, retry, settingsRetry, actions, deleteRevision, tileFields, heading, scan, libraries,
    openSource() {
      const link = { dataset: { sourceDialog: "source-dialog" }, closest: selector => selector === "a[data-source-dialog]" ? link : null };
      return document.emit("click", { target: link, button: 0 });
    },
    closeSource() {
      const link = { href: "/settings?tab=media", closest: selector => selector === "a[data-source-close]" ? link : selector === "dialog" ? sourceDialog : null };
      return document.emit("click", { target: link, button: 0 });
    },
    cancelSource() { return sourceDialog.emit("cancel"); },
    sourceAction() {
      const form = {
        matches: () => false,
        requestSubmit(submitter) {
          const event = sourceDialog.emit("submit", { target: form, submitter });
          if (!event.defaultPrevented) actions.push(submitter);
        },
      };
      return sourceDialog.emit("submit", { target: form, submitter: { name: "action" } });
    },
    backdrop(start = [50, 50], end = [50, 50], target = sourceDialog) {
      sourceDialog.emit("pointerdown", { target, button: 0, clientX: start[0], clientY: start[1] });
      return sourceDialog.emit("click", { target: sourceDialog, button: 0, clientX: end[0], clientY: end[1] });
    },
    commit() { return source.emit("change"); },
    blur() { return source.emit("focusout", { target: { name: "name", type: "text" } }); },
    async settle(index = requests.length - 1, overrides = {}, status = 200) {
      requests[index].resolve({ status, json: async () => ({ revision: 8 + index, name: "Server name", url: "https://new.example", enabled: true, scanInterval: 60, librarySummary: "Libraries", scanSummary: "Schedule", librariesChanged: false, ...overrides }) });
      await tick();
    },
    link() {
      const link = { href: "/other", closest: selector => selector === "a[href]" ? link : null };
      return document.emit("click", { target: link, button: 0 });
    },
    answer(value) { answer = value; },
    edit(value = "Unsaved source") { source.values.name = value; source.emit("input"); },
    tab() { return document.emit("click", { target: link, button: 0 }); },
    diagnostics() {
      const diagnosticLink = { closest: selector => selector === "a[data-diagnostics-link]" ? diagnosticLink : null };
      return document.emit("click", { target: diagnosticLink, button: 0 });
    },
    unload() { return window.emit("beforeunload"); },
    response(state) {
      settings.emit("htmx:afterRequest", {
        detail: { xhr: { status: 200, getResponseHeader: name => name === "X-Waim-Save" ? state : null } },
      });
    },
  };
}

const tick = () => new Promise(resolve => setImmediate(resolve));

test("dirty source tab navigation asks locally and never auto-saves the source", () => {
  const f = fixture();
  f.edit();
  assert.equal(f.tab().defaultPrevented, true);
  assert.deepEqual(f.prompts, ["Localized discard prompt"]);
  assert.equal(f.saves.length, 0);
  assert.equal(f.unload().defaultPrevented, true);
  f.answer(true);
  assert.equal(f.tab().defaultPrevented, false);
  assert.equal(f.unload().defaultPrevented, false);
  assert.equal(f.saves.length, 0);
});

test("reverted changes stop warning; explicit source submission is allowed", () => {
  const f = fixture();
  f.edit();
  f.edit("Saved source");
  assert.equal(f.tab().defaultPrevented, false);
  assert.equal(f.prompts.length, 0);
  f.edit();
  assert.equal(f.source.emit("submit").defaultPrevented, false);
  assert.equal(f.unload().defaultPrevented, false);
  assert.equal(f.saves.length, 0);
});

test("source protection works without HTMX and for server-returned drafts", () => {
  const f = fixture({ htmx: false, draft: true });
  assert.equal(f.tab().defaultPrevented, true);
  assert.equal(f.unload().defaultPrevented, true);
  assert.equal(f.source.emit("submit").defaultPrevented, false);
  assert.equal(f.unload().defaultPrevented, false);
});

test("failed global save re-arms source protection after discard was approved", () => {
  const f = fixture();
  f.edit();
  f.settings.emit("input", { target: { name: "scan_interval" } });
  f.answer(true);
  assert.equal(f.tab().defaultPrevented, true);
  assert.equal(f.saves.length, 1);
  assert.equal(f.saves[0].form, f.settings);
  f.response("failed");
  assert.equal(f.navigations.length, 0);
  assert.equal(f.unload().defaultPrevented, true);
  f.answer(false);
  assert.equal(f.tab().defaultPrevented, true);
  assert.equal(f.saves.length, 1);
});

test("a fresh source edit while global save is pending requires confirmation again", () => {
  const f = fixture();
  f.edit();
  f.settings.emit("htmx:beforeRequest");
  f.answer(true);
  assert.equal(f.tab().defaultPrevented, true);
  f.edit("A newer unsaved source edit");
  f.answer(false);
  f.response("ok");
  assert.equal(f.navigations.length, 0);
  assert.equal(f.prompts.length, 2);
  assert.equal(f.saves.length, 0);
});

test("approved discard waits for global autosave but never submits source values", () => {
  const f = fixture();
  f.edit();
  f.settings.emit("input", { target: { name: "scan_interval" } });
  f.answer(true);
  assert.equal(f.tab().defaultPrevented, true);
  f.settings.emit("htmx:beforeRequest");
  f.response("ok");
  assert.deepEqual(f.navigations, ["/settings?tab=metadata"]);
  assert.equal(f.saves.length, 1);
  assert.equal(f.saves[0].form, f.settings);
  assert.equal(f.unload().defaultPrevented, false);
});

test("diagnostic links open the stable outer details panel without saving", () => {
  const f = fixture();
  f.diagnostics();
  assert.equal(f.diagnosticPanel.open, true);
  assert.equal(f.saves.length, 0);
});

test("source dialogs open locally and guard close/cancel without auto-saving", async () => {
  const f = fixture({ dialog: true });
  assert.equal(f.openSource().defaultPrevented, true);
  assert.equal(f.sourceDialog.open, true);
  f.edit();
  f.closeSource();
  await tick();
  assert.equal(f.sourceDialog.open, true);
  assert.equal(f.cancelSource().defaultPrevented, true);
  await tick();
  f.answer(true);
  f.closeSource();
  assert.equal(f.sourceDialog.open, false);
  assert.equal(f.unload().defaultPrevented, false);
  assert.equal(f.source.values.name, "Saved source");
  f.openSource();
  assert.equal(f.source.values.name, "Saved source");
  assert.equal(f.saves.length, 0);
});

test("source dialogs reopen failed drafts and require discard before stored-setting actions", async () => {
  const f = fixture({ dialog: true, draft: true });
  assert.equal(f.sourceDialog.open, true);
  assert.equal(f.sourceAction().defaultPrevented, true);
  await tick();
  f.answer(true);
  assert.equal(f.sourceAction().defaultPrevented, true);
  assert.equal(f.actions.length, 1);
  assert.equal(f.unload().defaultPrevented, false);
  assert.equal(f.saves.length, 0);
});

test("discarding a server-returned source draft reloads its saved values", () => {
  const f = fixture({ dialog: true, draft: true });
  f.answer(true);
  f.closeSource();
  assert.deepEqual(f.navigations, ["/settings?tab=media"]);
  assert.equal(f.saves.length, 0);
});

test("existing source saves on commit, not input, and change plus blur never duplicate requests", async () => {
  const f = fixture({ autosave: true });
  f.edit("Updated");
  await tick();
  assert.equal(f.requests.length, 0);
  assert.equal(f.unload().defaultPrevented, true);
  f.commit();
  f.blur();
  await tick();
  assert.equal(f.requests.length, 1);
  assert.equal(f.sourceStatus.textContent, "Saving source");
  const request = f.requests[0];
  assert.equal(request.options.method, "POST");
  assert.equal(request.options.credentials, "same-origin");
  assert.equal(request.options.headers["X-Waim-Source-Autosave"], "true");
  assert.equal(request.options.headers["Content-Type"], "application/x-www-form-urlencoded");
  assert.equal(request.options.body.get("name"), "Updated");
  assert.equal(request.options.body.get("revision"), "7");
  await f.settle();
  assert.equal(f.requests.length, 1);
  assert.equal(f.sourceStatus.textContent, "Saved source");
  assert.equal(f.retry.hidden, true);
  assert.equal(f.unload().defaultPrevented, false);
  assert.equal(f.source.values.revision, "8");
  assert.equal(f.deleteRevision.value, "8");
  assert.equal(f.source.values.name, "Updated");
  assert.equal(f.heading.textContent, "Server name");
  assert.equal(f.tileFields["[data-source-libraries]"].textContent, "Libraries");
  assert.equal(f.tileFields["[data-source-schedule]"].textContent, "Schedule");
  assert.equal(f.tileFields["[data-source-next]"].textContent, "");
});

test("committed edits coalesce in flight using the saved revision and no acknowledged key", async () => {
  const f = fixture({ autosave: true });
  f.source.values.key = "first-key";
  f.edit("First");
  f.commit();
  await tick();
  f.edit("Second");
  f.commit();
  f.edit("Third");
  f.commit();
  assert.equal(f.requests.length, 1);
  await f.settle(0);
  assert.equal(f.requests.length, 2);
  assert.equal(f.source.values.key, "");
  assert.equal(f.requests[1].options.body.get("name"), "Third");
  assert.equal(f.requests[1].options.body.get("key"), "");
  assert.equal(f.requests[1].options.body.get("revision"), "8");
  await f.settle(1);
  assert.equal(f.unload().defaultPrevented, false);
  assert.equal(f.source.values.revision, "9");
});

test("newer uncommitted input and replacement key survive an in-flight response", async () => {
  const f = fixture({ autosave: true });
  f.source.values.key = "first-key";
  f.commit();
  await tick();
  f.source.values.key = "replacement";
  f.edit("Still typing");
  await f.settle(0);
  assert.equal(f.requests.length, 1);
  assert.equal(f.source.values.key, "replacement");
  assert.equal(f.source.values.name, "Still typing");
  assert.equal(f.unload().defaultPrevented, true);
  f.blur();
  await tick();
  assert.equal(f.requests[1].options.body.get("key"), "replacement");
  await f.settle(1);
  assert.equal(f.source.values.key, "");
  assert.equal(f.unload().defaultPrevented, false);
});

test("new typing postpones a queued commit until the next blur", async () => {
  const f = fixture({ autosave: true });
  f.edit("First");
  f.commit();
  await tick();
  f.edit("Committed second");
  f.commit();
  f.edit("Still typing third");
  await f.settle(0);
  assert.equal(f.requests.length, 1);
  assert.equal(f.unload().defaultPrevented, true);
  f.blur();
  await tick();
  assert.equal(f.requests[1].options.body.get("name"), "Still typing third");
  await f.settle(1);
});

test("composition does not autosave and closing waits for composition and its final save", async () => {
  const f = fixture({ autosave: true });
  f.openSource();
  f.source.emit("compositionstart");
  f.edit("Composing");
  await tick();
  assert.equal(f.requests.length, 0);
  f.closeSource();
  await tick();
  assert.equal(f.requests.length, 0);
  assert.equal(f.sourceDialog.open, true);
  f.edit("Final composition");
  f.source.emit("compositionend");
  await tick();
  assert.equal(f.requests.length, 1);
  assert.equal(f.requests[0].options.body.get("name"), "Final composition");
  await f.settle();
  assert.equal(f.sourceDialog.open, false);
});

test("plain composition completion without a commit does not save", async () => {
  const f = fixture({ autosave: true });
  f.source.emit("compositionstart");
  f.edit("Composed");
  f.source.emit("compositionend");
  await tick();
  assert.equal(f.requests.length, 0);
  f.blur();
  await tick();
  assert.equal(f.requests.length, 1);
  await f.settle();
});

test("close and Escape wait for the latest edit without duplicate submissions or discard prompts", async () => {
  const f = fixture({ autosave: true });
  f.openSource();
  f.edit("First");
  f.commit();
  await tick();
  f.closeSource();
  f.cancelSource();
  f.edit("Latest");
  await f.settle(0);
  assert.equal(f.sourceDialog.open, true);
  assert.equal(f.requests.length, 2);
  assert.equal(f.requests[1].options.body.get("name"), "Latest");
  await f.settle(1);
  assert.equal(f.sourceDialog.open, false);
  assert.equal(f.prompts.length, 0);
});

test("source actions wait for save, replay once, retain submitter and update removal revision", async () => {
  const f = fixture({ autosave: true });
  f.edit();
  assert.equal(f.sourceAction().defaultPrevented, true);
  f.sourceAction();
  await tick();
  assert.equal(f.actions.length, 0);
  assert.equal(f.requests.length, 1);
  await f.settle();
  assert.equal(f.deleteRevision.value, "8");
  assert.equal(f.actions.length, 1);
  assert.equal(f.actions[0].name, "action");
  assert.equal(f.prompts.length, 0);
  f.sourceAction();
  assert.equal(f.actions.length, 1);
});

test("failed saves keep drafts, dialog and navigation in place, with explicit retry", async () => {
  const f = fixture({ autosave: true });
  f.openSource();
  f.edit();
  f.closeSource();
  await tick();
  await f.settle(0, { error: "Localized conflict", revision: undefined }, 409);
  assert.equal(f.sourceDialog.open, true);
  assert.equal(f.source.values.name, "Unsaved source");
  assert.equal(f.sourceStatus.textContent, "Localized conflict");
  assert.equal(f.retry.hidden, false);
  assert.equal(f.unload().defaultPrevented, true);
  assert.equal(f.source.values.revision, "7");
  f.retry.emit("click");
  await tick();
  assert.equal(f.requests[1].options.body.get("revision"), "7");
  assert.equal(f.retry.hidden, true);
  await f.settle(1);
  f.closeSource();
  await tick();
  assert.equal(f.sourceDialog.open, false);
});

test("plaintext middleware and network errors use localized fallback and do not allow actions", async () => {
  for (const network of [false, true]) {
    const f = fixture({ autosave: true });
    f.edit();
    f.sourceAction();
    await tick();
    if (network) f.requests[0].reject(new TypeError("sensitive network text"));
    else f.requests[0].resolve({ status: 403, json: async () => { throw new Error("not JSON"); } });
    await tick();
    assert.equal(f.sourceStatus.textContent, "Source failed");
    assert.equal(f.retry.hidden, false);
    assert.equal(f.actions.length, 0);
    assert.equal(f.unload().defaultPrevented, true);
  }
});

test("failed validation prevents fetch, close, links and source actions", async () => {
  const f = fixture({ autosave: true });
  f.openSource();
  f.edit();
  f.source.reportValidity = () => false;
  f.closeSource();
  await tick();
  f.link();
  f.sourceAction();
  await tick();
  assert.equal(f.requests.length, 0);
  assert.equal(f.sourceDialog.open, true);
  assert.equal(f.navigations.length, 0);
  assert.equal(f.actions.length, 0);
  assert.equal(f.sourceStatus.textContent, "Source failed");
  assert.equal(f.retry.hidden, false);
});

test("URL changes discard obsolete libraries even if toggled while the save is pending", async () => {
  const f = fixture({ autosave: true });
  f.source.values.url = "https://new.example";
  f.commit();
  await tick();
  f.source.values.library = "stale-new-selection";
  f.edit("Latest");
  f.commit();
  await f.settle(0, { librariesChanged: true, enabled: false });
  assert.equal(f.libraries.text, "No libraries");
  assert.equal(f.source.values.library, undefined);
  assert.equal(f.requests[1].options.body.has("library"), false);
  assert.equal(f.scan.disabled, true);
  await f.settle(1);
  assert.equal(f.unload().defaultPrevented, false);
});

test("backdrop requires a pointer starting and ending outside the dialog rectangle", async () => {
  const f = fixture({ autosave: true });
  f.openSource();
  f.edit();
  f.backdrop([150, 150], [150, 150]); // Dialog padding is not a backdrop.
  f.backdrop([150, 150], [50, 50]); // Dragging out of the dialog is not a click outside.
  f.backdrop([50, 50], [50, 50], f.source); // Nor is dragging a form control.
  await tick();
  assert.equal(f.requests.length, 0);
  assert.equal(f.sourceDialog.open, true);
  f.backdrop();
  await tick();
  assert.equal(f.requests.length, 1);
  assert.equal(f.sourceDialog.open, true);
  await f.settle();
  assert.equal(f.sourceDialog.open, false);
});

test("all navigation links wait for source saves even without HTMX", async () => {
  for (const tab of [true, false]) {
    const f = fixture({ autosave: true, htmx: false });
    f.edit();
    assert.equal((tab ? f.tab() : f.link()).defaultPrevented, true);
    await tick();
    assert.equal(f.navigations.length, 0);
    await f.settle();
    assert.deepEqual(f.navigations, [tab ? "/settings?tab=metadata" : "/other"]);
  }
});

test("navigation still awaits global autosave after the source finishes", async () => {
  const f = fixture({ autosave: true });
  f.edit();
  f.settings.emit("input", { target: { name: "scan_interval" } });
  f.tab();
  await tick();
  await f.settle();
  assert.equal(f.navigations.length, 0);
  assert.equal(f.saves.length, 1);
  f.settings.emit("htmx:beforeRequest");
  f.response("ok");
  await tick();
  assert.deepEqual(f.navigations, ["/settings?tab=metadata"]);
});

test("a failed source save never aborts an already pending global save", async () => {
  const f = fixture({ autosave: true });
  f.settings.emit("htmx:beforeRequest");
  f.edit();
  f.tab();
  await tick();
  await f.settle(0, { error: "Conflict" }, 409);
  f.response("ok");
  await tick();
  assert.equal(f.navigations.length, 0);
  assert.equal(f.sourceStatus.textContent, "Conflict");
  assert.equal(f.unload().defaultPrevented, true);
  assert.equal(f.saves.length, 0);
});

test("global retry is visible only after failure and uses the validated settings-save event", () => {
  const f = fixture();
  assert.equal(f.settingsRetry.hidden, true);
  f.settings.emit("htmx:beforeRequest");
  f.response("failed");
  assert.equal(f.settingsRetry.hidden, false);
  f.settings.reportValidity = () => false;
  f.settingsRetry.emit("click");
  assert.equal(f.saves.length, 0);
  assert.equal(f.settingsRetry.hidden, false);
  f.settings.reportValidity = () => true;
  f.settingsRetry.emit("click");
  assert.equal(f.saves.length, 1);
  assert.equal(f.saves[0].form, f.settings);
  assert.equal(f.saves[0].event, "settings-save");
  f.settings.emit("htmx:beforeRequest");
  assert.equal(f.settingsRetry.hidden, true);
  f.settingsRetry.emit("click");
  f.settings.emit("submit");
  assert.equal(f.saves.length, 1);
  f.response("ok");
  assert.equal(f.settingsRetry.hidden, true);
  assert.equal(f.unload().defaultPrevented, false);
});

test("URL failure retains the draft and replacement key until retry succeeds", async () => {
  const f = fixture({ autosave: true });
  f.openSource();
  f.source.values.url = "https://replacement.example";
  f.commit();
  await tick();
  await f.settle(0, { error: "A new key is required" }, 400);
  assert.equal(f.source.values.url, "https://replacement.example");
  assert.equal(f.source.values.revision, "7");
  f.source.values.key = "replacement-secret";
  f.source.emit("input");
  f.retry.emit("click");
  await tick();
  assert.equal(f.source.values.key, "replacement-secret");
  assert.equal(f.requests[1].options.body.get("url"), "https://replacement.example");
  assert.equal(f.requests[1].options.body.get("key"), "replacement-secret");
  assert.equal(f.requests[1].options.body.get("revision"), "7");
  await f.settle(1, { librariesChanged: true });
  assert.equal(f.source.values.key, "");
  assert.equal(f.unload().defaultPrevented, false);
  assert.equal(f.sourceDialog.open, true);
  f.closeSource();
  assert.equal(f.sourceDialog.open, false);
});

test("existing failed drafts cannot be discarded by close, Escape or backdrop", async () => {
  for (const close of ["closeSource", "cancelSource", "backdrop"]) {
    const f = fixture({ autosave: true, draft: true });
    f.answer(true); // Existing sources must never even ask this question.
    f[close]();
    await tick();
    assert.equal(f.sourceDialog.open, true);
    await f.settle(0, { error: "Save conflict" }, 409);
    assert.equal(f.sourceDialog.open, true);
    assert.equal(f.unload().defaultPrevented, true);
    assert.equal(f.sourceStatus.textContent, "Save conflict");
    assert.equal(f.prompts.length, 0);
    assert.equal(f.navigations.length, 0);
  }
});

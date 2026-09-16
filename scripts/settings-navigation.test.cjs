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

function fixture({ htmx = true, draft = false } = {}) {
  const source = new Events();
  source.values = { name: "Saved source" };
  source.dataset = { sourceDirty: String(draft), unsavedPrompt: "Localized discard prompt" };
  const settings = new Events();
  settings.dataset = { saving: "Saving", failed: "Failed" };
  settings.reportValidity = () => true;
  const document = new Events();
  document.body = new Events();
  document.querySelectorAll = selector => selector === "form[data-source-edit]" ? [source] : [];
  document.getElementById = id => id === "settings-form" ? settings : null;
  const window = new Events();
  const prompts = [], navigations = [], saves = [];
  let answer = false;
  window.confirm = text => { prompts.push(text); return answer; };
  window.location = { assign: href => navigations.push(href) };
  window.matchMedia = () => ({ addEventListener() {} });
  if (htmx) window.htmx = { trigger: (form, event) => saves.push({ form, event }) };
  const FormData = class {
    constructor(form) { this.form = form; }
    entries() { return Object.entries(this.form.values || {}); }
  };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, "../internal/web/assets/static/app.js"), "utf8"), {
    document, window, FormData, navigator: {}, console,
  });
  document.emit("DOMContentLoaded");
  const link = { href: "/settings?tab=metadata", closest: () => link };
  return {
    source, settings, window, prompts, navigations, saves,
    answer(value) { answer = value; },
    edit(value = "Unsaved source") { source.values.name = value; source.emit("input"); },
    tab() { return document.emit("click", { target: link, button: 0 }); },
    unload() { return window.emit("beforeunload"); },
    response(state) {
      settings.emit("htmx:afterRequest", {
        detail: { xhr: { status: 200, getResponseHeader: name => name === "X-Waim-Save" ? state : null } },
      });
    },
  };
}

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

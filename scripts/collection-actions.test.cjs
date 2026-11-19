const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

function fixture(context = "suggestions") {
  const handlers = {};
  const requests = [], scrolls = [], controls = [];
  function control(kind = "Series", id = "42") {
    const feedback = { hidden: true, textContent: "" }, button = { disabled: false, textContent: "Watch" };
    const el = { dataset: { kind, id, watchContext: context, failed: "Localized failure", added: "Already tracked", remove: "Stop watching" } };
    const form = {
      action: "/collection/add", add: true,
      values: { kind, id, _epoch: "fixture-epoch" },
      matches: () => form.add,
      getAttribute: () => form.action,
      closest: () => el,
      reportValidity: () => true,
      querySelector: () => button,
      removeAttribute: () => { form.add = false; },
      replaceWith: link => { el.link = link; el.form = null; },
    };
    el.form = form;
    el.querySelector = selector => selector === "form" ? el.form : selector === "button" ? (el.form ? button : null) : feedback;
    controls.push(el);
    return { el, form, feedback, button };
  }
  const first = control(), duplicate = control(), unrelated = control("Movie", "42");
  const count = { textContent: "0" }, list = {
    dataset: { collectionRevision: "0" }, entries: [],
    querySelectorAll() { return this.entries; },
    appendChild(entry) { this.entries.push({ dataset: { collectionEntry: entry.key } }); },
  };
  const empty = { remove() { this.removed = true; } };
  const document = {
    body: { addEventListener() {} },
    addEventListener(name, fn) { (handlers[name] ||= []).push(fn); },
    querySelectorAll: selector => selector === "[data-watch-control]" ? controls : [],
    getElementById: id => context === "collection" ? ({ "virtual-entries": list, "virtual-entry-count": count, "virtual-entries-empty": empty }[id] || null) : null,
    createElement(tag) {
      if (tag === "template") return { set innerHTML(value) { this.content = { key: value }; } };
      return { appendChild(child) { this.child = child; } };
    },
  };
  const window = {
    scrollX: 0, scrollY: 800, matchMedia: () => ({ addEventListener() {} }),
    addEventListener() {}, scrollTo(x, y) { scrolls.push([x, y]); },
  };
  const FormData = class {
    constructor(form) { this.values = Object.entries(form.values); }
    [Symbol.iterator]() { return this.values[Symbol.iterator](); }
  };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, "../internal/web/assets/static/app.js"), "utf8"), {
    document, window, navigator: {}, console, URLSearchParams, FormData,
    fetch: (url, options) => new Promise((resolve, reject) => requests.push({ url, options, resolve, reject })),
  });
  handlers.DOMContentLoaded.forEach(fn => fn());
  return {
    first, duplicate, unrelated, requests, scrolls, window, list, count,
    submit(target = first.form) {
      const e = { target, defaultPrevented: false, preventDefault() { this.defaultPrevented = true; } };
      handlers.submit.forEach(fn => fn(e));
      return e;
    },
    async settle(index = 0, overrides = {}) {
      requests[index].resolve({ ok: true, json: async () => ({ kind: "Series", id: 42, count: 1, revision: 1, entryHTML: "series:42", ...overrides }) });
      await new Promise(resolve => setImmediate(resolve));
    },
    async fail(text = "<unsafe> error", index = 0) {
      requests[index].resolve({ ok: false, text: async () => text });
      await new Promise(resolve => setImmediate(resolve));
    },
  };
}

test("watch adds without navigation and updates duplicate suggestions only after success", async () => {
  const f = fixture();
  assert.equal(f.submit().defaultPrevented, true);
  f.submit(f.duplicate.form);
  assert.equal(f.requests.length, 1);
  assert.equal(f.first.button.disabled, true);
  assert.equal(f.duplicate.button.disabled, true);
  assert.equal(f.unrelated.button.disabled, false);
  assert.equal(f.requests[0].options.body.get("_epoch"), "fixture-epoch");
  f.window.scrollY = 1200; // User scrolls while the request is pending.
  await f.settle();
  assert.equal(f.first.el.link.href, "/collection");
  assert.equal(f.duplicate.el.link.child.textContent, "Already tracked");
  assert.equal(f.unrelated.el.form.action, "/collection/add");
  assert.deepEqual(f.scrolls, [[0, 1200]]);
});

test("failed additions keep the button and show text-only errors with retry", async () => {
  const f = fixture();
  f.submit();
  await f.fail();
  assert.equal(f.first.feedback.textContent, "<unsafe> error");
  assert.equal(f.first.feedback.hidden, false);
  assert.equal(f.first.el.link, undefined);
  assert.equal(f.first.button.disabled, false);
  assert.deepEqual(f.scrolls, []);
  f.submit();
  await f.settle(1);
  assert.equal(f.first.el.link.href, "/collection");
});

test("network failure uses localized feedback instead of navigating", async () => {
  const f = fixture();
  f.submit();
  f.requests[0].reject(new Error("network details"));
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(f.first.feedback.textContent, "Localized failure");
  assert.equal(f.first.button.disabled, false);
});

test("collection search additions update membership list/count without duplicates", async () => {
  const f = fixture("collection");
  f.submit();
  await f.settle();
  assert.equal(f.first.form.action, "/collection/remove");
  assert.equal(f.duplicate.form.action, "/collection/remove");
  assert.equal(f.first.form.add, false);
  assert.equal(f.first.button.textContent, "Stop watching");
  assert.equal(f.list.entries.length, 1);
  assert.equal(f.count.textContent, "1");
  assert.equal(f.submit().defaultPrevented, false); // Removal remains native.
});

test("out-of-order collection additions do not roll back the count", async () => {
  const f = fixture("collection");
  f.submit();
  f.submit(f.unrelated.form);
  await f.settle(1, { kind: "Movie", count: 2, revision: 2, entryHTML: "movie:42" });
  await f.settle(0);
  assert.equal(f.list.entries.length, 2);
  assert.equal(f.count.textContent, "2");
  assert.equal(f.list.dataset.collectionRevision, "2");
});

test("static demo forms and malformed responses cannot masquerade as saved additions", async () => {
  const f = fixture();
  f.first.form.action = "#";
  assert.equal(f.submit().defaultPrevented, false);
  assert.equal(f.requests.length, 0);
  f.first.form.action = "/collection/add";
  f.submit();
  await f.settle(0, { id: 99 });
  assert.equal(f.first.el.link, undefined);
  assert.equal(f.first.feedback.textContent, "Localized failure");
  assert.equal(f.first.button.disabled, false);
});

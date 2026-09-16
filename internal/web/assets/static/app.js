// waim front-end helpers.
(function () {
  "use strict";

  // Live-filter the findings table rows by the search box value and the
  // selected library filter.
  function filterFindings() {
    var box = document.getElementById("finding-search");
    var q = (box ? box.value : "").toLowerCase().trim();
    var libSel = document.getElementById("finding-lib-filter");
    var lib = libSel ? libSel.value : "";
    var rows = document.querySelectorAll("#findings tbody tr");
    for (var i = 0; i < rows.length; i++) {
      var textMatch = rows[i].textContent.toLowerCase().indexOf(q) !== -1;
      var memberships = (rows[i].getAttribute("data-libraries") || "").split(" ");
      var libMatch = !lib || rows[i].getAttribute("data-library") === lib || memberships.indexOf(lib) !== -1;
      rows[i].style.display = textMatch && libMatch ? "" : "none";
    }
  }
  window.waimFilterFindings = filterFindings;

  // Copy text to the clipboard, falling back to execCommand for non-secure
  // (plain http) contexts where navigator.clipboard is unavailable.
  function copyText(text) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).catch(function () {
        legacyCopy(text);
      });
      return;
    }
    legacyCopy(text);
  }

  function legacyCopy(text) {
    var ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    ta.style.position = "fixed";
    ta.style.top = "-1000px";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    try {
      document.execCommand("copy");
    } catch (e) {
      /* ignore */
    }
    document.body.removeChild(ta);
  }

  function onCopyClick(e) {
    var el = e.target.closest ? e.target.closest("[data-copy]") : null;
    if (!el) return;
    e.preventDefault();
    copyText(el.getAttribute("data-copy") || "");
    el.classList.add("copied");
    setTimeout(function () {
      el.classList.remove("copied");
    }, 1200);
  }

  // Limit how many rows of a rated list are visible, controlled by its dropdown.
  function applyRatedLimit(select) {
    var card = select.closest ? select.closest(".card") : null;
    if (!card) return;
    var list = card.querySelector(".rated-list");
    if (!list) return;
    var limit = parseInt(select.value, 10) || 10;
    var rows = list.querySelectorAll(".rated-row");
    for (var i = 0; i < rows.length; i++) {
      rows[i].hidden = i >= limit;
    }
  }

  // Reorder a rated list by the "<data attribute>:<direction>" of its dropdown.
  function applySort(select) {
    var card = select.closest ? select.closest(".card") : null;
    if (!card) return;
    var list = card.querySelector(".rated-list");
    if (!list) return;
    var parts = (select.value || "").split(":");
    var attr = "data-" + parts[0];
    var dir = parts[1] === "asc" ? 1 : -1;
    var rows = Array.prototype.slice.call(list.querySelectorAll(".rated-row"));
    rows.sort(function (a, b) {
      var av = a.getAttribute(attr) || "";
      var bv = b.getAttribute(attr) || "";
      var an = parseFloat(av);
      var bn = parseFloat(bv);
      if (!isNaN(an) && !isNaN(bn)) return (an - bn) * dir;
      return av.localeCompare(bv) * dir;
    });
    for (var i = 0; i < rows.length; i++) {
      list.appendChild(rows[i]);
    }
    var limit = card.querySelector(".rated-limit");
    if (limit) applyRatedLimit(limit);
  }

  function onRatedLimitChange(e) {
    var t = e.target;
    if (!t || !t.classList) return;
    if (t.classList.contains("rated-limit")) {
      applyRatedLimit(t);
    } else if (t.classList.contains("sort-list")) {
      applySort(t);
    }
  }

  // Mobile navigation panel. The toggle lives here rather than in an inline
  // handler so the strict CSP (script-src 'self') stays intact.
  var desktopNav = window.matchMedia("(min-width: 768px)");

  function setHidden(el, hidden) {
    if (!el) return;
    // Attribute rather than the .hidden property: SVG elements do not expose it.
    if (hidden) {
      el.setAttribute("hidden", "");
    } else {
      el.removeAttribute("hidden");
    }
  }

  function setNavOpen(open) {
    var btn = document.getElementById("nav-toggle");
    var panel = document.getElementById("nav-menu");
    if (!btn || !panel) return;
    setHidden(panel, !open);
    btn.setAttribute("aria-expanded", open ? "true" : "false");
    btn.setAttribute(
      "aria-label",
      btn.getAttribute(open ? "data-label-close" : "data-label-open") || ""
    );
    setHidden(btn.querySelector(".nav-icon-open"), open);
    setHidden(btn.querySelector(".nav-icon-close"), !open);
  }

  function onNavClick(e) {
    var btn = e.target.closest ? e.target.closest("#nav-toggle") : null;
    if (btn) {
      setNavOpen(btn.getAttribute("aria-expanded") !== "true");
      return;
    }
    // Close again when a link inside the panel is followed.
    var link = e.target.closest ? e.target.closest("#nav-menu a") : null;
    if (link) setNavOpen(false);
  }

  function onNavKeydown(e) {
    if (e.key === "Escape") setNavOpen(false);
  }

  function onViewportChange(e) {
    if (e.matches) setNavOpen(false);
  }

  // back on every polling GET so the server can answer 204 when nothing changed
  // and htmx leaves the DOM untouched instead of re-swapping it.
  var VIEW_TAG = "X-Waim-View";
  var viewTags = {};

  function regionID(detail) {
    var el = detail.target || detail.elt;
    return el && el.id ? el.id : "";
  }

  function onConfigRequest(e) {
    var id = regionID(e.detail);
    if (id && viewTags[id] && String(e.detail.verb).toLowerCase() === "get") {
      e.detail.headers[VIEW_TAG] = viewTags[id];
    }
  }

  function onAfterRequest(e) {
    var id = regionID(e.detail);
    var xhr = e.detail.xhr;
    if (!id || !xhr) return;
    var tag = xhr.getResponseHeader(VIEW_TAG);
    if (tag) {
      viewTags[id] = tag;
    } else {
      delete viewTags[id];
    }
  }

  // Filter the series dropdown of the statistics page from a search box and
  // load the first match right away.
  function filterSeries() {
    var box = document.getElementById("series-search");
    var select = document.getElementById("series-select");
    if (!box || !select) return;
    var q = box.value.toLowerCase().trim();
    var firstVisible = null;
    var selectedVisible = false;
    for (var i = 0; i < select.options.length; i++) {
      var opt = select.options[i];
      var match = !q || opt.text.toLowerCase().indexOf(q) !== -1;
      opt.hidden = !match;
      opt.disabled = !match;
      if (!match) continue;
      if (!firstVisible) firstVisible = opt;
      if (opt.selected) selectedVisible = true;
    }
    if (!selectedVisible && firstVisible) {
      firstVisible.selected = true;
      select.dispatchEvent(new Event("change", { bubbles: true }));
    }
  }

  // Live preview of the resulting TMDB request volume on the settings page.
  function fillTemplate(tpl, values) {
    return tpl.replace(/\{(\w+)\}/g, function (all, key) {
      return key in values ? values[key] : all;
    });
  }

  function num(value) {
    return Math.round(value).toLocaleString();
  }

  function formatSpan(minutes) {
    if (minutes < 90) return Math.round(minutes) + " min";
    var hours = minutes / 60;
    if (hours < 48) return Math.round(hours * 10) / 10 + " h";
    return Math.round((hours / 24) * 10) / 10 + " d";
  }

  function fieldValue(name, fallback) {
    var el = document.querySelector('[name="' + name + '"]');
    if (!el) return fallback;
    var v = parseFloat(el.value);
    return isNaN(v) ? fallback : v;
  }

  function updateRequestEstimates() {
    var rateOut = document.getElementById("tmdb-rate-estimate");
    if (rateOut) {
      var rps = fieldValue("scan_rate", 0);
      rateOut.textContent = fillTemplate(rateOut.getAttribute("data-tpl") || "", {
        rps: Math.round(rps * 10) / 10,
        perMin: num(rps * 60),
        perHour: num(rps * 3600),
      });
    }

    var cacheOut = document.getElementById("cache-estimate");
    if (!cacheOut) return;
    var enabled = document.querySelector('[name="cache_refresh_enabled"]');
    if (enabled && !enabled.checked) {
      cacheOut.textContent = cacheOut.getAttribute("data-tpl-off") || "";
      return;
    }
    var entries = parseInt(cacheOut.getAttribute("data-entries"), 10) || 0;
    var interval = fieldValue("cache_refresh_interval", 0);
    var percent = fieldValue("cache_refresh_percent", 0);
    if (interval <= 0 || percent <= 0) {
      cacheOut.textContent = "";
      return;
    }
    var batch = Math.ceil((entries * percent) / 100);
    cacheOut.textContent = fillTemplate(cacheOut.getAttribute("data-tpl") || "", {
      entries: num(entries),
      batch: num(batch),
      interval: num(interval),
      perHour: num((batch * 60) / interval),
      full: formatSpan((100 / percent) * interval),
    });
  }

  function sourceAutosave(form) {
    var dialog = form.closest("dialog");
    var status = form.querySelector("[data-source-save-status]");
    var retry = form.querySelector("[data-source-retry]");
    var baseline, pending = null, committed = false, flushing = false, composing = false;
    var failed = form.dataset.sourceDirty === "true", draft = failed;
    var compositionWaiters = [];
    function entries() {
      return Array.from(new FormData(form).entries()).filter(function (entry) { return entry[0] !== "revision"; });
    }
    baseline = JSON.stringify(entries());
    function dirty() { return draft || JSON.stringify(entries()) !== baseline; }
    function indicate(message, error) {
      if (status) status.textContent = message;
      if (retry) retry.hidden = !error;
    }
    function changed() {
      form.dataset.sourceDirty = String(dirty() || failed || !!pending);
    }
    function text(root, selector, value) {
      var el = root.querySelector(selector);
      if (el) el.textContent = value;
    }
    function applySaved(saved, sent) {
      dialog.querySelectorAll('input[name="revision"]').forEach(function (input) { input.value = String(saved.revision); });
      // Acknowledged secrets must not be sent again, nor erase a newer replacement.
      var key = form.querySelector('[name="key"]');
      var sentKey = sent.find(function (entry) { return entry[0] === "key"; });
      if (key && sentKey && key.value === sentKey[1]) key.value = "";
      sent = sent.map(function (entry) { return entry[0] === "key" ? ["key", ""] : entry; });
      if (saved.librariesChanged) {
        var options = form.querySelector("[data-source-library-options]");
        if (options) options.textContent = form.dataset.noLibraries;
        sent = sent.filter(function (entry) { return entry[0] !== "library"; });
      }
      baseline = JSON.stringify(sent);
      draft = false;
      text(dialog, "[data-source-heading-name]", saved.name);
      document.querySelectorAll("[data-source-tile]").forEach(function (tile) {
        if (tile.dataset.sourceTile !== form.dataset.sourceId) return;
        text(tile, "[data-source-name]", saved.name);
        text(tile, "[data-source-url]", saved.url);
        text(tile, "[data-source-libraries]", saved.librarySummary);
        text(tile, "[data-source-schedule]", saved.scanSummary);
        text(tile, "[data-source-next]", "");
      });
      dialog.querySelectorAll('form[action$="/scan"] button[type="submit"]').forEach(function (button) {
        button.disabled = !saved.enabled;
      });
    }
    function save() {
      if (pending) return pending;
      if (composing) return Promise.resolve(false);
      if (!dirty()) {
        committed = false;
        if (failed) indicate(form.dataset.saved, false);
        failed = false;
        changed();
        return Promise.resolve(true);
      }
      if (!form.reportValidity()) {
        failed = true;
        indicate(form.dataset.failed, true);
        changed();
        return Promise.resolve(false);
      }
      committed = false;
      var sent = entries();
      var body = new URLSearchParams(new FormData(form));
      indicate(form.dataset.saving, false);
      pending = Promise.resolve().then(function () {
        return fetch(form.action, {
          method: "POST", credentials: "same-origin", body: body,
          headers: { "X-Waim-Source-Autosave": "true", "Content-Type": "application/x-www-form-urlencoded" }
        });
      }).then(function (response) {
        return response.json().catch(function () { return null; }).then(function (saved) {
          if (response.status !== 200 || !saved || !Number.isInteger(saved.revision)) {
            throw { sourceSaveError: saved && typeof saved.error === "string" ? saved.error : form.dataset.failed };
          }
          applySaved(saved, sent);
          failed = false;
          indicate(form.dataset.saved, false);
          return true;
        });
      }).catch(function (error) {
        failed = true;
        // Network/middleware errors have no localized JSON message.
        indicate(error && error.sourceSaveError || form.dataset.failed, true);
        return false;
      }).then(function (ok) {
        pending = null;
        changed();
        if (ok && dirty() && (committed || flushing) && !composing) return save();
        return ok;
      });
      changed();
      return pending;
    }
    function commit() {
      changed();
      committed = true;
      if (!composing) save();
    }
    form.addEventListener("input", function () {
      committed = false;
      changed();
    });
    form.addEventListener("change", commit);
    form.addEventListener("focusout", function (e) {
      if (e.target.name && e.target.type !== "hidden") commit();
    });
    form.addEventListener("compositionstart", function () { composing = true; committed = false; });
    form.addEventListener("compositionend", function () {
      composing = false;
      compositionWaiters.splice(0).forEach(function (resolve) { resolve(); });
      if (committed) save();
    });
    form.addEventListener("submit", function (e) { e.preventDefault(); commit(); });
    if (retry) retry.addEventListener("click", commit);
    if (failed) indicate(form.dataset.failed, true);
    return {
      form: form,
      unsettled: function () { return dirty() || failed || !!pending; },
      flush: function () {
        flushing = true;
        var ready = composing ? new Promise(function (resolve) { compositionWaiters.push(resolve); }) : Promise.resolve();
        return ready.then(save).then(function (ok) {
          flushing = false;
          return ok && !dirty() && !failed;
        });
      }
    };
  }

  // Existing sources save before leaving; only new-source drafts may be discarded.
  function sourceNavigationGuard() {
    var forms = document.querySelectorAll("form[data-source-edit]");
    var dirtySources = new Set();
    var allowed = false;
    var autosaves = [];
    var navigate = function (href) { window.location.assign(href); };
    var navigationPending = false;
    forms.forEach(function (sourceForm) {
      if (sourceForm.dataset.sourceAutosave === "true") {
        autosaves.push(sourceAutosave(sourceForm));
        return;
      }
      var draft = sourceForm.dataset.sourceDirty === "true";
      var baseline = JSON.stringify(Array.from(new FormData(sourceForm).entries()));
      if (draft) dirtySources.add(sourceForm);
      function changed() {
        allowed = false;
        var current = JSON.stringify(Array.from(new FormData(sourceForm).entries()));
        if (draft || current !== baseline) dirtySources.add(sourceForm);
        else dirtySources.delete(sourceForm);
      }
      sourceForm.addEventListener("input", changed);
      sourceForm.addEventListener("change", changed);
      sourceForm.addEventListener("submit", function () {
        dirtySources.delete(sourceForm);
        allowed = false;
      });
      sourceForm.addEventListener("source-discard", function () {
        sourceForm.reset();
        draft = false;
        baseline = JSON.stringify(Array.from(new FormData(sourceForm).entries()));
        dirtySources.delete(sourceForm);
        allowed = false;
      });
    });
    function confirmNavigation() {
      if (!dirtySources.size || allowed) return true;
      var first = dirtySources.values().next().value;
      allowed = window.confirm(first.dataset.unsavedPrompt);
      return allowed;
    }
    document.addEventListener("click", function (e) {
      var link = e.target.closest ? e.target.closest("a[href]") || e.target.closest("a[data-settings-tab]") : null;
      if (!link || e.ctrlKey || e.metaKey || e.shiftKey || e.altKey || e.button > 0) return;
      if (link.closest("a[data-source-close]") || link.closest("a[data-source-dialog]")) return;
      if (autosaves.some(function (state) { return state.unsettled(); })) {
        e.preventDefault();
        e.stopImmediatePropagation();
        if (!navigationPending) {
          navigationPending = true;
          guard.run(function () { navigate(link.href); }).then(function () { navigationPending = false; });
        }
        return;
      }
      if (!confirmNavigation()) {
        e.preventDefault();
        e.stopImmediatePropagation();
      }
    }, true);
    window.addEventListener("beforeunload", function (e) {
      if ((dirtySources.size && !allowed) || autosaves.some(function (state) { return state.unsettled(); })) {
        e.preventDefault();
        e.returnValue = "";
      }
    });
    var guard = {
      confirmNavigation: confirmNavigation,
      cancelNavigation: function () { allowed = false; },
      setNavigator: function (fn) { navigate = fn; },
      run: function (action, dialog) {
        var states = autosaves.filter(function (state) { return !dialog || dialog.contains(state.form); });
        function finish() {
          if (dialog ? guard.discardDialog(dialog) : confirmNavigation()) action();
        }
        if (!states.some(function (state) { return state.unsettled(); })) {
          finish();
          return Promise.resolve();
        }
        return Promise.all(states.map(function (state) { return state.flush(); })).then(function (results) {
          if (results.every(Boolean)) finish();
        });
      },
      discardDialog: function (dialog) {
        var dirty = Array.from(dirtySources).filter(function (form) { return dialog.contains(form); });
        if (!dirty.length) return true;
        if (!window.confirm(dirty[0].dataset.unsavedPrompt)) return false;
        dirty.forEach(function (form) { form.dispatchEvent(new Event("source-discard")); });
        return true;
      }
    };
    return guard;
  }

  function sourceDialogs(guard) {
    var dialogs = document.querySelectorAll("dialog.source-dialog");
    if (!dialogs.length) return;
    dialogs.forEach(function (dialog) {
      var closing = false, actionPending = false, replay = null, backdropStarted = false;
      function close() {
        if (closing || actionPending) return;
        closing = true;
        var failedDraft = dialog.querySelector('form[data-source-dirty="true"]:not([data-source-autosave="true"])');
        guard.run(function () {
          var link = dialog.querySelector("a[data-source-close]");
          if (failedDraft && link) window.location.assign(link.href);
          else dialog.close();
        }, dialog).then(function () { closing = false; });
      }
      dialog.sourceClose = close;
      if (dialog.dataset.sourceAutoOpen === "true" && dialog.showModal) {
        dialog.removeAttribute("open");
        dialog.showModal();
      }
      dialog.addEventListener("cancel", function (e) {
        e.preventDefault();
        close();
      });
      dialog.addEventListener("submit", function (e) {
        if (e.target.matches("form[data-source-edit]")) return;
        if (replay === e.target) { replay = null; return; }
        e.preventDefault();
        e.stopImmediatePropagation();
        if (actionPending || closing) return;
        actionPending = true;
        var form = e.target, submitter = e.submitter, submitted = false;
        guard.run(function () {
          replay = form;
          try {
            form.requestSubmit(submitter || undefined);
            submitted = replay === null;
          }
          finally { replay = null; }
        }, dialog).then(function () { if (!submitted) actionPending = false; });
      }, true);
      function outside(e) {
        var rect = dialog.getBoundingClientRect();
        return e.target === dialog && (e.clientX < rect.left || e.clientX > rect.right || e.clientY < rect.top || e.clientY > rect.bottom);
      }
      dialog.addEventListener("pointerdown", function (e) { backdropStarted = e.button === 0 && outside(e); });
      dialog.addEventListener("pointercancel", function () { backdropStarted = false; });
      dialog.addEventListener("click", function (e) {
        var hit = backdropStarted && outside(e);
        backdropStarted = false;
        if (hit) close();
      });
    });
    document.addEventListener("click", function (e) {
      var open = e.target.closest ? e.target.closest("a[data-source-dialog]") : null;
      if (open && !e.ctrlKey && !e.metaKey && !e.shiftKey && !e.altKey && e.button === 0) {
        var dialog = document.getElementById(open.dataset.sourceDialog);
        if (dialog && dialog.showModal) {
          e.preventDefault();
          dialog.showModal();
        }
      }
      var close = e.target.closest ? e.target.closest("a[data-source-close]") : null;
      if (close) {
        var owner = close.closest("dialog");
        if (owner && owner.close) {
          e.preventDefault();
          owner.sourceClose();
        }
      }
    });
  }

  // A tab navigation waits for the active form's final save, never aborting it.
  // Failed saves keep the form and its draft in place instead of navigating.
  function settingsNavigation() {
    var sourceGuard = sourceNavigationGuard();
    sourceDialogs(sourceGuard);
    var form = document.getElementById("settings-form");
    if (!form || !window.htmx) return;
    var dirty = false, busy = false, destination = "", field = "";
    var retry = document.getElementById("settings-retry");
    function indicator(message) {
      var el = document.getElementById("save-indicator");
      if (el) el.textContent = message;
    }
    form.addEventListener("input", function (e) {
      dirty = true;
      field = e.target.name || "";
    }, true);
    form.addEventListener("change", function (e) {
      dirty = true;
      field = e.target.name || "";
    }, true);
    form.addEventListener("htmx:configRequest", function (e) {
      if (field) e.detail.headers["HX-Trigger-Name"] = field;
    });
    form.addEventListener("htmx:beforeRequest", function () {
      busy = true;
      dirty = false;
      if (retry) retry.hidden = true;
      indicator(form.dataset.saving);
    });
    form.addEventListener("htmx:afterRequest", function (e) {
      busy = false;
      var saved = e.detail.xhr && e.detail.xhr.getResponseHeader("X-Waim-Save") === "ok";
      if (!saved) {
        dirty = true;
        destination = "";
        sourceGuard.cancelNavigation();
        if (retry) retry.hidden = false;
        if (!e.detail.xhr || !e.detail.xhr.status || e.detail.xhr.status >= 400) indicator(form.dataset.failed);
        return;
      }
      if (retry) retry.hidden = true;
      if (!destination) destination = e.detail.xhr.getResponseHeader("X-Waim-Redirect") || "";
      if (destination) {
        if (dirty) window.htmx.trigger(form, "settings-save");
        else {
          var next = destination;
          destination = "";
          sourceGuard.run(function () { window.location.assign(next); });
        }
      }
    });
    function navigate(href) {
      if (!dirty && !busy) {
        sourceGuard.run(function () { window.location.assign(href); });
        return;
      }
      destination = href;
      if (!busy) {
        if (!form.reportValidity()) { destination = ""; sourceGuard.cancelNavigation(); return; }
        window.htmx.trigger(form, "settings-save");
      }
    }
    sourceGuard.setNavigator(navigate);
    document.addEventListener("click", function (e) {
      var link = e.target.closest ? e.target.closest("a[href]") || e.target.closest("a[data-settings-tab]") : null;
      if (e.defaultPrevented || !link || e.ctrlKey || e.metaKey || e.shiftKey || e.altKey || (!dirty && !busy)) return;
      if (link.closest("a[data-source-close]") || link.closest("a[data-source-dialog]")) return;
      e.preventDefault();
      navigate(link.href);
    });
    function submit() {
      if (!busy && form.reportValidity()) window.htmx.trigger(form, "settings-save");
    }
    if (retry) retry.addEventListener("click", submit);
    form.addEventListener("submit", function (e) { e.preventDefault(); submit(); });
    window.addEventListener("beforeunload", function (e) {
      if (dirty || busy) {
        e.preventDefault();
        e.returnValue = "";
      }
    });
  }

  document.addEventListener("DOMContentLoaded", function () {
    document.addEventListener("click", function (e) {
      var link = e.target.closest ? e.target.closest("a[data-diagnostics-link]") : null;
      var panel = document.getElementById("diagnostics");
      if (link && panel) panel.open = true;
    });
    settingsNavigation();
    var box = document.getElementById("finding-search");
    if (box) {
      box.addEventListener("input", filterFindings);
    }
    var libSel = document.getElementById("finding-lib-filter");
    if (libSel) {
      libSel.addEventListener("change", filterFindings);
    }
    var seriesBox = document.getElementById("series-search");
    if (seriesBox) {
      seriesBox.addEventListener("input", filterSeries);
    }
    if (document.getElementById("tmdb-rate-estimate")) {
      updateRequestEstimates();
      document.body.addEventListener("input", updateRequestEstimates);
      document.body.addEventListener("change", updateRequestEstimates);
    }
    // Re-apply the filter right after any HTMX swap (polling, sorting, scanning)
    // so filtered-out rows never flash back in.
    document.body.addEventListener("htmx:afterSwap", filterFindings);
    // Skip swaps of unchanged polled regions (see viewTags above).
    document.body.addEventListener("htmx:configRequest", onConfigRequest);
    document.body.addEventListener("htmx:afterRequest", onAfterRequest);
    // Click-to-copy for finding names (delegated; survives HTMX swaps).
    document.body.addEventListener("click", onCopyClick);
    // Mobile navigation panel (delegated: toggle button and its links).
    document.body.addEventListener("click", onNavClick);
    document.addEventListener("keydown", onNavKeydown);
    if (desktopNav.addEventListener) {
      desktopNav.addEventListener("change", onViewportChange);
    } else if (desktopNav.addListener) {
      desktopNav.addListener(onViewportChange);
    }
    // Expandable rated lists on the statistics page (delegated).
    document.body.addEventListener("change", onRatedLimitChange);
  });
})();

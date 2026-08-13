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
      var libMatch = !lib || rows[i].getAttribute("data-library") === lib;
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

  function onRatedLimitChange(e) {
    var t = e.target;
    if (t && t.classList && t.classList.contains("rated-limit")) {
      applyRatedLimit(t);
    }
  }

  // Submit the language switcher on selection. Handled here rather than with an
  // inline onchange attribute so the page works under a strict CSP.
  function onLangChange(e) {
    var t = e.target;
    if (t && t.classList && t.classList.contains("lang-select") && t.form) {
      t.form.submit();
    }
  }

  // Fingerprint of the markup currently displayed per polled region. It is sent
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

  document.addEventListener("DOMContentLoaded", function () {
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
    // Expandable rated lists on the statistics page (delegated).
    document.body.addEventListener("change", onRatedLimitChange);
    // Language switcher (delegated).
    document.body.addEventListener("change", onLangChange);
  });
})();

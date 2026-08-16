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
    } else if (t.classList.contains("upcoming-filter")) {
      applyUpcomingFilter(t);
    }
  }

  // Filter the upcoming releases by media type, hiding groups that end up empty.
  function applyUpcomingFilter(select) {
    var want = select.value;
    var groups = document.querySelectorAll("#upcoming-groups .upcoming-group");
    for (var i = 0; i < groups.length; i++) {
      var items = groups[i].querySelectorAll(".upcoming-item");
      var visible = 0;
      for (var j = 0; j < items.length; j++) {
        var match = want === "all" || items[j].getAttribute("data-type") === want;
        setHidden(items[j], !match);
        if (match) visible++;
      }
      setHidden(groups[i], visible === 0);
    }
    var markers = document.querySelectorAll("#upcoming-timeline [data-type]");
    for (var k = 0; k < markers.length; k++) {
      setHidden(markers[k], want !== "all" && markers[k].getAttribute("data-type") !== want);
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
    // Language switcher (delegated).
    document.body.addEventListener("change", onLangChange);
  });
})();

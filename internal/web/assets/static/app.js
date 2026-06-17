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
    var kindSel = document.getElementById("finding-kind-filter");
    var kind = kindSel ? kindSel.value : "";
    var rows = document.querySelectorAll("#findings tbody tr");
    for (var i = 0; i < rows.length; i++) {
      var textMatch = rows[i].textContent.toLowerCase().indexOf(q) !== -1;
      var libMatch = !lib || rows[i].getAttribute("data-library") === lib;
      var kinds = (rows[i].getAttribute("data-kinds") || "").split(" ");
      var kindMatch = !kind || kinds.indexOf(kind) !== -1;
      rows[i].style.display = textMatch && libMatch && kindMatch ? "" : "none";
    }
  }
  window.waimFilterFindings = filterFindings;

  // Apply a gap-type filter passed via the URL (?kind=...), e.g. when arriving
  // from a "Gaps by type" card on the statistics page, and scroll it into view.
  function applyKindFromURL() {
    var params = new URLSearchParams(window.location.search);
    var kind = params.get("kind");
    if (!kind) return;
    var kindSel = document.getElementById("finding-kind-filter");
    if (kindSel) {
      kindSel.value = kind;
    }
    filterFindings();
    var findings = document.getElementById("findings");
    if (findings && findings.scrollIntoView) {
      findings.scrollIntoView({ behavior: "smooth", block: "start" });
    }
  }

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

  document.addEventListener("DOMContentLoaded", function () {
    var box = document.getElementById("finding-search");
    if (box) {
      box.addEventListener("input", filterFindings);
    }
    var libSel = document.getElementById("finding-lib-filter");
    if (libSel) {
      libSel.addEventListener("change", filterFindings);
    }
    var kindSel = document.getElementById("finding-kind-filter");
    if (kindSel) {
      kindSel.addEventListener("change", filterFindings);
    }
    // Re-apply the filter after any HTMX swap (polling, sorting, scanning).
    document.body.addEventListener("htmx:afterSettle", filterFindings);
    // Click-to-copy for finding names (delegated; survives HTMX swaps).
    document.body.addEventListener("click", onCopyClick);
    // Expandable rated lists on the statistics page (delegated).
    document.body.addEventListener("change", onRatedLimitChange);
    // Honour a ?kind= filter coming from the statistics page.
    applyKindFromURL();
  });
})();

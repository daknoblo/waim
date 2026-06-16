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

  document.addEventListener("DOMContentLoaded", function () {
    var box = document.getElementById("finding-search");
    if (box) {
      box.addEventListener("input", filterFindings);
    }
    var libSel = document.getElementById("finding-lib-filter");
    if (libSel) {
      libSel.addEventListener("change", filterFindings);
    }
    // Re-apply the filter after any HTMX swap (polling, sorting, scanning).
    document.body.addEventListener("htmx:afterSettle", filterFindings);
    // Click-to-copy for finding names (delegated; survives HTMX swaps).
    document.body.addEventListener("click", onCopyClick);
  });
})();

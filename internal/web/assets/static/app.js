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
  });
})();

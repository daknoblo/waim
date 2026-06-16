// waim front-end helpers.
(function () {
  "use strict";

  // Live-filter the findings table rows by the search box value.
  function filterFindings() {
    var box = document.getElementById("finding-search");
    var q = (box ? box.value : "").toLowerCase().trim();
    var rows = document.querySelectorAll("#findings tbody tr");
    for (var i = 0; i < rows.length; i++) {
      var match = rows[i].textContent.toLowerCase().indexOf(q) !== -1;
      rows[i].style.display = match ? "" : "none";
    }
  }
  window.waimFilterFindings = filterFindings;

  document.addEventListener("DOMContentLoaded", function () {
    var box = document.getElementById("finding-search");
    if (box) {
      box.addEventListener("input", filterFindings);
    }
    // Re-apply the filter after any HTMX swap (polling, sorting, scanning).
    document.body.addEventListener("htmx:afterSettle", filterFindings);
  });
})();

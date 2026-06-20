// Theme: persist dark/light in localStorage.
(function () {
  const saved = localStorage.getItem("gramli-theme");
  if (saved) document.documentElement.setAttribute("data-theme", saved);
})();

function toggleTheme() {
  const el = document.documentElement;
  const next = el.getAttribute("data-theme") === "light" ? "dark" : "light";
  el.setAttribute("data-theme", next);
  localStorage.setItem("gramli-theme", next);
}

function closeModal(ev) {
  // Close only when clicking the backdrop or a close control, not inner content.
  if (ev && ev.target.closest(".modal") && !ev.target.closest("[data-close]")) return;
  const m = document.getElementById("modal");
  if (m) m.innerHTML = "";
}

// Keyboard shortcuts: "/" focuses search, Esc closes the modal.
document.addEventListener("keydown", function (e) {
  if (e.key === "Escape") closeModal();
  if (e.key === "/" && !/input|textarea|select/i.test(document.activeElement.tagName)) {
    const s = document.getElementById("search");
    if (s) { e.preventDefault(); s.focus(); }
  }
});

// Broken thumbnails fall back to the placeholder endpoint.
document.addEventListener("error", function (e) {
  const img = e.target;
  if (img.tagName === "IMG" && img.dataset.fallback !== "done") {
    img.dataset.fallback = "done";
    img.src = "/thumb/0";
  }
}, true);

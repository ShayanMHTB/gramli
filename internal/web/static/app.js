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

// Carousel: switch the active slide within the open post modal.
function modalSlide(delta) {
  const modal = document.querySelector("[data-post-modal]");
  if (!modal) return;
  const slides = Array.from(modal.querySelectorAll(".slide"));
  if (slides.length < 2) return;
  let cur = slides.findIndex((s) => s.classList.contains("active"));
  if (cur < 0) cur = 0;
  const v = slides[cur].querySelector("video");
  if (v) v.pause();
  slides[cur].classList.remove("active");
  cur = (cur + delta + slides.length) % slides.length;
  slides[cur].classList.add("active");
  const counter = modal.querySelector("[data-current]");
  if (counter) counter.textContent = cur + 1;
}

function copyText(text, btn) {
  const done = () => {
    if (!btn) return;
    const original = btn.textContent;
    btn.textContent = "Copied";
    setTimeout(() => (btn.textContent = original), 1200);
  };
  if (navigator.clipboard) navigator.clipboard.writeText(text).then(done, done);
}

function modalOpen() {
  return !!document.querySelector("[data-post-modal]");
}

// siblingPost opens the next/previous post from the gallery grid into the modal,
// so j/k browses the archive without leaving the lightbox.
function siblingPost(delta) {
  const modal = document.querySelector("[data-post-modal]");
  if (!modal || !window.htmx) return;
  const sc = modal.getAttribute("data-shortcode");
  const tiles = Array.from(document.querySelectorAll('.tile[hx-get^="/post/"]'));
  const idx = tiles.findIndex((t) => t.getAttribute("hx-get") === "/post/" + sc);
  if (idx < 0) return;
  const next = tiles[idx + delta];
  if (!next) return;
  window.htmx.ajax("GET", next.getAttribute("hx-get"), { target: "#modal", swap: "innerHTML" });
}

// Keyboard shortcuts: "/" focuses search, Esc closes the modal, arrows page
// through an open post's media.
document.addEventListener("keydown", function (e) {
  const typing = /input|textarea|select/i.test(document.activeElement.tagName);
  if (e.key === "Escape") closeModal();
  if (e.key === "/" && !typing) {
    const s = document.getElementById("search");
    if (s) { e.preventDefault(); s.focus(); }
  }
  if (modalOpen() && !typing) {
    if (e.key === "ArrowRight") { e.preventDefault(); modalSlide(1); }
    if (e.key === "ArrowLeft") { e.preventDefault(); modalSlide(-1); }
    if (e.key === "j") { e.preventDefault(); siblingPost(1); }
    if (e.key === "k") { e.preventDefault(); siblingPost(-1); }
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

(function () {
  var isHome = location.pathname === "/" || location.pathname === "/index.html";
  var p = isHome ? "#" : "/#";

  // --- Navigation ---
  document.getElementById("site-nav").innerHTML =
    '<div class="container nav-inner">' +
      '<a href="/" class="logo">⚡ Speedy e-Boekhouden</a>' +
      '<div class="nav-links">' +
        '<a href="' + p + 'functies">Functies</a>' +
        '<a href="' + p + 'hoe-het-werkt">Hoe het werkt</a>' +
        '<a href="' + p + 'faq">FAQ</a>' +
        '<a href="/beveiliging">Beveiliging</a>' +
        '<a href="/disclaimer">Disclaimer</a>' +
        '<a href="/app/" class="btn btn-primary btn-sm" data-analytics="nav">Inloggen</a>' +
      "</div>" +
      '<button class="mobile-menu-btn" aria-label="Menu">' +
        "<span></span><span></span><span></span>" +
      "</button>" +
    "</div>";

  document.getElementById("site-nav").querySelector(".mobile-menu-btn")
    .addEventListener("click", function () {
      document.querySelector(".nav-links").classList.toggle("open");
    });

  // --- Footer ---
  document.getElementById("site-footer").innerHTML =
    '<div class="container">' +
      '<div class="footer-content">' +
        '<div class="footer-brand">' +
          '<span class="logo">⚡ Speedy e-Boekhouden</span>' +
          "<p>Supercharge je e-boekhouden.nl administratie met AI.</p>" +
        "</div>" +
        '<div class="footer-links">' +
          '<p class="footer-heading">Product</p>' +
          '<a href="' + p + 'functies">Functies</a>' +
          '<a href="' + p + 'hoe-het-werkt">Hoe het werkt</a>' +
          '<a href="' + p + 'faq">FAQ</a>' +
          '<a href="/app/">Inloggen</a>' +
        "</div>" +
        '<div class="footer-links">' +
          '<p class="footer-heading">Juridisch</p>' +
          '<a href="/beveiliging">Beveiliging</a>' +
          '<a href="/disclaimer">Disclaimer</a>' +
          '<a href="mailto:info@speedy-eboekhouden.nl">Contact</a>' +
        "</div>" +
      "</div>" +
      '<div class="footer-legal">' +
        "<hr>" +
        '<div class="legal-text">' +
          "<p>Speedy e-Boekhouden is niet gelieerd aan e-Boekhouden B.V. " +
          'Gebruik op eigen risico. Zie onze <a href="/disclaimer" style="color:#94a3b8;text-decoration:underline">volledige disclaimer</a>.</p>' +
        "</div>" +
        '<p class="copyright">&copy; 2026 Speedy e-Boekhouden. Alle rechten voorbehouden.</p>' +
      "</div>" +
    "</div>";

  // --- Analytics events ---
  var track = window.plausible || function () {};

  // CTA button clicks
  document.querySelectorAll("a.btn-primary, a.btn-outline").forEach(function (el) {
    el.addEventListener("click", function () {
      var section = el.closest("section, header, nav");
      var location = section
        ? section.id || section.className.split(" ")[0] || "unknown"
        : "nav";
      track("CTA Click", { props: { location: location, text: el.textContent.trim() } });
    });
  });

  // FAQ toggle tracking
  document.querySelectorAll(".faq-item summary").forEach(function (el) {
    el.addEventListener("click", function () {
      track("FAQ Toggle", { props: { question: el.textContent.trim().slice(0, 80) } });
    });
  });

  // Scroll depth (fire once per threshold)
  var depthFired = {};
  window.addEventListener("scroll", function () {
    var h = document.documentElement.scrollHeight - window.innerHeight;
    if (h <= 0) return;
    var pct = Math.round((window.scrollY / h) * 100);
    [25, 50, 75, 100].forEach(function (t) {
      if (pct >= t && !depthFired[t]) {
        depthFired[t] = true;
        track("Scroll Depth", { props: { depth: t + "%" } });
      }
    });
  });

  // Page engagement (30s on page)
  setTimeout(function () {
    track("Page Engagement", { props: { page: location.pathname } });
  }, 30000);
})();

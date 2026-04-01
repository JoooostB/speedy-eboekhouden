(function () {
  var isHome = location.pathname === "/" || location.pathname === "/index.html";
  var p = isHome ? "#" : "/#";

  document.getElementById("site-nav").innerHTML =
    '<div class="container nav-inner">' +
      '<a href="/" class="logo">⚡ Speedy e-Boekhouden</a>' +
      '<div class="nav-links">' +
        '<a href="' + p + 'features">Functies</a>' +
        '<a href="' + p + 'hoe-het-werkt">Hoe het werkt</a>' +
        '<a href="' + p + 'faq">FAQ</a>' +
        '<a href="/beveiliging">Beveiliging</a>' +
        '<a href="/disclaimer">Disclaimer</a>' +
        '<a href="/app/" class="btn btn-primary btn-sm">Inloggen</a>' +
      "</div>" +
      '<button class="mobile-menu-btn" aria-label="Menu">' +
        "<span></span><span></span><span></span>" +
      "</button>" +
    "</div>";

  document.getElementById("site-nav").querySelector(".mobile-menu-btn")
    .addEventListener("click", function () {
      document.querySelector(".nav-links").classList.toggle("open");
    });

  document.getElementById("site-footer").innerHTML =
    '<div class="container">' +
      '<div class="footer-content">' +
        '<div class="footer-brand">' +
          '<span class="logo">⚡ Speedy e-Boekhouden</span>' +
          "<p>De snelste manier om uren in te voeren in e-boekhouden.nl.</p>" +
        "</div>" +
        '<div class="footer-links">' +
          "<h4>Product</h4>" +
          '<a href="' + p + 'features">Functies</a>' +
          '<a href="' + p + 'hoe-het-werkt">Hoe het werkt</a>' +
          '<a href="' + p + 'faq">FAQ</a>' +
          '<a href="/app/">Inloggen</a>' +
        "</div>" +
        '<div class="footer-links">' +
          "<h4>Juridisch</h4>" +
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
})();

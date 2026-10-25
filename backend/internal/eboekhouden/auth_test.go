package eboekhouden

import (
	"fmt"
	"testing"
)

// The pages below are made up: there is no recorded sample of either the
// wrong-password page or the new-IP page. They model what any login page
// carries (scripts, a meta description, an e-mail field) to show that only
// a visible mention of an IP address counts as the new-IP block.
func TestLoginBlockedForNewIP(t *testing.T) {
	const loginShell = `<!DOCTYPE html>
<html lang="nl">
<head>
<meta name="DESCRIPTION" content="Inloggen bij je administratie">
<SCRIPT type="text/javascript">var clientIP = "192.0.2.1"; function tip() {}</SCRIPT>
<style>.IP { color: red }</style>
</head>
<body>
<!-- IP check -->
<form method="post" action="inloggen.asp?LOGIN=1">
<label for="txtEmail">E-mailadres</label><input id="txtEmail" name="txtEmail" data-ip="1">
<label for="txtWachtwoord">Wachtwoord</label><input type="password" name="txtWachtwoord">
<a href="wachtwoord.asp">Wachtwoord vergeten? Vul je e-mailadres in, dan sturen we je een e-mail. Bevestig daarna je nieuwe wachtwoord.</a>
%s
</form>
</body>
</html>`

	tests := []struct {
		name string
		body string
		want bool
	}{
		{"plain login page", fmt.Sprintf(loginShell, ""), false},
		{"wrong password", fmt.Sprintf(loginShell, `<div class="error">De combinatie van e-mailadres en wachtwoord is onjuist.</div>`), false},
		{"empty body", "", false},
		{"new IP, capitals", fmt.Sprintf(loginShell, `<div class="error">Je logt in vanaf een onbekend IP-adres. Bevestig het adres via de link in de e-mail die we je sturen.</div>`), true},
		{"new IP, Groene Boekje spelling", `<p>Dit ip-adres is nog niet goedgekeurd.</p>`, true},
		{"new IP, entity between words", `<p>Onbekend&nbsp;IP&#45;adres</p>`, true},
		{"IP only in a script", `<script>if (ip) { block(); }</script><p>Wachtwoord onjuist.</p>`, false},
		{"IP only in a comment", `<!-- IP --><p>Wachtwoord onjuist.</p>`, false},
		{"IP only in an attribute", `<input name="IP" value="IP"><p>Wachtwoord onjuist.</p>`, false},
		{"IP inside other words", `<p>TIP: SHIP the VIP chip.</p>`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := loginBlockedForNewIP(tc.body); got != tc.want {
				t.Errorf("loginBlockedForNewIP() = %v, want %v", got, tc.want)
			}
		})
	}
}

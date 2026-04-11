package mail

import "fmt"

func welcomeHTML(name, appURL string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="nl">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Welkom bij Speedy e-Boekhouden</title>
</head>
<body style="margin:0;padding:0;background-color:#f8fafc;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,'Helvetica Neue',Arial,sans-serif;color:#1a1a2e;">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background-color:#f8fafc;">
<tr><td align="center" style="padding:40px 16px;">
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="max-width:560px;background-color:#ffffff;border-radius:12px;overflow:hidden;">

<!-- Header -->
<tr>
<td style="background-color:#1565c0;padding:32px 40px;text-align:center;">
  <h1 style="margin:0;color:#ffffff;font-size:24px;font-weight:700;letter-spacing:-0.02em;">
    Speedy e-Boekhouden
  </h1>
</td>
</tr>

<!-- Body -->
<tr>
<td style="padding:40px;">
  <h2 style="margin:0 0 8px;font-size:22px;font-weight:700;color:#1a1a2e;">
    Welkom, %s!
  </h2>
  <p style="margin:0 0 24px;font-size:16px;line-height:1.6;color:#546e7a;">
    Je account is aangemaakt. Tijd om je e-boekhouden.nl administratie te superchargen.
  </p>

  <!-- Features -->
  <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="margin-bottom:32px;">
    <tr>
      <td style="padding:16px;background-color:#f8fafc;border-radius:8px;margin-bottom:8px;">
        <table role="presentation" cellpadding="0" cellspacing="0"><tr>
          <td style="vertical-align:top;padding-right:12px;font-size:20px;">&#9200;</td>
          <td>
            <strong style="color:#1a1a2e;">Urenregistratie</strong><br>
            <span style="color:#546e7a;font-size:14px;">Boek uren voor je hele team in 30 seconden.</span>
          </td>
        </tr></table>
      </td>
    </tr>
    <tr><td style="height:8px;"></td></tr>
    <tr>
      <td style="padding:16px;background-color:#f8fafc;border-radius:8px;">
        <table role="presentation" cellpadding="0" cellspacing="0"><tr>
          <td style="vertical-align:top;padding-right:12px;font-size:20px;">&#128182;</td>
          <td>
            <strong style="color:#1a1a2e;">Afschriften verwerken</strong><br>
            <span style="color:#546e7a;font-size:14px;">AI stelt boekingen voor bij je bankafschriften.</span>
          </td>
        </tr></table>
      </td>
    </tr>
    <tr><td style="height:8px;"></td></tr>
    <tr>
      <td style="padding:16px;background-color:#f8fafc;border-radius:8px;">
        <table role="presentation" cellpadding="0" cellspacing="0"><tr>
          <td style="vertical-align:top;padding-right:12px;font-size:20px;">&#128196;</td>
          <td>
            <strong style="color:#1a1a2e;">Factuurverwerking</strong><br>
            <span style="color:#546e7a;font-size:14px;">Upload een PDF en AI leest alle gegevens uit.</span>
          </td>
        </tr></table>
      </td>
    </tr>
  </table>

  <!-- CTA -->
  <table role="presentation" width="100%%" cellpadding="0" cellspacing="0">
    <tr>
      <td align="center">
        <a href="%s" style="display:inline-block;padding:14px 32px;background-color:#1565c0;color:#ffffff;text-decoration:none;border-radius:8px;font-size:16px;font-weight:600;">
          Aan de slag
        </a>
      </td>
    </tr>
  </table>

  <!-- Next steps -->
  <p style="margin:32px 0 0;font-size:14px;line-height:1.6;color:#546e7a;">
    <strong style="color:#1a1a2e;">Volgende stappen:</strong><br>
    1. Verbind je e-boekhouden.nl account via het dashboard<br>
    2. Stel optioneel een Anthropic API-sleutel in voor AI-functies
  </p>
</td>
</tr>

<!-- Footer -->
<tr>
<td style="padding:24px 40px;border-top:1px solid #e2e8f0;text-align:center;">
  <p style="margin:0;font-size:13px;color:#94a3b8;line-height:1.6;">
    Je ontvangt deze e-mail omdat je een account hebt aangemaakt bij
    <a href="%s" style="color:#1565c0;text-decoration:none;">Speedy e-Boekhouden</a>.
  </p>
</td>
</tr>

</table>
</td></tr>
</table>
</body>
</html>`, name, appURL, appURL)
}

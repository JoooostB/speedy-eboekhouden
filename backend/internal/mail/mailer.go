package mail

import (
	"crypto/tls"
	"errors"
	"fmt"
	"html"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

// Config holds SMTP connection settings.
type Config struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
}

// Bounds of one delivery. smtp.SendMail has none, so a server that accepts
// the connection and then stalls would hold the sender forever; mails are
// sent in the background, so stalled sends would pile up.
const (
	defaultDialTimeout = 10 * time.Second
	defaultSendTimeout = 30 * time.Second
)

// Mailer sends transactional emails via SMTP.
type Mailer struct {
	config Config
	// dialTimeout bounds connecting, sendTimeout the whole SMTP
	// conversation (TLS handshake included). Tests shorten them.
	dialTimeout time.Duration
	sendTimeout time.Duration
}

// New creates a new mailer. Returns nil if SMTP is not configured.
func New(cfg Config) *Mailer {
	if cfg.Host == "" {
		return nil
	}
	return &Mailer{config: cfg, dialTimeout: defaultDialTimeout, sendTimeout: defaultSendTimeout}
}

// sanitizeHeader strips CR/LF to prevent email header injection.
func sanitizeHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}

// SendRecoveryEmail sends a passkey recovery link.
func (m *Mailer) SendRecoveryEmail(to, name, recoveryURL string) error {
	subject := "Nieuwe passkey instellen voor Speedy e-Boekhouden"
	plain := fmt.Sprintf("Hoi %s,\r\n\r\n"+
		"Er is een link aangevraagd om een nieuwe passkey in te stellen voor je account bij Speedy e-Boekhouden. "+
		"Open de link op het apparaat waarmee je wilt inloggen:\r\n\r\n"+
		"%s\r\n\r\n"+
		"De link is 15 minuten geldig en werkt één keer. Lukt het niet? Vraag dan een nieuwe aan via \"Passkey kwijt?\" op de inlogpagina.\r\n\r\n"+
		"Heb je dit niet zelf aangevraagd? Dan kun je deze e-mail negeren. Zonder de link verandert er niets aan je account.\r\n\r\n"+
		"Met vriendelijke groet,\r\nSpeedy e-Boekhouden",
		sanitizeHeader(name), sanitizeHeader(recoveryURL))
	return m.sendPlain(sanitizeHeader(to), subject, plain)
}

// SendWelcomeEmail sends a branded welcome email after registration.
func (m *Mailer) SendWelcomeEmail(to, name, appURL string) error {
	subject := "Welkom bij Speedy e-Boekhouden"
	// HTML-escape user-supplied values to prevent HTML injection
	safeName := html.EscapeString(name)
	safeURL := html.EscapeString(appURL)
	htmlBody := welcomeHTML(safeName, safeURL)
	plain := fmt.Sprintf("Hoi %s,\r\n\r\n"+
		"Welkom bij Speedy e-Boekhouden. Je account is aangemaakt.\r\n\r\n"+
		"Speedy werkt rechtstreeks in je eigen administratie in e-Boekhouden. Alles wat je via Speedy boekt, staat daar gewoon.\r\n\r\n"+
		"Met Speedy kun je onder meer:\r\n"+
		"- de uren van je hele team in één keer boeken;\r\n"+
		"- je bankafschriften verwerken, met per regel een boekingsvoorstel van AI;\r\n"+
		"- facturen en bonnetjes laten uitlezen en boeken, ook als je ze naar het mailadres van je administratie stuurt.\r\n\r\n"+
		"Zo begin je:\r\n"+
		"1. Klik in Speedy op \"e-Boekhouden koppelen\" en log in met je e-Boekhouden-gegevens.\r\n"+
		"2. Wil je AI gebruiken? Vul dan bij Instellingen je eigen Anthropic API-sleutel in. De kosten lopen via je eigen Anthropic-account.\r\n\r\n"+
		"Naar Speedy: %s\r\n\r\n"+
		"Met vriendelijke groet,\r\nSpeedy e-Boekhouden",
		sanitizeHeader(name), sanitizeHeader(appURL))
	return m.sendHTML(sanitizeHeader(to), subject, htmlBody, plain)
}

func (m *Mailer) sendPlain(to, subject, body string) error {
	msg := "From: Speedy e-Boekhouden <" + sanitizeHeader(m.config.From) + ">\r\n" +
		"To: " + sanitizeHeader(to) + "\r\n" +
		"Subject: " + sanitizeHeader(subject) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" + body

	return m.deliver(to, []byte(msg))
}

func (m *Mailer) sendHTML(to, subject, htmlBody, plain string) error {
	boundary := "----=_SpeedyBoundary"
	msg := "From: Speedy e-Boekhouden <" + sanitizeHeader(m.config.From) + ">\r\n" +
		"To: " + sanitizeHeader(to) + "\r\n" +
		"Subject: " + sanitizeHeader(subject) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n" +
		"\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" + plain + "\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n" +
		"\r\n" + htmlBody + "\r\n" +
		"--" + boundary + "--\r\n"

	return m.deliver(to, []byte(msg))
}

// deliver does what smtp.SendMail does (STARTTLS when offered, PLAIN auth,
// one recipient), on a connection with a deadline. Without a username it
// sends without AUTH, for a relay that needs none.
func (m *Mailer) deliver(to string, msg []byte) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(m.config.Host, m.config.Port), m.dialTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(m.sendTimeout)); err != nil {
		return err
	}

	c, err := smtp.NewClient(conn, m.config.Host)
	if err != nil {
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: m.config.Host}); err != nil {
			return err
		}
	}
	if ok, _ := c.Extension("AUTH"); ok {
		if err := c.Auth(smtp.PlainAuth("", m.config.Username, m.config.Password, m.config.Host)); err != nil {
			return err
		}
	} else if m.config.Username != "" {
		return errors.New("smtp: server doesn't support AUTH")
	}
	if err := c.Mail(m.config.From); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// ErrClass renders a send error for a log line without the recipient. SMTP
// replies often echo the address ("550 5.1.1 <iemand@voorbeeld.nl>: user
// unknown"), so only the reply code survives; network errors become their
// kind, and anything else (TLS, AUTH support) only its Go type.
func ErrClass(err error) string {
	if err == nil {
		return "<nil>"
	}
	var reply *textproto.Error
	if errors.As(err, &reply) {
		return fmt.Sprintf("smtp_reply_%d", reply.Code)
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return "smtp_timeout"
		}
		return "smtp_network"
	}
	return fmt.Sprintf("smtp_error(%T)", err)
}

package mail

import (
	"crypto/tls"

	gomail "gopkg.in/mail.v2"
)

type Mail struct {
	dialer *gomail.Dialer
}

func New(host string, port int, username, password string, skipTLS bool) *Mail {
	d := gomail.NewDialer(host, port, username, password)
	if skipTLS {
		d.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Mail{
		dialer: d,
	}
}

func (m *Mail) Send(fromFriendly, fromEmail, to, subject, bodyHTML, bodyTXT string) error {
	msg := gomail.NewMessage()
	msg.SetAddressHeader("From", fromEmail, fromFriendly)
	msg.SetHeader("To", to)
	msg.SetHeader("Subject", subject)
	msg.SetBody("text/plain", bodyTXT)
	msg.AddAlternative("text/html", bodyHTML)

	if err := m.dialer.DialAndSend(msg); err != nil {
		return err
	}
	return nil
}

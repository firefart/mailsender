package mail

import (
	"crypto/tls"
	"time"

	gomail "gopkg.in/mail.v2"
)

type Mail struct {
	dialer *gomail.Dialer
}

func New(host string, port int, username, password string, useTLS bool, skipCertificateCheck bool, timeout time.Duration) *Mail {
	d := gomail.NewDialer(host, port, username, password)
	if skipCertificateCheck {
		d.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}
	d.SSL = useTLS
	d.Timeout = timeout
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

package mail

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/firefart/mailsender/internal/config"

	gomail "github.com/wneessen/go-mail"
)

type Mail struct {
	dryRun bool
	config config.SystemConfiguration
}

func New(c config.SystemConfiguration, dryRun bool) *Mail {
	return &Mail{
		dryRun: dryRun,
		config: c,
	}
}

// gomail.Client is NOT threadsafe
// https://github.com/wneessen/go-mail/discussions/268
// so we need to create a new client each time :/
func (m *Mail) newClient() (*gomail.Client, error) {
	var options []gomail.Option

	options = append(options, gomail.WithTimeout(m.config.Timeout.Duration))
	options = append(options, gomail.WithPort(m.config.Port))
	if m.config.User != "" && m.config.Password != "" {
		options = append(options, gomail.WithSMTPAuth(gomail.SMTPAuthPlain))
		options = append(options, gomail.WithUsername(m.config.User))
		options = append(options, gomail.WithPassword(m.config.Password))
	}
	if m.config.SkipCertificateCheck {
		options = append(options, gomail.WithTLSConfig(&tls.Config{
			InsecureSkipVerify: true,
		}))
	}

	// use either tls, starttls, or starttls with fallback to plaintext
	if m.config.TLS {
		options = append(options, gomail.WithSSL())
	} else if m.config.StartTLS {
		options = append(options, gomail.WithTLSPortPolicy(gomail.TLSMandatory))
	} else {
		options = append(options, gomail.WithTLSPortPolicy(gomail.TLSOpportunistic))
	}

	mailer, err := gomail.NewClient(m.config.Server, options...)
	if err != nil {
		return nil, fmt.Errorf("could not create mail client: %w", err)
	}

	return mailer, nil
}

func (m *Mail) Send(ctx context.Context, fromFriendly, fromEmail, to, subject, bodyHTML, bodyTXT string) error {
	if m.dryRun {
		// do nothing in dry-run mode
		return nil
	}

	mailer, err := m.newClient()
	if err != nil {
		return err
	}

	msg := gomail.NewMsg(gomail.WithNoDefaultUserAgent())
	if err := msg.FromFormat(fromFriendly, fromEmail); err != nil {
		return err
	}
	if err := msg.To(to); err != nil {
		return err
	}
	msg.Subject(subject)
	msg.SetBodyString(gomail.TypeTextPlain, bodyTXT)
	msg.AddAlternativeString(gomail.TypeTextHTML, bodyHTML)

	if err := mailer.DialAndSendWithContext(ctx, msg); err != nil {
		return err
	}
	return nil
}

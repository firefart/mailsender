package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"text/template"
	"time"

	"github.com/firefart/mailsender/internal/config"
	"github.com/firefart/mailsender/internal/mail"
	"go.etcd.io/bbolt"

	"github.com/asdine/storm/v3"
	"github.com/asdine/storm/v3/q"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

type importOptions struct {
	dbname      string
	csvFilePath string
}

type dumpOptions struct {
	dbname string
}

func (o dumpOptions) validate() error {
	if o.dbname == "" {
		return fmt.Errorf("please set a database name")
	}

	return nil
}

type dbValue struct {
	ID        int `storm:"id,increment"`
	Email     string
	Name      string
	GivenName string
	Sent      bool
	SentTime  time.Time
}

func (o importOptions) validate() error {
	if o.dbname == "" {
		return fmt.Errorf("please set a database name")
	}

	if o.csvFilePath == "" {
		return fmt.Errorf("please set a csv file name")
	}

	return nil
}

type sendOptions struct {
	dbname         string
	numberOfEmails int
	delay          time.Duration
	stopOnError    bool
	config         config.SystemConfiguration
	configEmail    config.MailConfiguration
	dryRun         bool
}

func (o sendOptions) validate() error {
	if o.dbname == "" {
		return fmt.Errorf("please set a database name")
	}

	if o.configEmail.HTMLTemplate == "" {
		return fmt.Errorf("please set a html template path")
	}

	if o.configEmail.TXTTemplate == "" {
		return fmt.Errorf("please set a txt template path")
	}

	if o.configEmail.Subject == "" {
		return fmt.Errorf("please set a subject")
	}

	if o.config.From.Name == "" {
		return fmt.Errorf("please set a friendly from name")
	}

	if o.config.From.Mail == "" {
		return fmt.Errorf("please set a from email")
	}

	if o.config.Server == "" {
		return fmt.Errorf("please set a mail host")
	}

	if o.config.Port == 0 {
		return fmt.Errorf("please set a mail port")
	}
	return nil
}

type templateData struct {
	Name      string
	GivenName string
	Email     string
}

func main() {
	log := logrus.New()
	log.SetFormatter(&logrus.TextFormatter{
		DisableColors: false,
		FullTimestamp: true,
	})
	app := &cli.App{
		Name:  "mailsender",
		Usage: "sends a bunch of emails and tracks the status",
		Commands: []*cli.Command{
			{
				Name:  "import",
				Usage: "import list of emails to the database",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "debug", Aliases: []string{"d"}, Value: false, Usage: "enable debug output"},
					&cli.StringFlag{Name: "dbname", Usage: "local database name", Value: "emails.db"},
					&cli.StringFlag{Name: "csv", Required: true, Usage: "csv file to import"},
				},
				Before: func(ctx *cli.Context) error {
					if ctx.Bool("debug") {
						log.SetLevel(logrus.DebugLevel)
					}
					return nil
				},
				Action: func(cCtx *cli.Context) error {
					opts := importOptions{
						dbname:      cCtx.String("dbname"),
						csvFilePath: cCtx.String("csv"),
					}

					if err := opts.validate(); err != nil {
						return err
					}
					return importEmails(log, opts)
				},
			},
			{
				Name:  "dump",
				Usage: "dump database in human readable format",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "debug", Aliases: []string{"d"}, Value: false, Usage: "enable debug output"},
					&cli.StringFlag{Name: "dbname", Usage: "local database name", Value: "emails.db"},
				},
				Before: func(ctx *cli.Context) error {
					if ctx.Bool("debug") {
						log.SetLevel(logrus.DebugLevel)
					}
					return nil
				},
				Action: func(cCtx *cli.Context) error {
					opts := dumpOptions{
						dbname: cCtx.String("dbname"),
					}

					if err := opts.validate(); err != nil {
						return err
					}
					return dumpDatabase(opts)
				},
			},
			{
				Name:  "send",
				Usage: "send emails",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "debug", Aliases: []string{"d"}, Value: false, Usage: "enable debug output"},
					&cli.BoolFlag{Name: "dry-run", Value: false, Usage: "dry-run disables email sending for debugging"},
					&cli.StringFlag{Name: "dbname", Usage: "local database name", Value: "emails.db"},
					&cli.StringFlag{Name: "systemconfig", Aliases: []string{"c"}, Usage: "System config file to use", Required: true},
					&cli.StringFlag{Name: "mailconfig", Aliases: []string{"mc"}, Usage: "Mail config file to use", Required: true},
					&cli.IntFlag{Name: "count", Usage: "number of emails to send in this run", Value: 1000},
					&cli.DurationFlag{Name: "delay", Usage: "time to sleep between email batches", Value: 1 * time.Minute},
					&cli.BoolFlag{Name: "stop-on-error", Usage: "Flag to stop on error instead of sending the next email", Value: false},
				},
				Before: func(ctx *cli.Context) error {
					if ctx.Bool("debug") {
						log.SetLevel(logrus.DebugLevel)
					}
					return nil
				},
				Action: func(cCtx *cli.Context) error {
					systemConfiguration, err := config.GetSystemConfig(cCtx.String("systemconfig"))
					if err != nil {
						return err
					}
					mailConfiguration, err := config.GetMailConfig(cCtx.String("mailconfig"))
					if err != nil {
						return err
					}

					opts := sendOptions{
						dbname:         cCtx.String("dbname"),
						numberOfEmails: cCtx.Int("count"),
						delay:          cCtx.Duration("delay"),
						stopOnError:    cCtx.Bool("stop-on-error"),
						dryRun:         cCtx.Bool("dry-run"),
						config:         systemConfiguration,
						configEmail:    mailConfiguration,
					}

					if err := opts.validate(); err != nil {
						return err
					}

					ctx, cancel := signal.NotifyContext(cCtx.Context, os.Interrupt)
					defer cancel()

					return sendEmails(ctx, log, opts)
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func getUnsentEmailCount(db *storm.DB) (int, error) {
	count := 0
	count, err := db.Select(q.Eq("Sent", false)).Count(new(dbValue))
	if err != nil {
		return -1, fmt.Errorf("getUnsentEmailCount: %w", err)
	}
	return count, nil
}

func dumpDatabase(opts dumpOptions) error {
	db, err := storm.Open(opts.dbname, storm.BoltOptions(0600, &bbolt.Options{Timeout: 1 * time.Second}))
	if err != nil {
		return fmt.Errorf("could not open database: %w", err)
	}
	defer db.Close()
	var candidates []dbValue
	if err := db.All(&candidates); err != nil {
		return err
	}
	for _, candidate := range candidates {
		fmt.Printf("#####################\n")
		fmt.Printf("Email: %s\n", candidate.Email)
		fmt.Printf("Name: %s\n", candidate.Name)
		fmt.Printf("Given Name: %s\n", candidate.GivenName)
		fmt.Printf("Sent: %t\n", candidate.Sent)
		if candidate.Sent {
			fmt.Printf("Sent: %s\n", candidate.SentTime)
		}
	}
	return nil
}

func importEmails(log *logrus.Logger, opts importOptions) error {
	if err := os.Remove(opts.dbname); err != nil {
		// ignore if no previous database
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	db, err := storm.Open(opts.dbname, storm.BoltOptions(0600, &bbolt.Options{Timeout: 1 * time.Second}), storm.Batch())
	if err != nil {
		return fmt.Errorf("could not open database: %w", err)
	}
	defer db.Close()

	csvFile, err := os.Open(opts.csvFilePath)
	if err != nil {
		return fmt.Errorf("error opening csv file: %w", err)
	}
	defer csvFile.Close()

	csvReader := csv.NewReader(csvFile)
	csvReader.FieldsPerRecord = 3
	csvReader.TrimLeadingSpace = true
	count := -1
	for {
		records, err := csvReader.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("error reading csv: %w", err)
		}

		count++
		// no need to process header line
		if count == 0 {
			continue
		}

		email := records[2]
		name := records[0]
		givenName := records[1]

		value := dbValue{
			Email:     email,
			Name:      name,
			GivenName: givenName,
			Sent:      false,
		}
		if err := db.Save(&value); err != nil {
			return fmt.Errorf("error on inserting %s into database: %w", email, err)
		}
	}
	log.Infof("inserted %d records", count)
	return nil
}

func sendEmails(ctx context.Context, log *logrus.Logger, opts sendOptions) error {
	mail := mail.New(opts.config, opts.dryRun)

	templateHTML, err := template.ParseFiles(opts.configEmail.HTMLTemplate)
	if err != nil {
		return fmt.Errorf("could not parse html template %s: %w", opts.configEmail.HTMLTemplate, err)
	}
	templateTXT, err := template.ParseFiles(opts.configEmail.TXTTemplate)
	if err != nil {
		return fmt.Errorf("could not parse txt template %s: %w", opts.configEmail.TXTTemplate, err)
	}

	db, err := storm.Open(opts.dbname, storm.BoltOptions(0600, &bbolt.Options{Timeout: 1 * time.Second}))
	if err != nil {
		return fmt.Errorf("could not open database %s: %w", opts.dbname, err)
	}
	defer db.Close()

	totalSent := 0
	for {
		log.Infof("starting next email batch")
		emailsSent, err := sendEmailsWorker(ctx, log, opts, templateHTML, templateTXT, mail, db)
		if err != nil {
			return err
		}
		totalSent += emailsSent
		log.Infof("Sent %d emails (%d total emails sent)", emailsSent, totalSent)

		remainder, err := getUnsentEmailCount(db)
		if err != nil {
			return err
		}

		if remainder == 0 {
			log.Infof("sent all %d emails", totalSent)
			break
		}

		log.Infof("sleeping for %s. %d mails to go", opts.delay, remainder)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(opts.delay):
		}
	}

	return nil
}

func sendEmailsWorker(ctx context.Context, log *logrus.Logger, opts sendOptions, htmlTemplate, txtTemplate *template.Template, mail *mail.Mail, db *storm.DB) (int, error) {
	count := 0

	var candidates []dbValue
	if err := db.Find("Sent", false, &candidates, storm.Limit(opts.numberOfEmails)); err != nil {
		return -1, fmt.Errorf("sendEmailsWorker: db.Find error: %w", err)
	}
	for _, candidate := range candidates {
		data := templateData{
			Name:      candidate.Name,
			GivenName: candidate.GivenName,
			Email:     candidate.Email,
		}
		var tplHTML, tplTXT bytes.Buffer
		if err := htmlTemplate.Execute(&tplHTML, data); err != nil {
			return -1, fmt.Errorf("could not execute html template: %w", err)
		}
		if err := txtTemplate.Execute(&tplTXT, data); err != nil {
			return -1, fmt.Errorf("could not execute TXT template: %w", err)
		}

		if err := mail.Send(ctx, opts.config.From.Name, opts.config.From.Mail, candidate.Email, opts.configEmail.Subject, tplHTML.String(), tplTXT.String()); err != nil {
			if opts.stopOnError {
				return -1, fmt.Errorf("could not send email to %s: %w", candidate.Email, err)
			}
			// continue with next email
			log.Errorf("could not send email to %s: %v. Continuing sending emails", candidate.Email, err)
			count++
			continue
		}

		log.Debugf("sent email to %s", candidate.Email)

		candidate.Sent = true
		candidate.SentTime = time.Now()
		if err := db.Update(&candidate); err != nil {
			return -1, err
		}
		count++
	}
	return count, nil
}

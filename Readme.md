# mailsender

This little tool can be used to send personalized mass emails out to people. You can specify a template to use and some other options.

First you need to create the database using the `import` command and a csv with the format `"name","givenname","mail"` (the first line of the csv should contain the fieldnames and is ignored on import).

After import you can use the `send` subcommand to send emails to add people. On a succesful send the date is added to the database. You can also specify the count of how many emails should be sent in each run.

To send emails to the whole database run the command multiple times as it will only pick the emails that did not receive an email yet until 0 emails are sent.

For testing you can use https://github.com/axllent/mailpit

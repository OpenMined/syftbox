package email

type EmailInfo struct {
	FromName  string // Name of the sender
	FromEmail string // Email of the sender
	ToName    string // Name of the recipient
	ToEmail   string // Email of the recipient
	Subject   string // Subject of the email
	HTMLBody  string // HTML body of the email
}

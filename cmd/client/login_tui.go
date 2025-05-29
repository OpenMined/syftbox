package main // Or your desired package name e.g., tui

import (
	"fmt"
	"log"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/openmined/syftbox/internal/utils"
)

// View states
type viewState int

const (
	emailView viewState = iota
	otpView
)

// Strings
const (
	txtEmailPlaceholder = "your@email.com"
	txtOtpPlaceholder   = "••••••••"
	txtEmailPrompt      = "Enter your email address"
	txtRequestingOTP    = "Requesting OTP..."
	txtVerifyingOTP     = "Verifying OTP..."
	txtOtpPrompt        = "Enter the OTP sent to %s"
	txtOtpInfo          = "Please check your inbox or junk folder."
	txtInvalidEmail     = "Invalid email"
	txtInvalidOTP       = "Invalid OTP"
	txtHelp             = "Press 'Enter' to submit. 'Esc' to go back/quit. 'Ctrl+C' to quit."
)

// Styles
var (
	focusedStyle     = green
	helpStyle        = gray
	errorTextStyle   = red
	errorHeaderStyle = red.Bold(true)
	spinnerStyle     = cyan
	placeholderStyle = gray
	titleStyle       = cyan.Bold(true)
)

type LoginTUIOpts struct {
	Email              string
	ServerURL          string
	DataDir            string
	ConfigPath         string
	Note               string // optional note to display to the user
	EmailSubmitHandler func(email string) error
	OTPSubmitHandler   func(email, otp string) error
	EmailValidator     func(email string) bool
	OTPValidator       func(otp string) bool
}

// Model holds the application's state
type loginModel struct {
	opts *LoginTUIOpts

	emailInput textinput.Model
	otpInput   textinput.Model
	spinner    spinner.Model

	currentView  viewState
	previousView viewState

	isLoading    bool
	errorMessage string // For all types of errors
	message      string // For loading messages
	width        int

	submittedEmail string // To store the email for the OTP callback
}

// --- Messages ---
type emailProcessedMsg struct{ err error }
type otpProcessedMsg struct{ err error }

// newLoginModel creates the initial state of the application
func newLoginModel(opts *LoginTUIOpts) loginModel {
	email := textinput.New()
	email.Placeholder = txtEmailPlaceholder
	email.Focus()
	email.CharLimit = 64
	email.Width = 64
	email.PromptStyle = focusedStyle
	email.TextStyle = focusedStyle
	email.PlaceholderStyle = placeholderStyle

	otp := textinput.New()
	otp.Placeholder = txtOtpPlaceholder
	otp.CharLimit = 8
	otp.Width = 8
	otp.PromptStyle = focusedStyle
	otp.TextStyle = focusedStyle
	otp.PlaceholderStyle = placeholderStyle

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle

	return loginModel{
		opts:         opts,
		currentView:  emailView,
		previousView: emailView,
		emailInput:   email,
		otpInput:     otp,
		spinner:      s,
		isLoading:    false,
	}
}

// Init is the first command that is run when the program starts
func (m loginModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.spinner.Tick)
}

// Update handles messages and updates the model accordingly
func (m loginModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle input focus and key processing
		if m.emailInput.Focused() {
			// Clear error when user starts typing in the email field
			m.errorMessage = ""
			m.emailInput, cmd = m.emailInput.Update(msg)
			cmds = append(cmds, cmd)
		} else if m.otpInput.Focused() {
			// Clear error when user starts typing in the OTP field
			m.errorMessage = ""
			m.otpInput, cmd = m.otpInput.Update(msg)
			cmds = append(cmds, cmd)
		}

		// Handle special keys
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit

		case tea.KeyEsc:
			// Handle Escape key (go back)
			return m.handleEscapeKey()

		case tea.KeyEnter:
			if m.isLoading {
				return m, nil // Don't process Enter if already loading
			}

			switch m.currentView {
			case emailView:
				return m.submitEmail()

			case otpView:
				return m.submitOtp()
			}
		}

	case spinner.TickMsg:
		// Always update the spinner
		var spinnerCmd tea.Cmd
		m.spinner, spinnerCmd = m.spinner.Update(msg)
		cmds = append(cmds, spinnerCmd)

	case emailProcessedMsg:
		return m.handleEmailMsg(msg)

	case otpProcessedMsg:
		return m.handleOTPMsg(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
	}

	return m, tea.Batch(cmds...)
}

// handleEscapeKey processes the Escape key to navigate back
func (m loginModel) handleEscapeKey() (tea.Model, tea.Cmd) {
	// If we're in OTP view, go back to email view
	if m.currentView == otpView {
		m.currentView = emailView
		m.otpInput.Blur()
		m.emailInput.Focus()
		m.errorMessage = ""
		return m, textinput.Blink
	}

	// If we're already in email view, quit
	return m, tea.Quit
}

// submitEmail validates and submits the email address
func (m loginModel) submitEmail() (tea.Model, tea.Cmd) {
	m.previousView = emailView
	m.errorMessage = "" // Clear any previous error

	emailVal := strings.TrimSpace(m.emailInput.Value())
	if !m.opts.EmailValidator(emailVal) {
		m.errorMessage = txtInvalidEmail
		return m, nil
	}

	// Email format is valid, proceed with submission
	m.errorMessage = ""
	m.isLoading = true
	m.message = txtRequestingOTP
	m.submittedEmail = emailVal

	// Blur the input while loading
	m.emailInput.Blur()

	return m, func() tea.Msg {
		err := m.opts.EmailSubmitHandler(m.submittedEmail)
		return emailProcessedMsg{err: err}
	}
}

// submitOtp validates and submits the OTP
func (m loginModel) submitOtp() (tea.Model, tea.Cmd) {
	m.previousView = otpView
	m.errorMessage = "" // Clear any previous error

	otpVal := strings.TrimSpace(m.otpInput.Value())
	if !m.opts.OTPValidator(otpVal) {
		m.errorMessage = txtInvalidOTP
		return m, nil
	}

	m.errorMessage = ""
	m.isLoading = true
	m.message = txtVerifyingOTP

	// Blur the input while loading
	m.otpInput.Blur()

	return m, func() tea.Msg {
		err := m.opts.OTPSubmitHandler(m.submittedEmail, otpVal)
		return otpProcessedMsg{err: err}
	}
}

// handleEmailMsg processes the response from email submission
func (m loginModel) handleEmailMsg(msg emailProcessedMsg) (tea.Model, tea.Cmd) {
	m.isLoading = false

	if msg.err != nil {
		// Store the API error and refocus the email input
		m.errorMessage = fmt.Sprintf("%s %s", errorHeaderStyle.Render("ERROR: "), msg.err.Error())
		m.emailInput.Focus()
		return m, textinput.Blink
	}

	// Email submission to API was successful
	m.currentView = otpView
	m.message = ""
	m.errorMessage = "" // Clear any error messages

	// Focus the OTP input
	m.otpInput.Focus()

	return m, textinput.Blink
}

// handleOTPMsg processes the response from OTP submission
func (m loginModel) handleOTPMsg(msg otpProcessedMsg) (tea.Model, tea.Cmd) {
	m.isLoading = false

	if msg.err != nil {
		// Store the API error and refocus the OTP input
		m.errorMessage = fmt.Sprintf("%s %s", errorHeaderStyle.Render("ERROR:"), msg.err.Error())
		m.otpInput.Focus()
		return m, textinput.Blink
	}

	// OTP verification was successful. Quit the TUI.
	return m, tea.Quit
}

// View renders the UI based on the current model state.
func (m loginModel) View() string {
	var b strings.Builder
	// Render header
	b.WriteString(titleStyle.Render(utils.SyftBoxArt))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("%s%s\n", gray.Render("Server  "), green.Render(m.opts.ServerURL)))
	b.WriteString(fmt.Sprintf("%s%s\n", gray.Render("Data    "), green.Render(m.opts.DataDir)))
	b.WriteString(fmt.Sprintf("%s%s\n", gray.Render("Config  "), green.Render(m.opts.ConfigPath)))
	if m.opts.Note != "" {
		b.WriteString(fmt.Sprintf("\n%s\n", yellow.Render(m.opts.Note)))
	}
	b.WriteString("\n")

	// Render content based on current view
	switch m.currentView {
	case emailView:
		m.renderEmailView(&b)
	case otpView:
		m.renderOtpView(&b)
	}
	// Render loading, error, and help views
	m.renderLoadingView(&b)
	m.renderErrorView(&b)
	m.renderHelpView(&b)
	b.WriteString("\n")
	return b.String()
}

// renderEmailView renders the email input view
func (m loginModel) renderEmailView(b *strings.Builder) {
	b.WriteString(txtEmailPrompt)
	b.WriteString("\n\n")
	b.WriteString(m.emailInput.View())
}

// renderOtpView renders the OTP input view
func (m loginModel) renderOtpView(b *strings.Builder) {
	b.WriteString(fmt.Sprintf(txtOtpPrompt, green.Render(m.submittedEmail)))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render(txtOtpInfo))
	b.WriteString("\n\n")
	b.WriteString(m.otpInput.View())
}

// renderLoadingView renders the loading view
func (m loginModel) renderLoadingView(b *strings.Builder) {
	if m.isLoading {
		b.WriteString("\n\n")
		b.WriteString(fmt.Sprintf("%s %s", m.spinner.View(), m.message))
	}
}

// renderErrorView renders the error view
func (m loginModel) renderErrorView(b *strings.Builder) {
	if m.errorMessage != "" {
		b.WriteString("\n\n")
		b.WriteString(errorTextStyle.Render(m.errorMessage))
	}
}

// renderHelpView renders the help view
func (m loginModel) renderHelpView(b *strings.Builder) {
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render(txtHelp))
}

// RunLoginTUI is the main entry point to start the Bubble Tea login interface.
func RunLoginTUI(opts LoginTUIOpts) error {
	loginM := newLoginModel(&opts)
	model, err := tea.NewProgram(loginM, tea.WithAltScreen()).Run()
	if err != nil {
		log.Printf("Error running TUI: %v", err)
		return fmt.Errorf("TUI encountered an error during execution: %w", err)
	}

	// Check the final model state for errors or interruptions
	if fm, ok := model.(loginModel); ok {
		// Check for errors
		if fm.errorMessage != "" {
			return fmt.Errorf("login process interrupted: %s", fm.errorMessage)
		}

		// If we're still in email view when we exit, the user probably quit
		if fm.currentView == emailView {
			return fmt.Errorf("login process cancelled by user")
		}
	}

	// If we reach here, the login was successful or the user quit cleanly
	return nil
}

package main

import "github.com/charmbracelet/lipgloss"

var (
	// https://github.com/muesli/termenv/blob/master/ansicolors.go
	// https://github.com/fidian/ansi
	red       = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	green     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	cyan      = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	gray      = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	lightGray = lipgloss.NewStyle().Foreground(lipgloss.Color("248"))
)

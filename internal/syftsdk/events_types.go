package syftsdk

import (
	"errors"

	"github.com/openmined/syftbox/internal/syftmsg"
)

var (
	// ErrEventsNotConnected is returned when trying to use events without an active connection
	ErrEventsNotConnected = errors.New("events: not connected")
	// ErrEventsMessageQueueFull is returned when the message queue is full
	ErrEventsMessageQueueFull = errors.New("events: message queue full")
)

// EventMessage represents a message sent or received via the events system
type EventMessage struct {
	// The message payload
	Message *syftmsg.Message
}

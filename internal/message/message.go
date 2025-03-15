package message

import (
	"encoding/json"
	"fmt"
)

const (
	MsgSystem MessageType = iota
	MsgError
	MsgFileWrite
	MsgFileMove
	MsgFileDelete
)

type Message struct {
	Id   string      `json:"id"`
	Type MessageType `json:"typ"`
	Data any         `json:"dat"`
}

// UnmarshalJSON implements the json.Unmarshaler interface for Message
func (m *Message) UnmarshalJSON(data []byte) error {
	// Create a temporary struct to hold the raw JSON data
	type tempMessage struct {
		Id   string          `json:"id"`
		Type MessageType     `json:"typ"`
		Data json.RawMessage `json:"dat"`
	}

	var temp tempMessage
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	// Copy the simple fields
	m.Id = temp.Id
	m.Type = temp.Type

	// Unmarshal Data based on the message type
	switch m.Type {
	case MsgSystem:
		var sys System
		if err := json.Unmarshal(temp.Data, &sys); err != nil {
			return err
		}
		m.Data = sys
	case MsgError:
		var err Error
		if err := json.Unmarshal(temp.Data, &err); err != nil {
			return err
		}
		m.Data = err
	case MsgFileWrite:
		var fileWrite FileWrite
		if err := json.Unmarshal(temp.Data, &fileWrite); err != nil {
			return err
		}
		m.Data = fileWrite
	case MsgFileDelete:
		var fileDelete FileDelete
		if err := json.Unmarshal(temp.Data, &fileDelete); err != nil {
			return err
		}
		m.Data = fileDelete
	case MsgFileMove:
		var fileMove FileMove
		if err := json.Unmarshal(temp.Data, &fileMove); err != nil {
			return err
		}
		m.Data = fileMove
	default:
		return fmt.Errorf("unknown message type: %d", m.Type)
	}

	return nil
}

type Error struct {
	Code    int    `json:"cod"`
	Path    string `json:"pth"`
	Message string `json:"msg"`
}

type FileWrite struct {
	Path    string `json:"pth"`
	Etag    string `json:"etg"`
	Length  int64  `json:"len"`
	Content []byte `json:"con,omitempty"`
}

type FileDelete struct {
	Path string `json:"pth"`
}

type FileMove struct {
	From string `json:"frm"`
	To   string `json:"to"`
}

type System struct {
	SystemVersion string `json:"ver"`
	Message       string `json:"msg"`
}

package wsproto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/coder/websocket"
	"github.com/openmined/syftbox/internal/syftmsg"
	"github.com/vmihailenco/msgpack/v5"
)

// Encoding indicates which wire encoding is used for WebSocket messages.
type Encoding uint8

const (
	EncodingJSON Encoding = iota
	EncodingMsgPack
)

func (e Encoding) String() string {
	switch e {
	case EncodingMsgPack:
		return "msgpack"
	default:
		return "json"
	}
}

const (
	magic0  = byte('S')
	magic1  = byte('B')
	version = byte(1)
)

// PreferredEncoding parses a comma-separated preference list (e.g. "msgpack,json").
// Returns EncodingJSON if list is empty/unknown.
func PreferredEncoding(list string) Encoding {
	parts := strings.Split(list, ",")
	for _, p := range parts {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "msgpack":
			return EncodingMsgPack
		case "json":
			return EncodingJSON
		}
	}
	return EncodingJSON
}

// Marshal encodes a syftmsg.Message for WebSocket transport.
// JSON uses TextMessage with legacy structure.
// MsgPack uses BinaryMessage with an envelope: [magic][version][encoding][payload].
func Marshal(msg *syftmsg.Message, enc Encoding) (websocket.MessageType, []byte, error) {
	if enc == EncodingJSON {
		data, err := json.Marshal(msg)
		return websocket.MessageText, data, err
	}

	payload, err := marshalMsgpack(msg)
	if err != nil {
		return websocket.MessageBinary, nil, err
	}

	buf := make([]byte, 4+len(payload))
	buf[0], buf[1], buf[2], buf[3] = magic0, magic1, version, byte(enc)
	copy(buf[4:], payload)
	return websocket.MessageBinary, buf, nil
}

// Unmarshal decodes a WebSocket frame into syftmsg.Message.
// Legacy clients send TextMessage JSON without envelope.
func Unmarshal(typ websocket.MessageType, data []byte) (*syftmsg.Message, Encoding, error) {
	switch typ {
	case websocket.MessageText:
		var msg syftmsg.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, EncodingJSON, err
		}
		return &msg, EncodingJSON, nil

	case websocket.MessageBinary:
		if len(data) < 4 || data[0] != magic0 || data[1] != magic1 {
			return nil, EncodingMsgPack, errors.New("binary message missing SB envelope")
		}
		if data[2] != version {
			return nil, EncodingMsgPack, fmt.Errorf("unsupported ws envelope version: %d", data[2])
		}
		enc := Encoding(data[3])
		payload := data[4:]
		switch enc {
		case EncodingMsgPack:
			msg, err := unmarshalMsgpack(payload)
			return msg, enc, err
		case EncodingJSON:
			// Allow binary JSON envelopes if ever used.
			var msg syftmsg.Message
			if err := json.Unmarshal(payload, &msg); err != nil {
				return nil, enc, err
			}
			return &msg, enc, nil
		default:
			return nil, enc, fmt.Errorf("unknown ws encoding: %d", enc)
		}

	default:
		return nil, EncodingJSON, fmt.Errorf("unsupported websocket message type: %v", typ)
	}
}

type wireMessage struct {
	Id   string               `msgpack:"id"`
	Type syftmsg.MessageType  `msgpack:"typ"`
	Data []byte               `msgpack:"dat"`
}

func marshalMsgpack(msg *syftmsg.Message) ([]byte, error) {
	var dat []byte
	var err error

	switch msg.Type {
	case syftmsg.MsgSystem:
		switch v := msg.Data.(type) {
		case syftmsg.System:
			dat, err = msgpack.Marshal(&v)
		case *syftmsg.System:
			dat, err = msgpack.Marshal(v)
		default:
			return nil, fmt.Errorf("invalid system payload type: %T", msg.Data)
		}
	case syftmsg.MsgError:
		switch v := msg.Data.(type) {
		case syftmsg.Error:
			dat, err = msgpack.Marshal(&v)
		case *syftmsg.Error:
			dat, err = msgpack.Marshal(v)
		default:
			return nil, fmt.Errorf("invalid error payload type: %T", msg.Data)
		}
	case syftmsg.MsgFileWrite:
		switch v := msg.Data.(type) {
		case syftmsg.FileWrite:
			dat, err = msgpack.Marshal(&v)
		case *syftmsg.FileWrite:
			dat, err = msgpack.Marshal(v)
		default:
			return nil, fmt.Errorf("invalid file write payload type: %T", msg.Data)
		}
	case syftmsg.MsgFileDelete:
		switch v := msg.Data.(type) {
		case syftmsg.FileDelete:
			dat, err = msgpack.Marshal(&v)
		case *syftmsg.FileDelete:
			dat, err = msgpack.Marshal(v)
		default:
			return nil, fmt.Errorf("invalid file delete payload type: %T", msg.Data)
		}
	case syftmsg.MsgAck:
		switch v := msg.Data.(type) {
		case syftmsg.Ack:
			dat, err = msgpack.Marshal(&v)
		case *syftmsg.Ack:
			dat, err = msgpack.Marshal(v)
		default:
			return nil, fmt.Errorf("invalid ack payload type: %T", msg.Data)
		}
	case syftmsg.MsgNack:
		switch v := msg.Data.(type) {
		case syftmsg.Nack:
			dat, err = msgpack.Marshal(&v)
		case *syftmsg.Nack:
			dat, err = msgpack.Marshal(v)
		default:
			return nil, fmt.Errorf("invalid nack payload type: %T", msg.Data)
		}
	case syftmsg.MsgHttp:
		switch v := msg.Data.(type) {
		case syftmsg.HttpMsg:
			dat, err = msgpack.Marshal(&v)
		case *syftmsg.HttpMsg:
			dat, err = msgpack.Marshal(v)
		default:
			return nil, fmt.Errorf("invalid http payload type: %T", msg.Data)
		}
	case syftmsg.MsgFileNotify:
		switch v := msg.Data.(type) {
		case syftmsg.FileWrite:
			dat, err = msgpack.Marshal(&v)
		case *syftmsg.FileWrite:
			dat, err = msgpack.Marshal(v)
		default:
			return nil, fmt.Errorf("invalid file notify payload type: %T", msg.Data)
		}
	case syftmsg.MsgACLManifest:
		switch v := msg.Data.(type) {
		case syftmsg.ACLManifest:
			dat, err = msgpack.Marshal(&v)
		case *syftmsg.ACLManifest:
			dat, err = msgpack.Marshal(v)
		default:
			return nil, fmt.Errorf("invalid ACL manifest payload type: %T", msg.Data)
		}
	default:
		return nil, fmt.Errorf("unknown message type: %d", msg.Type)
	}
	if err != nil {
		return nil, err
	}

	w := wireMessage{Id: msg.Id, Type: msg.Type, Data: dat}
	return msgpack.Marshal(&w)
}

func unmarshalMsgpack(payload []byte) (*syftmsg.Message, error) {
	var w wireMessage
	dec := msgpack.NewDecoder(bytes.NewReader(payload))
	dec.SetCustomStructTag("msgpack")
	if err := dec.Decode(&w); err != nil {
		return nil, err
	}

	msg := &syftmsg.Message{Id: w.Id, Type: w.Type}
	switch w.Type {
	case syftmsg.MsgSystem:
		var sys syftmsg.System
		if err := msgpack.Unmarshal(w.Data, &sys); err != nil {
			return nil, err
		}
		msg.Data = sys
	case syftmsg.MsgError:
		var e syftmsg.Error
		if err := msgpack.Unmarshal(w.Data, &e); err != nil {
			return nil, err
		}
		msg.Data = e
	case syftmsg.MsgFileWrite:
		var fw syftmsg.FileWrite
		if err := msgpack.Unmarshal(w.Data, &fw); err != nil {
			return nil, err
		}
		msg.Data = fw
	case syftmsg.MsgFileDelete:
		var fd syftmsg.FileDelete
		if err := msgpack.Unmarshal(w.Data, &fd); err != nil {
			return nil, err
		}
		msg.Data = fd
	case syftmsg.MsgAck:
		var ack syftmsg.Ack
		if err := msgpack.Unmarshal(w.Data, &ack); err != nil {
			return nil, err
		}
		msg.Data = ack
	case syftmsg.MsgNack:
		var nack syftmsg.Nack
		if err := msgpack.Unmarshal(w.Data, &nack); err != nil {
			return nil, err
		}
		msg.Data = nack
	case syftmsg.MsgHttp:
		var httpMsg syftmsg.HttpMsg
		if err := msgpack.Unmarshal(w.Data, &httpMsg); err != nil {
			return nil, err
		}
		msg.Data = &httpMsg
	case syftmsg.MsgFileNotify:
		var fn syftmsg.FileWrite
		if err := msgpack.Unmarshal(w.Data, &fn); err != nil {
			return nil, err
		}
		msg.Data = fn
	case syftmsg.MsgACLManifest:
		var manifest syftmsg.ACLManifest
		if err := msgpack.Unmarshal(w.Data, &manifest); err != nil {
			return nil, err
		}
		msg.Data = &manifest
	default:
		return nil, fmt.Errorf("unknown message type: %d", w.Type)
	}

	return msg, nil
}

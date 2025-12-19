package wsproto

import (
	"encoding/json"
	"testing"

	"github.com/coder/websocket"
	"github.com/openmined/syftbox/internal/syftmsg"
	"github.com/stretchr/testify/require"
)

func TestCodec_LegacyJSONRoundTrip(t *testing.T) {
	content := []byte("hello world")
	msg := syftmsg.NewFileWrite("a/b.request", "etag", int64(len(content)), content)

	typ, data, err := Marshal(msg, EncodingJSON)
	require.NoError(t, err)
	require.Equal(t, websocket.MessageText, typ)

	decoded, enc, err := Unmarshal(typ, data)
	require.NoError(t, err)
	require.Equal(t, EncodingJSON, enc)

	fw, ok := decoded.Data.(syftmsg.FileWrite)
	require.True(t, ok)
	require.Equal(t, "a/b.request", fw.Path)
	require.Equal(t, content, fw.Content)
}

func TestCodec_MsgPackRoundTrip_WithPointerData(t *testing.T) {
	content := make([]byte, 1024)
	for i := range content {
		content[i] = byte(i % 251)
	}
	msg := syftmsg.NewFileWrite("x/y.request", "etag2", int64(len(content)), content)

	typ, data, err := Marshal(msg, EncodingMsgPack)
	require.NoError(t, err)
	require.Equal(t, websocket.MessageBinary, typ)
	require.True(t, len(data) > 4)
	require.Equal(t, byte('S'), data[0])
	require.Equal(t, byte('B'), data[1])
	require.Equal(t, byte(1), data[2])
	require.Equal(t, byte(EncodingMsgPack), data[3])

	decoded, enc, err := Unmarshal(typ, data)
	require.NoError(t, err)
	require.Equal(t, EncodingMsgPack, enc)

	fw, ok := decoded.Data.(syftmsg.FileWrite)
	require.True(t, ok)
	require.Equal(t, "x/y.request", fw.Path)
	require.Equal(t, content, fw.Content)
}

func TestCodec_MsgPackRoundTrip_WithValueData(t *testing.T) {
	// Simulate a message that came from JSON decoding where Data is a value.
	content := []byte("abc")
	msg := &syftmsg.Message{
		Id:   "id1",
		Type: syftmsg.MsgSystem,
		Data: syftmsg.System{SystemVersion: "1.2.3", Message: "ok"},
	}

	typ, data, err := Marshal(msg, EncodingMsgPack)
	require.NoError(t, err)
	require.Equal(t, websocket.MessageBinary, typ)

	decoded, enc, err := Unmarshal(typ, data)
	require.NoError(t, err)
	require.Equal(t, EncodingMsgPack, enc)

	sys, ok := decoded.Data.(syftmsg.System)
	require.True(t, ok)
	require.Equal(t, "1.2.3", sys.SystemVersion)
	require.Equal(t, "ok", sys.Message)

	// Also check file write value case.
	fwMsg := &syftmsg.Message{
		Id:   "id2",
		Type: syftmsg.MsgFileWrite,
		Data: syftmsg.FileWrite{Path: "p", ETag: "e", Length: int64(len(content)), Content: content},
	}
	_, fwBin, err := Marshal(fwMsg, EncodingMsgPack)
	require.NoError(t, err)
	decodedFW, _, err := Unmarshal(websocket.MessageBinary, fwBin)
	require.NoError(t, err)
	fw, ok := decodedFW.Data.(syftmsg.FileWrite)
	require.True(t, ok)
	require.Equal(t, content, fw.Content)
}

func TestCodec_UnmarshalTextMatchesStandardJSON(t *testing.T) {
	msg := syftmsg.NewSystemMessage("v", "hi")
	j, err := json.Marshal(msg)
	require.NoError(t, err)

	decoded, enc, err := Unmarshal(websocket.MessageText, j)
	require.NoError(t, err)
	require.Equal(t, EncodingJSON, enc)

	var std syftmsg.Message
	require.NoError(t, json.Unmarshal(j, &std))
	require.Equal(t, std.Type, decoded.Type)
	require.Equal(t, std.Id, decoded.Id)
}

func TestCodec_RejectsBinaryWithoutEnvelope(t *testing.T) {
	_, _, err := Unmarshal(websocket.MessageBinary, []byte{0, 1, 2, 3})
	require.Error(t, err)
}


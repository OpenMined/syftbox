package syftmsg

import (
	"testing"

	"github.com/vmihailenco/msgpack/v5"
)

func TestAckMsgpackFieldNames(t *testing.T) {
	b, err := msgpack.Marshal(&Ack{OriginalId: "abc"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := msgpack.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, ok := m["OriginalId"]; !ok || got != "abc" {
		t.Fatalf("expected msgpack key OriginalId=abc, got %#v", m)
	}
	if _, ok := m["OriginalID"]; ok {
		t.Fatalf("unexpected msgpack key OriginalID present: %#v", m)
	}
}

func TestNackMsgpackFieldNames(t *testing.T) {
	b, err := msgpack.Marshal(&Nack{OriginalId: "abc", Error: "nope"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := msgpack.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, ok := m["OriginalId"]; !ok || got != "abc" {
		t.Fatalf("expected msgpack key OriginalId=abc, got %#v", m)
	}
	if got, ok := m["Error"]; !ok || got != "nope" {
		t.Fatalf("expected msgpack key Error=nope, got %#v", m)
	}
	if _, ok := m["OriginalID"]; ok {
		t.Fatalf("unexpected msgpack key OriginalID present: %#v", m)
	}
}


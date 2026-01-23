package syftmsg

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type ACLEntry struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

type ACLManifest struct {
	Version   int        `json:"version"`
	Datasite  string     `json:"datasite"`
	For       string     `json:"for"`
	ForHash   string     `json:"for_hash"`
	Generated time.Time  `json:"generated"`
	ACLOrder  []ACLEntry `json:"acl_order"`
}

func HashPrincipal(principal string) string {
	if principal == "*" {
		return "public"
	}
	h := sha256.Sum256([]byte(principal))
	return hex.EncodeToString(h[:8])
}

func NewACLManifest(datasite, forPrincipal string, aclOrder []ACLEntry) *ACLManifest {
	return &ACLManifest{
		Version:   1,
		Datasite:  datasite,
		For:       forPrincipal,
		ForHash:   HashPrincipal(forPrincipal),
		Generated: time.Now().UTC(),
		ACLOrder:  aclOrder,
	}
}

func NewACLManifestMessage(manifest *ACLManifest) *Message {
	return &Message{
		Id:   generateID(),
		Type: MsgACLManifest,
		Data: manifest,
	}
}

package sync

import (
	"testing"
	"time"

	"github.com/openmined/syftbox/internal/syftmsg"
)

func TestACLStagingPendingExpires(t *testing.T) {
	now := time.Date(2026, 1, 19, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	manager := NewACLStagingManager(
		nil,
		WithACLStagingTTL(2*time.Second),
		WithACLStagingGrace(0),
		WithACLStagingNow(clock),
	)

	manifest := &syftmsg.ACLManifest{
		Datasite: "bob@example.com",
		ACLOrder: []syftmsg.ACLEntry{
			{Path: "bob@example.com/app_data/aclprop/rpc", Hash: "h"},
		},
	}

	manager.SetManifest(manifest)
	if !manager.HasPendingManifest("bob@example.com") {
		t.Fatal("expected pending manifest")
	}

	now = now.Add(3 * time.Second)
	if manager.HasPendingManifest("bob@example.com") {
		t.Fatal("expected pending manifest to expire")
	}

	if manager.IsPendingACLPath("bob@example.com/app_data/aclprop/rpc/syft.pub.yaml") {
		t.Fatal("expected ACL path to no longer be pending after expiry")
	}
}

func TestACLStagingGraceWindow(t *testing.T) {
	now := time.Date(2026, 1, 19, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	manager := NewACLStagingManager(
		nil,
		WithACLStagingTTL(1*time.Second),
		WithACLStagingGrace(5*time.Second),
		WithACLStagingNow(clock),
	)

	manifest := &syftmsg.ACLManifest{
		Datasite: "bob@example.com",
		ACLOrder: []syftmsg.ACLEntry{
			{Path: "bob@example.com/app_data/aclprop/rpc", Hash: "h"},
		},
	}

	manager.SetManifest(manifest)

	now = now.Add(2 * time.Second)
	if !manager.IsPendingACLPath("bob@example.com/app_data/aclprop/rpc/syft.pub.yaml") {
		t.Fatal("expected ACL path to be protected by grace window after expiry")
	}

	now = now.Add(6 * time.Second)
	if manager.IsPendingACLPath("bob@example.com/app_data/aclprop/rpc/syft.pub.yaml") {
		t.Fatal("expected ACL path to be unprotected after grace window")
	}
}

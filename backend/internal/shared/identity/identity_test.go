package identity

import (
	"context"
	"net/http"
	"testing"

	"health-nexus/internal/shared/contextkeys"
)

func reqWithUserID(uid int64) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "/", http.NoBody)
	return r.WithContext(context.WithValue(r.Context(), contextkeys.UserID, uid))
}

func reqWithDeviceID(did string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "/", http.NoBody)
	return r.WithContext(context.WithValue(r.Context(), contextkeys.DeviceID, did))
}

func TestFromRequestOrZero(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/", http.NoBody)

	// 两者皆无 → 零值（匿名且无设备，IP 兜底场景）
	if id := FromRequestOrZero(req); !id.Anon() || id.DeviceID != "" {
		t.Fatalf("empty ctx: got %+v, want zero anon identity", id)
	}

	// 有 user → Anon()==false
	if id := FromRequestOrZero(reqWithUserID(0)); !id.Anon() {
		t.Fatal("UserID=0 should be treated as anonymous")
	}
	if id := FromRequestOrZero(reqWithUserID(42)); id.Anon() || id.UserID != 42 {
		t.Fatalf("UserID=42: got %+v", id)
	}

	// 无 user 有 device → 匿名 + device
	if id := FromRequestOrZero(reqWithDeviceID("device-abc")); !id.Anon() || id.DeviceID != "device-abc" {
		t.Fatalf("device only: got %+v", id)
	}
}

func TestIdentity_IsValidAndSubject(t *testing.T) {
	zero := Identity{}
	if zero.IsValid() {
		t.Fatal("zero identity should be invalid")
	}
	user := Identity{UserID: 1}
	if !user.IsValid() {
		t.Fatal("user identity should be valid")
	}
	anon := Identity{DeviceID: "d"}
	if !anon.IsValid() || !anon.Anon() {
		t.Fatal("anon identity with device should be valid")
	}
}

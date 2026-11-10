package gateway

import (
	"encoding/binary"
	"net/http/httptest"
	"testing"
)

func TestAuthentication(t *testing.T) {
	h := (&HTTP{Token: "secret", Devices: map[string]Device{}}).Handler()
	for _, path := range []string{"/api/tablets", "/healthz"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 401
		if path == "/healthz" {
			want = 200
		}
		if w.Code != want {
			t.Fatal(path, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/tablets", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "[]\n" {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestTouchWireFormat(t *testing.T) {
	b, err := (Command{Type: "touch", X: 5, Y: 10, Width: 100, Height: 200}).Encode()
	if err != nil || len(b) != 32 || binary.BigEndian.Uint32(b[10:]) != 5 || binary.BigEndian.Uint16(b[20:]) != 200 {
		t.Fatal(b, err)
	}
	if _, err = (Command{Type: "touch", Width: 0}).Encode(); err == nil {
		t.Fatal("invalid coordinates accepted")
	}
}

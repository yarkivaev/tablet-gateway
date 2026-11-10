package gateway

import (
	"bytes"
	"encoding/json"
	"github.com/pion/webrtc/v4"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestBrowserNegotiationAndDisconnectCleanup(t *testing.T) {
	var starts, stops atomic.Int32
	h := &HTTP{Token: "test-token", Devices: map[string]Device{"demo": {ID: "demo"}}, Manager: NewManager(func(string, func(Frame)) (Capture, error) {
		starts.Add(1)
		return &fakeCapture{stop: func() { stops.Add(1) }}, nil
	})}
	defer h.Close()
	browser, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	browser.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	dc, err := browser.CreateDataChannel("control", nil)
	if err != nil {
		t.Fatal(err)
	}
	opened := make(chan struct{})
	dc.OnOpen(func() { dc.SendText(`{"type":"ping"}`); close(opened) })
	offer, err := browser.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gather := webrtc.GatheringCompletePromise(browser)
	if err = browser.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gather
	body, _ := json.Marshal(browser.LocalDescription())
	req := httptest.NewRequest("POST", "/api/tablets/demo/offer", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	h.Handler().ServeHTTP(response, req)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var answer webrtc.SessionDescription
	if err = json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if err = browser.SetRemoteDescription(answer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-opened:
	case <-time.After(10 * time.Second):
		t.Fatal("DataChannel did not open")
	}
	if starts.Load() != 1 {
		t.Fatal("capture not started")
	}
	dc.Close()
	browser.Close()
	until := time.Now().Add(5 * time.Second)
	for stops.Load() != 1 && time.Now().Before(until) {
		time.Sleep(10 * time.Millisecond)
	}
	if stops.Load() != 1 {
		t.Fatal("capture leaked after disconnect")
	}
}

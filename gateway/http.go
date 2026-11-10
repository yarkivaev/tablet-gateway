package gateway

import (
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

type HTTP struct {
	Manager  *Manager
	Devices  map[string]Device
	Token    string
	ICE      []webrtc.ICEServer
	PublicIP string
	mu       sync.Mutex
	peers    map[*webrtc.PeerConnection]func()
}

func (h *HTTP) Close() {
	h.mu.Lock()
	list := []func(){}
	for _, close := range h.peers {
		list = append(list, close)
	}
	h.mu.Unlock()
	for _, close := range list {
		close()
	}
}
func (h *HTTP) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /api/tablets", func(w http.ResponseWriter, r *http.Request) {
		list := []map[string]string{}
		for _, d := range h.Devices {
			list = append(list, map[string]string{"id": d.ID, "label": d.Label})
		}
		json.NewEncoder(w).Encode(list)
	})
	mux.HandleFunc("POST /api/tablets/{id}/offer", h.offer)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path != "/healthz" {
			if h.Token == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+h.Token)) != 1 {
				http.Error(w, "unauthorized", 401)
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func (h *HTTP) offer(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.Devices[r.PathValue("id")]; !ok {
		http.Error(w, "unknown tablet", 404)
		return
	}
	var offer webrtc.SessionDescription
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&offer); err != nil || offer.Type != webrtc.SDPTypeOffer {
		http.Error(w, "invalid offer", 400)
		return
	}
	settings := webrtc.SettingEngine{}
	settings.SetEphemeralUDPPortRange(50000, 50100)
	if h.PublicIP != "" {
		settings.SetNAT1To1IPs([]string{h.PublicIP}, webrtc.ICECandidateTypeHost)
	}
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(settings)).NewPeerConnection(webrtc.Configuration{ICEServers: h.ICE})
	if err != nil {
		http.Error(w, "peer unavailable", 503)
		return
	}
	var viewer *Viewer
	var state sync.Mutex
	closed := false
	closePeer := func() {
		state.Lock()
		if closed {
			state.Unlock()
			return
		}
		closed = true
		v := viewer
		state.Unlock()
		if v != nil {
			v.Close()
		}
		pc.Close()
		h.mu.Lock()
		delete(h.peers, pc)
		h.mu.Unlock()
	}
	h.mu.Lock()
	if h.peers == nil {
		h.peers = map[*webrtc.PeerConnection]func(){}
	}
	if len(h.peers) >= 32 {
		h.mu.Unlock()
		pc.Close()
		http.Error(w, "capacity reached", 503)
		return
	}
	h.peers[pc] = closePeer
	h.mu.Unlock()
	deadline := time.AfterFunc(20*time.Second, closePeer)
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateConnected {
			deadline.Stop()
		}
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed || s == webrtc.PeerConnectionStateDisconnected {
			go closePeer()
		}
	})
	track, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000, SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"}, "screen", "tablet")
	if err != nil {
		closePeer()
		http.Error(w, "track unavailable", 500)
		return
	}
	sender, err := pc.AddTrack(track)
	if err != nil {
		closePeer()
		http.Error(w, "track unavailable", 500)
		return
	}
	go func() {
		b := make([]byte, 1500)
		for {
			if _, _, err := sender.Read(b); err != nil {
				return
			}
		}
	}()
	frames := make(chan Frame, 8)
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != "control" {
			return
		}
		last := time.Now()
		var heartbeat sync.Mutex
		dc.OnOpen(func() {
			go func() {
				ticker := time.NewTicker(5 * time.Second)
				defer ticker.Stop()
				for range ticker.C {
					state.Lock()
					ended := closed
					state.Unlock()
					if ended {
						return
					}
					heartbeat.Lock()
					expired := time.Since(last) > 15*time.Second
					heartbeat.Unlock()
					if expired {
						closePeer()
						return
					}
				}
			}()
		})
		dc.OnClose(func() { go closePeer() })
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			if len(msg.Data) > 2048 {
				return
			}
			var c Command
			if json.Unmarshal(msg.Data, &c) != nil {
				return
			}
			heartbeat.Lock()
			last = time.Now()
			heartbeat.Unlock()
			state.Lock()
			v := viewer
			state.Unlock()
			if v == nil {
				return
			}
			switch c.Type {
			case "ping":
				dc.SendText(`{"type":"pong"}`)
			case "claim":
				dc.SendText(fmt.Sprintf(`{"type":"control","granted":%t}`, v.Claim()))
			case "release":
				v.Release()
			case "touch", "key":
				if data, err := c.Encode(); err == nil {
					v.Control(data)
				}
			}
		})
	})
	if err = pc.SetRemoteDescription(offer); err != nil {
		closePeer()
		http.Error(w, "invalid SDP", 400)
		return
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		closePeer()
		http.Error(w, "answer failed", 500)
		return
	}
	gather := webrtc.GatheringCompletePromise(pc)
	if err = pc.SetLocalDescription(answer); err != nil {
		closePeer()
		http.Error(w, "answer failed", 500)
		return
	}
	select {
	case <-gather:
	case <-r.Context().Done():
		closePeer()
		return
	case <-time.After(10 * time.Second):
		closePeer()
		http.Error(w, "ICE timeout", 504)
		return
	}
	state.Lock()
	if closed {
		state.Unlock()
		http.Error(w, "session expired", 504)
		return
	}
	viewer, err = h.Manager.Join(r.PathValue("id"), func(f Frame) {
		select {
		case frames <- f:
		default:
			go closePeer()
		}
	})
	state.Unlock()
	if err != nil {
		closePeer()
		http.Error(w, "tablet unavailable", 503)
		return
	}
	go func() {
		var config []byte
		var previous uint64
		for {
			state.Lock()
			ended := closed
			state.Unlock()
			if ended {
				return
			}
			select {
			case <-viewer.Done():
				closePeer()
				return
			case f := <-frames:
				if f.Config {
					config = append([]byte(nil), f.Data...)
					continue
				}
				duration := time.Second / 15
				if previous > 0 && f.PTS > previous {
					duration = time.Duration(f.PTS-previous) * time.Microsecond
				}
				previous = f.PTS
				data := append(append([]byte{}, config...), f.Data...)
				if err := track.WriteSample(media.Sample{Data: data, Duration: duration}); err != nil {
					closePeer()
					return
				}
			case <-time.After(time.Second):
			}
		}
	}()
	w.Header().Set("Content-Type", "application/json")
	if json.NewEncoder(w).Encode(pc.LocalDescription()) != nil {
		closePeer()
	}
}

type Command struct {
	Type   string `json:"type"`
	Action uint8  `json:"action"`
	X      uint32 `json:"x"`
	Y      uint32 `json:"y"`
	Width  uint16 `json:"width"`
	Height uint16 `json:"height"`
	Key    uint32 `json:"key"`
}

func (c Command) Encode() ([]byte, error) {
	if c.Type == "key" {
		if c.Action > 1 || !(c.Key == 3 || c.Key == 4 || c.Key == 187) {
			return nil, fmt.Errorf("invalid key")
		}
		b := make([]byte, 14)
		b[0] = 0
		b[1] = c.Action
		binary.BigEndian.PutUint32(b[2:], c.Key)
		return b, nil
	}
	if c.Type != "touch" || c.Action > 2 || c.Width == 0 || c.Height == 0 || c.X >= uint32(c.Width) || c.Y >= uint32(c.Height) {
		return nil, fmt.Errorf("invalid touch")
	}
	b := make([]byte, 32)
	b[0] = 2
	b[1] = c.Action
	binary.BigEndian.PutUint64(b[2:], 0xfffffffffffffffe)
	binary.BigEndian.PutUint32(b[10:], c.X)
	binary.BigEndian.PutUint32(b[14:], c.Y)
	binary.BigEndian.PutUint16(b[18:], c.Width)
	binary.BigEndian.PutUint16(b[20:], c.Height)
	if c.Action != 1 {
		binary.BigEndian.PutUint16(b[22:], 0xffff)
	}
	return b, nil
}

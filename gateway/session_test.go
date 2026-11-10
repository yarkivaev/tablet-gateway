package gateway

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestSharedCaptureStopsWithLastViewer(t *testing.T) {
	var starts, stops atomic.Int32
	m := NewManager(func(string, func(Frame)) (Capture, error) {
		starts.Add(1)
		return &fakeCapture{stop: func() { stops.Add(1) }}, nil
	})
	a, err := m.Join("tablet", func(Frame) {})
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Join("tablet", func(Frame) {})
	if err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 1 {
		t.Fatal("duplicate capture")
	}
	a.Close()
	if stops.Load() != 0 {
		t.Fatal("stopped with active viewer")
	}
	b.Close()
	b.Close()
	if stops.Load() != 1 {
		t.Fatal("capture must stop exactly once")
	}
}
func TestFailedStartAllowsRetry(t *testing.T) {
	n := 0
	m := NewManager(func(string, func(Frame)) (Capture, error) {
		n++
		if n == 1 {
			return nil, errors.New("offline")
		}
		return &fakeCapture{stop: func() {}}, nil
	})
	if _, err := m.Join("tablet", func(Frame) {}); err == nil {
		t.Fatal("expected failure")
	}
	v, err := m.Join("tablet", func(Frame) {})
	if err != nil {
		t.Fatal(err)
	}
	v.Close()
}
func TestExclusiveControl(t *testing.T) {
	m := NewManager(func(string, func(Frame)) (Capture, error) { return &fakeCapture{stop: func() {}}, nil })
	a, _ := m.Join("t", func(Frame) {})
	b, _ := m.Join("t", func(Frame) {})
	defer a.Close()
	defer b.Close()
	if !a.Claim() || b.Claim() {
		t.Fatal("control not exclusive")
	}
	a.Release()
	if !b.Claim() {
		t.Fatal("control not released")
	}
}

type fakeCapture struct{ stop func() }

func (f *fakeCapture) Close()               { f.stop() }
func (f *fakeCapture) Control([]byte) error { return nil }

func TestLateViewerReceivesCodecConfiguration(t *testing.T) {
	var publish func(Frame)
	m := NewManager(func(_ string, send func(Frame)) (Capture, error) {
		publish = send
		return &fakeCapture{stop: func() {}}, nil
	})
	a, _ := m.Join("t", func(Frame) {})
	defer a.Close()
	publish(Frame{Config: true, Data: []byte{1, 2, 3}})
	var config Frame
	b, _ := m.Join("t", func(f Frame) { config = f })
	defer b.Close()
	if !config.Config || len(config.Data) != 3 {
		t.Fatal("late viewer missing codec config")
	}
}

type dyingCapture struct {
	fakeCapture
	done chan struct{}
}

func (d *dyingCapture) Done() <-chan struct{} { return d.done }
func TestFailedCaptureIsRemoved(t *testing.T) {
	died := make(chan struct{})
	n := 0
	m := NewManager(func(string, func(Frame)) (Capture, error) {
		n++
		return &dyingCapture{fakeCapture{stop: func() {}}, died}, nil
	})
	a, _ := m.Join("t", func(Frame) {})
	close(died)
	select {
	case <-a.Done():
	case <-time.After(time.Second):
		t.Fatal("capture failure not propagated")
	}
	b, err := m.Join("t", func(Frame) {})
	if err != nil || n != 2 {
		t.Fatal("dead session reused")
	}
	a.Close()
	b.Close()
}

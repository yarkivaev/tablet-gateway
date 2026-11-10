package gateway

import "sync"

type Frame struct {
	Data   []byte
	PTS    uint64
	Config bool
}
type Capture interface {
	Close()
	Control([]byte) error
}
type Factory func(string, func(Frame)) (Capture, error)
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*session
	start    Factory
}
type session struct {
	capture Capture
	viewers map[*Viewer]func(Frame)
	owner   *Viewer
	config  *Frame
	done    chan struct{}
	ended   bool
}
type Viewer struct {
	manager *Manager
	id      string
	session *session
	once    sync.Once
}

func NewManager(start Factory) *Manager {
	return &Manager{sessions: map[string]*session{}, start: start}
}
func (m *Manager) Join(id string, send func(Frame)) (*Viewer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[id]
	if s == nil {
		s = &session{viewers: map[*Viewer]func(Frame){}, done: make(chan struct{})}
		c, err := m.start(id, func(f Frame) {
			m.mu.Lock()
			if f.Config {
				copyFrame := f
				copyFrame.Data = append([]byte(nil), f.Data...)
				s.config = &copyFrame
			}
			callbacks := make([]func(Frame), 0, len(s.viewers))
			for _, cb := range s.viewers {
				callbacks = append(callbacks, cb)
			}
			m.mu.Unlock()
			for _, cb := range callbacks {
				cb(f)
			}
		})
		if err != nil {
			return nil, err
		}
		s.capture = c
		m.sessions[id] = s
		if observable, ok := c.(interface{ Done() <-chan struct{} }); ok {
			go func() {
				<-observable.Done()
				m.mu.Lock()
				if m.sessions[id] == s {
					delete(m.sessions, id)
				}
				if !s.ended {
					s.ended = true
					s.owner = nil
					close(s.done)
				}
				m.mu.Unlock()
			}()
		}
	}
	v := &Viewer{manager: m, id: id, session: s}
	s.viewers[v] = send
	if s.config != nil {
		send(*s.config)
	}
	return v, nil
}
func (v *Viewer) Close() {
	v.once.Do(func() {
		m := v.manager
		m.mu.Lock()
		delete(v.session.viewers, v)
		if v.session.owner == v {
			v.session.owner = nil
		}
		last := len(v.session.viewers) == 0
		if last {
			if m.sessions[v.id] == v.session {
				delete(m.sessions, v.id)
			}
			if !v.session.ended {
				v.session.ended = true
				close(v.session.done)
			}
		}
		m.mu.Unlock()
		if last {
			v.session.capture.Close()
		}
	})
}
func (v *Viewer) Done() <-chan struct{} { return v.session.done }
func (v *Viewer) Claim() bool {
	m := v.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := v.session.viewers[v]; !ok || v.session.ended {
		return false
	}
	if v.session.owner != nil && v.session.owner != v {
		return false
	}
	v.session.owner = v
	return true
}
func (v *Viewer) Release() {
	m := v.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if v.session.owner == v {
		v.session.owner = nil
	}
}
func (v *Viewer) Control(data []byte) error {
	m := v.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if v.session.owner != v {
		return nil
	}
	return v.session.capture.Control(data)
}

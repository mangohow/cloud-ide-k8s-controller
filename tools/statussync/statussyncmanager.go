package statussync

import (
	"errors"
	"sync"
)

var (
	ErrNotFound = errors.New("not Found")
)

// StatusInformer 状态同步通知器，当Pod状态处于Running时，通知对端
type StatusInformer struct {
	sync.Mutex
	m map[string]chan struct{}
}

func NewManager() *StatusInformer {
	return &StatusInformer{
		m: make(map[string]chan struct{}),
	}
}

func (m *StatusInformer) Add(name string) <-chan struct{} {
	m.Lock()
	defer m.Unlock()
	ch := make(chan struct{}, 1)
	m.m[name] = ch

	return ch
}

func (m *StatusInformer) Delete(name string) {
	m.Lock()
	defer m.Unlock()
	delete(m.m, name)
}

func (m *StatusInformer) Sync(name string) error {
	m.Lock()
	defer m.Unlock()
	ch, ok := m.m[name]
	if !ok {
		return ErrNotFound
	}
	ch <- struct{}{}

	return nil
}

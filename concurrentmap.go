package smpperf

import (
	"sync"
)

type ConcurrentStringMap struct {
	mp    map[string]string
	mutex sync.RWMutex
}

func NewConcurrentStringMap() *ConcurrentStringMap {
	return &ConcurrentStringMap{
		mp: map[string]string{},
	}
}

func (m *ConcurrentStringMap) Set(key string, value string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.mp[key] = value
}

func (m *ConcurrentStringMap) Get(key string) (string, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	v, ok := m.mp[key]
	return v, ok
}

func (m *ConcurrentStringMap) Delete(key string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.mp, key)
}

func (m *ConcurrentStringMap) Keys() []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	keys := make([]string, len(m.mp))
	i := 0
	for key := range m.mp {
		keys[i] = key
		i++
	}
	return keys
}

type ConcurrentIntMap struct {
	mp    map[interface{}]*SafeInt
	mutex sync.RWMutex
}

func NewConcurrentIntMap() *ConcurrentIntMap {
	return &ConcurrentIntMap{
		mp: map[interface{}]*SafeInt{},
	}
}

func (m *ConcurrentIntMap) Create(key interface{}) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.mp[key] = NewSafeInt(0)
}

func (m *ConcurrentIntMap) Increment(key interface{}) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.mp[key].Increment()
}

func (m *ConcurrentIntMap) Get(key interface{}) (*SafeInt, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	v, ok := m.mp[key]
	return v, ok
}

func (m *ConcurrentIntMap) GetAll() map[interface{}]int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	projectMap := map[interface{}]int{}
	for _, k := range m.Keys() {
		if v, ok := m.Get(k); ok {
			projectMap[k] = v.Val()
		}
	}
	return projectMap
}

func (m *ConcurrentIntMap) Keys() []interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	keys := make([]interface{}, len(m.mp))
	i := 0
	for key := range m.mp {
		keys[i] = key
		i++
	}
	return keys
}

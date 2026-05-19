package main

import "sync"

// Store хранит состояние процессов и обработанные ключи идемпотентности.
// Все методы потокобезопасны.
type Store struct {
	mu              sync.Mutex
	processes       map[string]*Process
	idempotencyKeys map[string]string // idempotency_key → state после обработки
}

func NewStore() *Store {
	return &Store{
		processes:       make(map[string]*Process),
		idempotencyKeys: make(map[string]string),
	}
}

// Lock/Unlock — явная блокировка для handler, которому нужна атомарная операция
// «проверить идемпотентность + изменить состояние».
func (s *Store) Lock()   { s.mu.Lock() }
func (s *Store) Unlock() { s.mu.Unlock() }

// CheckIdempotency возвращает сохранённое состояние, если ключ уже обработан.
func (s *Store) CheckIdempotency(key string) (state string, seen bool) {
	state, seen = s.idempotencyKeys[key]
	return
}

// SaveIdempotency сохраняет результат обработки ключа.
func (s *Store) SaveIdempotency(key, state string) {
	s.idempotencyKeys[key] = state
}

// GetOrCreate возвращает процесс по ключу; если не существует — создаёт новый.
func (s *Store) GetOrCreate(key string) *Process {
	proc, ok := s.processes[key]
	if !ok {
		proc = &Process{Key: key, State: StateNew}
		s.processes[key] = proc
	}
	return proc
}

// Get возвращает процесс и признак существования.
func (s *Store) Get(key string) (*Process, bool) {
	proc, ok := s.processes[key]
	return proc, ok
}

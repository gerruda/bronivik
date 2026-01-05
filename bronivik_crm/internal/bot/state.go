package bot

import "sync"

type bookingStep string

const (
	stepNone        bookingStep = "none"
	stepCabinet     bookingStep = "cabinet"
	stepItem        bookingStep = "item"
	stepDate        bookingStep = "date"
	stepTime        bookingStep = "time"
	stepClientName  bookingStep = "client_name"
	stepClientPhone bookingStep = "client_phone"
	stepConfirm     bookingStep = "confirm"
)

type BookingDraft struct {
	CabinetID   int64
	CabinetName string
	ItemName    string
	Date        string // YYYY-MM-DD
	TimeLabel   string // HH:MM-HH:MM
	ClientName  string
	ClientPhone string
}

type userState struct {
	Step  bookingStep
	Draft BookingDraft
}

type stateStore struct {
	mu sync.Mutex
	m  map[int64]*userState
}

func newStateStore() *stateStore {
	return &stateStore{m: make(map[int64]*userState)}
}

func (s *stateStore) get(userID int64) *userState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.m[userID]
	if st == nil {
		st = &userState{Step: stepNone}
		s.m[userID] = st
	}
	return st
}

func (s *stateStore) reset(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, userID)
}

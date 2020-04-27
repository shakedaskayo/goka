package goka

import (
	"fmt"
	"sync"
)

// State types a state of the Signal
type State int

type waiter struct {
	done     chan struct{}
	state    State
	minState bool
}

// Signal allows synchronization on a state, waiting for that state and checking
// the current state
type Signal struct {
	m                    sync.Mutex
	state                State
	waiters              []*waiter
	stateChangeObservers []*StateChangeObserver
	allowedStates        map[State]bool
}

// NewSignal creates a new Signal based on the states
func NewSignal(states ...State) *Signal {
	s := &Signal{
		allowedStates: make(map[State]bool),
	}
	for _, state := range states {
		s.allowedStates[state] = true
	}

	return s
}

// SetState changes the state of the signal
// and notifies all goroutines waiting for the new state
func (s *Signal) SetState(state State) *Signal {
	s.m.Lock()
	defer s.m.Unlock()
	if !s.allowedStates[state] {
		panic(fmt.Errorf("trying to set illegal state %v", state))
	}

	// set the state and notify all channels waiting for it.
	s.state = state
	var newWaiters []*waiter
	for _, w := range s.waiters {
		if w.state == state || (w.minState && state >= w.state) {
			close(w.done)
			continue
		}
		newWaiters = append(newWaiters, w)
	}
	s.waiters = newWaiters

	// notify the state change observers
	for _, obs := range s.stateChangeObservers {
		obs.c <- struct{}{}
	}

	return s
}

// IsState returns if the signal is in the requested state
func (s *Signal) IsState(state State) bool {
	return s.state == state
}

// State returns the current state
func (s *Signal) State() State {
	return s.state
}

// WaitForStateMin returns a channel that will be closed, when the signal enters passed
// state or higher (states are ints, so we're just comparing ints here)
func (s *Signal) WaitForStateMin(state State) chan struct{} {
	s.m.Lock()
	defer s.m.Unlock()

	w := &waiter{
		done:     make(chan struct{}),
		state:    state,
		minState: true,
	}

	return s.waitForWaiter(state, w)
}

// WaitForState returns a channel that closes when the signal reaches passed
// state.
func (s *Signal) WaitForState(state State) chan struct{} {
	s.m.Lock()
	defer s.m.Unlock()

	w := &waiter{
		done:  make(chan struct{}),
		state: state,
	}

	return s.waitForWaiter(state, w)
}

func (s *Signal) waitForWaiter(state State, w *waiter) chan struct{} {

	// if the signal is currently in that state (or in a higher state if minState is set)
	// then close the waiter immediately
	if curState := s.State(); state == curState || (w.minState && curState >= state) {
		close(w.done)
	} else {
		s.waiters = append(s.waiters, w)
	}

	return w.done
}

// StateChangeObserver wraps a channel that triggers when the signal's state changes
type StateChangeObserver struct {
	c    chan struct{}
	stop func()
	s    *Signal
}

// Stop stops the observer. Its update channel will be closed and
func (s *StateChangeObserver) Stop() {
	s.stop()
}

// C returns the channel to observer state changes
func (s *StateChangeObserver) C() <-chan struct{} {
	return s.c
}

// State returns the current state of the Signal
func (s *StateChangeObserver) State() State {
	return s.s.State()
}

// ObserveStateChange returns a channel that receives state changes.
// Note that the caller must take care of consuming that channel, otherwise the Signal
// will block upon state changes
func (s *Signal) ObserveStateChange() *StateChangeObserver {
	s.m.Lock()
	defer s.m.Unlock()

	observer := &StateChangeObserver{
		c: make(chan struct{}, 1),
		s: s,
	}

	// the stop funtion stops the observer by closing its channel
	// and removing it from the list of observers
	observer.stop = func() {
		s.m.Lock()
		defer s.m.Unlock()

		// iterate over all observers and close *this* one
		for idx, obs := range s.stateChangeObservers {
			if obs == observer {
				copy(s.stateChangeObservers[idx:], s.stateChangeObservers[idx+1:])
				s.stateChangeObservers[len(s.stateChangeObservers)-1] = nil
				s.stateChangeObservers = s.stateChangeObservers[:len(s.stateChangeObservers)-1]
			}
		}
		close(observer.c)
	}

	s.stateChangeObservers = append(s.stateChangeObservers, observer)
	return observer
}

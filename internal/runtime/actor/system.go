package actor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrMailboxFull = errors.New("actor mailbox full")

type SessionHandler interface {
	Handle(ctx context.Context, payload any) (any, error)
	Close() error
}

type SessionFactory func(sessionID string) (SessionHandler, error)

type request struct {
	ctx     context.Context
	payload any
	resp    chan response
}

type response struct {
	value any
	err   error
}

type actorState struct {
	sessionID string
	handler   SessionHandler
	mailbox   chan request
	lastSeen  time.Time
	closed    chan struct{}
}

type System struct {
	mu         sync.Mutex
	actors     map[string]*actorState
	factory    SessionFactory
	mailboxCap int
	idleTTL    time.Duration
	stop       chan struct{}
	wg         sync.WaitGroup
	onStart    func()
	onStop     func()
}

func NewSystem(factory SessionFactory, mailboxCap int, idleTTL time.Duration) *System {
	if mailboxCap <= 0 {
		mailboxCap = 32
	}
	if idleTTL <= 0 {
		idleTTL = 10 * time.Minute
	}
	s := &System{
		actors:     make(map[string]*actorState),
		factory:    factory,
		mailboxCap: mailboxCap,
		idleTTL:    idleTTL,
		stop:       make(chan struct{}),
	}
	s.wg.Add(1)
	go s.reaper()
	return s
}

func (s *System) SetActorHooks(onStart, onStop func()) {
	s.onStart = onStart
	s.onStop = onStop
}

func (s *System) Stop() error {
	close(s.stop)
	s.wg.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, actor := range s.actors {
		close(actor.mailbox)
		<-actor.closed
	}
	s.actors = map[string]*actorState{}
	return nil
}

func (s *System) Submit(ctx context.Context, sessionID string, payload any, wait bool) (any, error) {
	actor, err := s.getOrCreate(sessionID)
	if err != nil {
		return nil, err
	}

	req := request{ctx: ctx, payload: payload}
	if wait {
		req.resp = make(chan response, 1)
	}

	select {
	case actor.mailbox <- req:
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, ErrMailboxFull
	}

	if !wait {
		return nil, nil
	}

	select {
	case res := <-req.resp:
		return res.value, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *System) ActorCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.actors)
}

func (s *System) getOrCreate(sessionID string) (*actorState, error) {
	s.mu.Lock()
	if actor, ok := s.actors[sessionID]; ok {
		actor.lastSeen = time.Now()
		s.mu.Unlock()
		return actor, nil
	}
	handler, err := s.factory(sessionID)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	actor := &actorState{
		sessionID: sessionID,
		handler:   handler,
		mailbox:   make(chan request, s.mailboxCap),
		lastSeen:  time.Now(),
		closed:    make(chan struct{}),
	}
	s.actors[sessionID] = actor
	s.mu.Unlock()

	if s.onStart != nil {
		s.onStart()
	}
	go s.runActor(actor)
	return actor, nil
}

func (s *System) runActor(actor *actorState) {
	defer close(actor.closed)
	defer func() {
		_ = actor.handler.Close()
		if s.onStop != nil {
			s.onStop()
		}
	}()

	for req := range actor.mailbox {
		func() {
			defer func() {
				if rec := recover(); rec != nil && req.resp != nil {
					req.resp <- response{err: fmt.Errorf("actor panic: %v", rec)}
				}
			}()

			value, err := actor.handler.Handle(req.ctx, req.payload)
			if req.resp != nil {
				req.resp <- response{value: value, err: err}
			}
		}()
		s.mu.Lock()
		actor.lastSeen = time.Now()
		s.mu.Unlock()
	}
}

func (s *System) reaper() {
	defer s.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.evictIdle()
		}
	}
}

func (s *System) evictIdle() {
	now := time.Now()
	var candidates []*actorState

	s.mu.Lock()
	for key, actor := range s.actors {
		if now.Sub(actor.lastSeen) > s.idleTTL {
			delete(s.actors, key)
			candidates = append(candidates, actor)
		}
	}
	s.mu.Unlock()

	for _, actor := range candidates {
		close(actor.mailbox)
		<-actor.closed
	}
}

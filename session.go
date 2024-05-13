package webserver

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
)

// Sessions could be
// via Cookies
// Stored in DB
// Stored in memory
type SessionStore interface {
	ParseToken(http.Header) string
	Get(token string) (interface{}, error)
	Save(token string, data interface{}) error
	Delete(token string) error
}
type SessionStoreContext interface {
	GetCtx(token string, ctx context.Context) (interface{}, error)
	SaveCtx(token string, data interface{}, ctx context.Context) error
	DeleteCtx(token string, ctx context.Context) error
}

type InMemorySessionStore[T any] struct {
	Sessions map[string]T
	mu       *sync.RWMutex
}

func NewInMemorySessionStore[T any]() *InMemorySessionStore[T] {
	return &InMemorySessionStore[T]{
		Sessions: make(map[string]T),
		mu:       new(sync.RWMutex),
	}
}

func (store *InMemorySessionStore[T]) ParseToken(header http.Header) string {
	// look for session_token cookie
	// if not present, set to random string
	r := http.Request{Header: header}
	token, err := r.Cookie("session_token")
	if err != nil {
		buf := make([]byte, 12)
		rand.Read(buf)
		return base64.StdEncoding.EncodeToString(buf)
	} else {
		return token.Value
	}
}
func (store *InMemorySessionStore[T]) Get(token string) (interface{}, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.Sessions[token], nil
}
func (store *InMemorySessionStore[T]) Save(token string, data interface{}) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.Sessions[token] = data.(T)
	return nil
}
func (store InMemorySessionStore[T]) Delete(token string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.Sessions, token)
	return nil
}

// Sessionless is a placeholder for anyone who wants to run a sessionless-server
type Sessionless struct{}

func (_ Sessionless) ParseToken(header http.Header) string {
	return ""
}
func (_ Sessionless) Get(token string) (interface{}, error)     { return nil, nil }
func (_ Sessionless) Save(token string, data interface{}) error { return nil }
func (_ Sessionless) Delete(token string) error                 { return nil }

type Session[T any] struct {
	Token string
	Data  *T
	store SessionStore
	req   *Request
}

func (session *Session[T]) load(ctx context.Context) error {
	session.Token = session.store.ParseToken(session.req.Headers)
	data, err := session.store.Get(session.Token)
	if err != nil {
		return err
	}

	if data != nil {
		sd := data.(T)
		session.Data = &sd
	} else {
		session.Data = nil
	}

	return nil
}

func (session *Session[T]) save(ctx context.Context) error {
	if session.Data == nil {
		session.store.Save(session.Token, nil)
	} else {
		session.store.Save(session.Token, *session.Data)
	}
	if session.Token > "" {
		session.req.SetCookie(http.Cookie{
			Name:   "session_token",
			Value:  session.Token,
			MaxAge: (24 * 60 * 60),
		})
	}

	return nil
}

func (session *Session[T]) delete(ctx context.Context) error {
	session.store.Delete(session.Token)
	session.req.SetCookie(http.Cookie{
		Name:   "session_token",
		Value:  "",
		MaxAge: -1,
	})
	session.Data = nil
	return nil
}

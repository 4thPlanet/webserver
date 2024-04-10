package webserver

import (
	"context"
	"io"
	"log"
	"net/http"
	"reflect"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/flate"
	"github.com/klauspost/compress/gzip"
)

type Server[S any] struct {
	sessionStore          SessionStore
	middlewares           []Middleware
	contentTypeInterfaces map[string]reflect.Type
	mux                   *http.ServeMux
}

// How to do generic-less server, with generic sessions (server-level) and request.Body data (route-level)?
// Set up generic sessions (applied to Server), with NewServerWithoutSessions() returning a mocked session provider?
// Still have request.Body problem but is a bit less of an issue..

type Middleware func(req *Request) error
type WebsocketHandler func(req *Request, inFeed <-chan []byte) <-chan []byte
type EventStreamHandler func(req *Request) <-chan EventStreamer

func New[S any](sessionStore SessionStore) *Server[S] {
	s := &Server[S]{
		middlewares:           make([]Middleware, 0),
		sessionStore:          sessionStore,
		contentTypeInterfaces: make(map[string]reflect.Type),
		mux:                   http.NewServeMux(),
	}

	s.RegisterContentTypeInterface("html", (*Htmler)(nil))
	s.RegisterContentTypeInterface("csv", (*Csver)(nil))
	s.RegisterContentTypeInterface("json", (*Jsoner)(nil))

	return s
}

func (s *Server[S]) RegisterContentTypeInterface(contentType string, i interface{}) {
	// i must be an interface
	reflection := reflect.TypeOf(i)
	if reflection.Kind() == reflect.Pointer {
		reflection = reflection.Elem()
	}
	if reflection.Kind() != reflect.Interface {
		panic("type of i is not an interface.")
	}

	// i must have a single method defined
	if reflection.NumMethod() != 1 {
		panic("interface must implement a single method with no arguments returning []byte")
	}

	// That method must accept 0 arguments, and output a single []byte
	fn := reflection.Method(0).Type
	if fn.NumIn() != 0 ||
		fn.NumOut() != 1 ||
		fn.Out(0) != byteSlice {
		panic("interface must implement a single method with no arguments returning []byte")
	}

	s.contentTypeInterfaces[contentType] = reflection
}

func (s *Server[S]) Middleware(mw Middleware) {
	// apply mw on all requests
	s.middlewares = append(s.middlewares, mw)
}

func ApplyErrorHandler[T any, S any](s *Server[S], fn func(req *Request, code int) T) {
	// function to be called when an error is returned
}

func (s *Server[S]) determineResponseInterface(acceptHeader string, implementsMap map[string]bool) reflect.Type {
	// TODO: This is just asking for a panic() to happen...
	acceptedContentTypes := strings.Split(strings.Split(acceptHeader, ";")[0], ",")

	for _, contentType := range acceptedContentTypes {
		// is contentTypeInterfaces[contentType] set?
		if implementsMap[contentType] {
			return s.contentTypeInterfaces[contentType]
		} else {
			parts := strings.Split(contentType, "/")
			if implementsMap[parts[1]] {

				if parts[1] == "*" {
					// text/* or similar
					if implementsMap[parts[0]] {
						return s.contentTypeInterfaces[parts[0]]
					}
				} else {
					// text/html or similar
					return s.contentTypeInterfaces[parts[1]]

				}
			}
		}
	}
	return nil
}

// You're not able to use generics on a method, so going through a public function which accepts the Server object is the least-bad way to get type safety in the handlers.
func ApplyRoute[T any, S any, B any](s *Server[S], Path string, body B, handlers map[Verb]func(req *Request) (T, error)) *Route[B, T] {

	route := &Route[B, T]{
		path:        Path,
		middlewares: make([]Middleware, 0),
		handlers:    handlers,
	}

	implements := map[string]bool{}
	for t, i := range s.contentTypeInterfaces {
		implements[t] = reflect.TypeOf(new(T)).Elem().Implements(i)
	}

	// TODO: If T is an interface then check will have to be performed at run-time (maybe it's an Htmler which is also a Csver)..

	s.mux.HandleFunc(Path, func(w http.ResponseWriter, r *http.Request) {

		req := &Request{
			req:             r,
			Path:            r.URL.Path,
			Headers:         r.Header,
			Cookies:         r.Cookies(),
			Context:         r.Context(),
			ResponseHeaders: w.Header(),
		}
		session := Session[S]{
			store: s.sessionStore,
			req:   req,
		}

		err := session.load(context.TODO())

		if err != nil {
			log.Println("Error loading session:", err)
		}
		req.Session = session.Data

		// before v1.22, Verb can't be included in the Pattern
		req.Verb, err = ParseVerb(r.Method)
		if err != nil {
			// https://stackoverflow.com/questions/72217705/http-response-status-for-unknown-nonexistent-http-method
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		session.req = req

		handler, isset := handlers[req.Verb]
		if !isset {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		responseInterface := s.determineResponseInterface(r.Header.Get("Accept"), implements)

		if responseInterface == nil {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}

		// TODO: Setup location for uploaded files to go
		// TODO: Configurable max upload size
		if err := readBody(req, new(B)); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		for _, mw := range s.middlewares {
			err := mw(req)
			if err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusTeapot)
				return
			}
		}

		for _, mw := range route.middlewares {
			err := mw(req)
			if err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusTeapot)
				return
			}
		}

		// TODO: Special case for text/event-stream (Server-Side Events, T needs to be a channel sending events)
		if false {
			go func(ch <-chan EventStreamer) {
				for evt := range ch {
					b := evt.AsEventStream()
					toWrite := append(eventStreamMessagePrefix, b...)
					toWrite = append(toWrite, '\n')
					w.Write(toWrite)
				}
			}(route.eventStream(req))
			return
		}

		// TODO: Special case for websocket connections (Upgrade: websocket, Connection: Upgrade, Sec-WebSocket-Key and Sec-WebSocket-Version are set)
		if false {

		}

		response, err := handler(req)

		if err != nil {
			w.WriteHeader(400)
			return
		} else {
			b := deliverContentAsInterface(response, responseInterface)

			var writer io.Writer = w

			encodings := strings.Split(r.Header.Get("Accept-Encoding"), ",")
		ENCODINGLOOP:
			for _, encoding := range encodings {
				switch strings.TrimSpace(encoding) {
				case "gzip":
					w.Header().Add("Content-Encoding", "gzip")
					encoding := gzip.NewWriter(w)
					defer encoding.Flush()
					writer = encoding
					break ENCODINGLOOP
				case "deflate":
					w.Header().Add("Content-Encoding", "deflate")
					encoding, _ := flate.NewWriter(w, flate.DefaultCompression)
					defer encoding.Flush()
					writer = encoding
					break ENCODINGLOOP
				case "br":
					w.Header().Add("Content-Encoding", "br")
					encoding := brotli.NewWriter(w)
					defer encoding.Flush()
					writer = encoding
					break ENCODINGLOOP
				case "identity":
					break
					// TODO: case "compress":
					// TODO: case "zstd":

				}
			}

			session.Data = req.Session.(*S)
			err := session.save(context.TODO())

			if err != nil {
				log.Println("Error saving session:", err)
			}

			writer.Write(b)
		}

	})

	return route

}

func (s *Server[S]) Start(addr string) {

	http.ListenAndServe(addr, s.mux)
}

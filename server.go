package webserver

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"mime"
	"mime/multipart"
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

type Request struct {
	req             http.Request
	Session         interface{}
	Verb            Verb
	Path            string
	Headers         http.Header
	Cookies         []*http.Cookie
	Body            interface{} // TODO: generic without breaking Request logic?
	Context         context.Context
	ResponseHeaders http.Header
}

func (req *Request) SetCookie(cookie http.Cookie) {
	req.ResponseHeaders.Set("set-cookie", cookie.String())
}

type Route[B any, T any] struct {
	path        string
	middlewares []Middleware
	websocket   WebsocketHandler
	eventStream EventStreamHandler
	handlers    map[Verb]func(req *Request) (T, error)
}

func (r *Route[B, T]) Middleware(mw Middleware) {
	r.middlewares = append(r.middlewares, mw)
}

func (r *Route[B, T]) Websocket(handler WebsocketHandler) {
	r.websocket = handler
}

var eventStreamMessagePrefix = []byte("data: ")

func (r *Route[B, T]) EventStream(handler EventStreamHandler) {
	r.eventStream = handler
}

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

		// TODO: This is just asking for a panic() to happen...
		acceptHeader := r.Header.Get("Accept")
		acceptedContentTypes := strings.Split(strings.Split(acceptHeader, ";")[0], ",")
		var responseInterface reflect.Type

		for _, contentType := range acceptedContentTypes {
			// is contentTypeInterfaces[contentType] set?
			responseInterface, isset = s.contentTypeInterfaces[contentType]
			if isset && !implements[contentType] {
				isset = false
				responseInterface = nil
			}
			if !isset {
				// split on /, look at second value
				parts := strings.Split(contentType, "/")
				responseInterface, isset = s.contentTypeInterfaces[parts[1]]
				if isset && !implements[parts[1]] {
					isset = false
					responseInterface = nil
				}

				if !isset && parts[1] == "*" {
					responseInterface, isset = s.contentTypeInterfaces[parts[0]]
					if isset && !implements[parts[0]] {
						isset = false
						responseInterface = nil
					}
				}
			}
			if responseInterface != nil {
				break

			}
		}

		if responseInterface == nil {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}

		// TODO: determine ahead of time if B implements the required interfaceDoes it implement interface for content type?
		// TODO: Setup location for uploaded files to go
		// TODO: Configurable max upload size
		bodyRdr := bufio.NewReader(r.Body)
		if _, err := bodyRdr.Peek(1); err == nil {
			body := new(B)

			mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				log.Println("Error reading request content type:", err)
				return
			}
			switch mediaType {
			case "application/x-www-form-urlencoded":
				reqBody, err := io.ReadAll(bodyRdr)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					log.Println("Error reading request body:", err)
					return
				}
				parser, ok := (interface{}(body)).(FormDataParser)
				if ok {
					err := parser.ParseFormData(reqBody)
					if err != nil {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
				}
			case "multipart/form-data":
				rd := multipart.NewReader(bodyRdr, params["boundary"])

				parser, ok := (interface{}(body)).(MultipartFormDataParser)
				if ok {
					err := parser.ParseMultipartFormData(rd)
					if err != nil {
						w.WriteHeader(http.StatusBadRequest)
						return
					}

				}
			case "application/json":
				reqBody, err := io.ReadAll(bodyRdr)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					log.Println("Error reading request body:", err)
					return
				}
				err = json.Unmarshal(reqBody, body)
				if err != nil {
					w.WriteHeader(http.StatusBadRequest)
					log.Println("Error parsing JSON:", err)
					return
				}
			case "text":
				reqBody, err := io.ReadAll(bodyRdr)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					log.Println("Error reading request body:", err)
					return
				}
				parser, ok := (interface{}(body)).(PlainTextParser)
				if ok {
					err := parser.ParsePlainText(reqBody)
					if err != nil {
						log.Println("Error parsing request body:", err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
				}

			default:
				log.Println(r.Header.Get("Content-Type"))

			}
			req.Body = *body
			log.Println("The parsed body is: ", req.Body)
			//			req.Body = reqBody

		}

		// TODO: File uploads

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

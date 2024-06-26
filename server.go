package webserver

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/klauspost/compress/flate"
	"github.com/klauspost/compress/gzip"
)

// This reflection lookup is used in both ApplyErrorHandler as well as ApplyRoute functions
var rdrInterface = reflect.TypeOf((*io.Reader)(nil)).Elem()

type Server[S any] struct {
	Logger                defaultLogger
	SecureConfig          *tls.Config
	MaxPostSize           uint
	sessionStore          SessionStore
	middlewares           []Middleware
	contentTypeInterfaces map[string]reflect.Type
	mux                   *http.ServeMux
	errorHandler          *errorHandler[S]
}

type Middleware func(req *Request) *Error
type WebsocketHandler func(req *Request, inFeed <-chan []byte) <-chan []byte
type EventStreamHandler func(req *Request) <-chan EventStreamer

func New[S any](sessionStore SessionStore) *Server[S] {
	s := &Server[S]{
		Logger:                DefaultLogger,
		MaxPostSize:           10 << 20, // 10MB
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

func (s *Server[S]) determineResponseInterface(acceptHeader string, implementsMap map[string]bool) reflect.Type {

	if len(acceptHeader) == 0 {
		return nil
	}

	offers := []string{}
	for contentType, implements := range implementsMap {
		if implements {
			if strings.Contains(contentType, "/") {
				offers = append(offers, contentType)
			} else {
				offers = append(offers, "text/"+contentType)
			}

		}
	}

	type acceptedContentType struct {
		contentType string
		weight      float64
	}

	acceptHeaderSplit := strings.Split(acceptHeader, ",")

	acceptedContentTypes := make([]acceptedContentType, len(acceptHeaderSplit))
	for idx, contentType := range acceptHeaderSplit {

		acceptedParts := strings.Split(contentType, ";q=")
		acceptedType := acceptedContentType{contentType: acceptedParts[0], weight: 1.0}
		if len(acceptedParts) > 1 {
			weight, err := strconv.ParseFloat(acceptedParts[1], 64)
			if err != nil {
				log.Println("Error parsing weight value:", err)
				continue
			}
			acceptedType.weight = weight
		}
		acceptedContentTypes[idx] = acceptedType
	}

	sort.Slice(acceptedContentTypes, func(i, j int) bool {
		return acceptedContentTypes[i].weight > acceptedContentTypes[j].weight
	})

	for _, contentTypeEntry := range acceptedContentTypes {
		// is contentTypeInterfaces[contentType] set?
		contentType := contentTypeEntry.contentType
		if implementsMap[contentType] {
			return s.contentTypeInterfaces[contentType]
		} else {
			parts := strings.Split(contentType, "/")
			if len(parts) == 1 {
				continue
			}

			if parts[1] == "*" {
				// text/* or similar - need to match on parts[0]
				if implementsMap[parts[0]] {
					return s.contentTypeInterfaces[parts[0]]
				}
			} else {
				if implementsMap[parts[1]] {
					return s.contentTypeInterfaces[parts[1]]
				}
			}
		}
	}
	return nil
}

// You're not able to use generics on a method, so going through a public function which accepts the Server object is the least-bad way to get type safety in the handlers.
func ApplyRoute[T any, S any, B any](s *Server[S], Path string, body B, handlers map[Verb]func(req *Request) (T, *Error)) *Route[B, T] {

	route := &Route[B, T]{
		path:        Path,
		middlewares: make([]Middleware, 0),
		handlers:    handlers,
	}

	implements := map[string]bool{}
	responseType := reflect.TypeOf(new(T)).Elem()
	for t, i := range s.contentTypeInterfaces {
		implements[t] = responseType.Implements(i)
	}

	isReader := reflect.TypeOf(new(T)).Elem().Implements(rdrInterface)

	// TODO: If T is an interface then check will have to be performed at run-time (maybe it's an Htmler which is also a Csver)..

	s.mux.HandleFunc(Path, func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, int64(s.MaxPostSize))
		req := &Request{
			req:             r,
			startTime:       time.Now(),
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

		if err := session.load(context.TODO()); err != nil {
			s.Logger.LogError(req, fmt.Errorf("Error loading session: %v", err))
		}

		runMiddlewares := func() *Error {
			for _, mw := range s.middlewares {
				err := mw(req)
				if err != nil {
					s.errorHandler.Apply(req, *err, w)
					return err
				}
			}

			for _, mw := range route.middlewares {
				err := mw(req)
				if err != nil {
					s.errorHandler.Apply(req, *err, w)
					return err
				}
			}
			return nil
		}

		req.Session = session.Data

		// before v1.22, Verb can't be included in the Pattern
		var verbErr error
		req.Verb, verbErr = ParseVerb(r.Method)
		if verbErr != nil {
			// https://stackoverflow.com/questions/72217705/http-response-status-for-unknown-nonexistent-http-method
			s.errorHandler.Apply(req, Error{Code: http.StatusNotImplemented}, w)
			return
		}
		session.req = req

		handler, isset := handlers[req.Verb]
		if !isset {
			s.errorHandler.Apply(req, Error{Code: http.StatusMethodNotAllowed}, w)
			return
		}

		// TODO: Error if event-stream and not supported on route...
		if r.Header.Get("Accept") == "text/event-stream" && route.eventStream != nil {
			if err := readBody(req, new(B)); err != nil {
				s.Logger.LogError(req, err.Error)
				s.errorHandler.Apply(req, *err, w)
				return
			}

			err := runMiddlewares()
			if err != nil {
				s.errorHandler.Apply(req, *err, w)
				return
			}

			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			// should this be r.Context.Done()?
			events := route.eventStream(req)

			for evt := range events {
				_, err := fmt.Fprintf(w, "data: %s\n\n", evt.AsEventStream())
				if err != nil {
					s.Logger.LogError(req, fmt.Errorf("Error sending event: %v", err))
					break
				}

				w.(http.Flusher).Flush()
			}

			return
		}

		// TODO: Error if upgrade = websocket and not supported on route...
		// TODO: protocol check
		if r.Header.Get("Upgrade") == "websocket" && route.websocket != nil {

			err := runMiddlewares()
			if err != nil {
				s.errorHandler.Apply(req, *err, w)
				return
			}

			var closedConnectionError = &wsutil.ClosedError{}
			in := make(chan []byte)

			conn, _, _, upgradeErr := ws.UpgradeHTTP(r, w)

			if upgradeErr != nil {
				// handle error
				s.Logger.LogError(req, fmt.Errorf("Error upgrading websocket connection! %v", upgradeErr))
				s.errorHandler.Apply(req, Error{Code: http.StatusInternalServerError, Error: upgradeErr}, w)
				return
			}
			ctx, cancel := context.WithCancel(req.Context)
			req.Context = ctx
			out := route.websocket(req, in)

			// TODO: Configurable keepalive?
			go func() {
				defer func() {
					close(in)
					cancel()
				}()

				for {
					payload, err := wsutil.ReadClientText(conn)
					if err != nil {
						// Only really care if it's not a closed connection error...
						if !errors.As(err, closedConnectionError) {
							s.Logger.LogError(req, fmt.Errorf("Error reading websocket payload! %v", err))
						}
						return
					}
					in <- payload
				}
			}()

			for msg := range out {
				// TODO: Allow for Binary vs Text messages
				wsErr := wsutil.WriteServerMessage(conn, ws.OpText, msg)
				if err != nil {
					s.Logger.LogError(req, fmt.Errorf("Error writing message: %v", wsErr))
				}

			}
			return
		}

		responseInterface := s.determineResponseInterface(r.Header.Get("Accept"), implements)

		if responseInterface == nil {
			// if T implements io.Reader then interface will be that
			if !isReader {
				s.errorHandler.Apply(req, Error{Code: http.StatusNotAcceptable}, w)
				return
			}
		}

		if err := readBody(req, new(B)); err != nil {
			s.Logger.LogError(req, err.Error)
			s.errorHandler.Apply(req, *err, w)
			return
		}

		if err := runMiddlewares(); err != nil {
			s.errorHandler.Apply(req, *err, w)
			return
		}

		response, err := handler(req)

		if err != nil {
			s.errorHandler.Apply(req, *err, w)
			return
		} else {

			if req.ResponseCode == 0 {
				req.ResponseCode = 200
			}

			var b []byte
			if responseInterface != nil {
				b = deliverContentAsInterface(response, responseInterface)

			} else {
				rdr := (interface{})(response).(io.Reader)
				var rdrErr error
				b, rdrErr = io.ReadAll(rdr)
				if rdrErr != nil {
					s.Logger.LogError(req, fmt.Errorf("Error reading from Reader: %v", err))
					s.errorHandler.Apply(req, Error{Code: http.StatusInternalServerError}, w)
					return
				}
			}

			session.Data = req.Session.(*S)
			err := session.save(context.TODO())

			if err != nil {
				s.Logger.LogError(req, fmt.Errorf("Error saving session: %v", err))
			}

			req.responseSize = uint(len(b))
			err = writeWithContentEncoding(b, r.Header.Get("Accept-Encoding"), w, req.ResponseCode)
			if err != nil {
				s.Logger.LogError(req, fmt.Errorf("Error writing response content: %v", err))
			}

			s.Logger.LogRequest(req)
		}
	})

	return route

}

func writeWithContentEncoding(content []byte, acceptEncodingHeader string, w http.ResponseWriter, statusCode int) error {
	if len(content) == 0 {
		return nil
	}
	var writer io.Writer = w

	encodings := strings.Split(acceptEncodingHeader, ",")
ENCODINGLOOP:
	for _, encoding := range encodings {
		switch strings.TrimSpace(encoding) {
		case "gzip":
			w.Header().Set("Content-Encoding", "gzip")
			encoding := gzip.NewWriter(w)
			defer encoding.Flush()
			writer = encoding
			break ENCODINGLOOP
		case "deflate":
			w.Header().Set("Content-Encoding", "deflate")
			encoding, _ := flate.NewWriter(w, flate.DefaultCompression)
			defer encoding.Flush()
			writer = encoding
			break ENCODINGLOOP
		case "br":
			w.Header().Set("Content-Encoding", "br")
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
	w.WriteHeader(statusCode)
	_, err := writer.Write(content)
	return err
}

func (s *Server[S]) PublicRoute(dirPath string, pathPrefix string) {
	// Read all subdirectories of dirPath
	// for each directory found, create route at pathPrefix/directory

	if !strings.HasSuffix(pathPrefix, "/") {
		pathPrefix += "/"
	}

	fileHashMap := map[string]string{}
	_ = fileHashMap

	fs.WalkDir(os.DirFS(dirPath), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Fatal(err)
		}

		if path == "." {
			return nil
		}

		if strings.Contains(path, "/") {
			return fs.SkipDir
		}

		ApplyRoute(s, pathPrefix+path+"/", RequestBody{}, map[Verb]func(req *Request) (*bytes.Buffer, *Error){
			GET: func(req *Request) (*bytes.Buffer, *Error) {
				hashCheck := req.Headers.Get("If-None-Match")
				if hashCheck > "" && fileHashMap[req.Path] == hashCheck {
					// return 304
					req.ResponseCode = http.StatusNotModified
					return new(bytes.Buffer), nil
				}

				b, err := os.ReadFile(dirPath + req.Path)
				if err != nil {
					var internalServerError Error
					if errors.Is(err, os.ErrNotExist) {
						s.Logger.LogError(req, fmt.Errorf("FILE NOT FOUND!!"))
						internalServerError = Error{Code: http.StatusNotFound}
					} else {
						internalServerError = Error{Code: http.StatusInternalServerError}
					}

					return nil, &internalServerError
				}

				// Set ETag to md5 of file
				etag := fmt.Sprintf("%x", md5.Sum(b))
				fileHashMap[req.Path] = etag

				// Perform a hash check again, in case fileHashMap simply hadn't been initialized..
				if hashCheck == etag {
					req.ResponseCode = http.StatusNotModified
					return new(bytes.Buffer), nil
				}
				req.ResponseHeaders.Add("ETag", etag)
				return bytes.NewBuffer(b), nil
			},
		})

		return nil
	})

}

// Starts listening on the server
// Returns host and port used (in case 0 is returned), or error if there is one
func (s *Server[S]) Start(addr string) (string, uint, error) {

	// Listen on the specified address.
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return "", 0, err
	}

	if s.SecureConfig != nil {
		l = tls.NewListener(l, s.SecureConfig)
	}

	addrParts := strings.Split(l.Addr().String(), ":")

	go func() {
		defer l.Close()
		server := http.Server{
			Handler: s.mux,
		}
		server.Serve(l)
	}()

	// parse the port to a uint
	var port uint
	for _, r := range addrParts[1] {
		port = (port * 10) + uint(r-'0')
	}

	return addrParts[0], port, nil

}

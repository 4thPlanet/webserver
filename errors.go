package webserver

import (
	"fmt"
	"io"
	"net/http"
	"reflect"
)

// Error Code is an http Status Code >= 400
// Setting to [123]xx could result in unexpected behavior

type Error struct {
	Code  uint
	Error error
}

type errorHandler[S any] struct {
	server     *Server[S]
	fn         reflect.Value
	implements map[string]bool
	isReader   bool
}

func (handler *errorHandler[S]) Apply(req *Request, err Error, w http.ResponseWriter) {
	if handler != nil {
		var (
			buf []byte
		)
		responseInterface := handler.server.determineResponseInterface(req.Headers.Get("Accept"), handler.implements)
		response := handler.fn.Call([]reflect.Value{
			reflect.ValueOf(req),
			reflect.ValueOf(err),
		})[0].Interface()

		if responseInterface != nil {
			buf = deliverContentAsInterface(response, responseInterface)
		} else if handler.isReader {
			var e error
			rdr := response.(io.Reader)

			buf, e = io.ReadAll(rdr)
			if e != nil {
				// well, this is awkward...
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}
		req.responseSize = uint(len(buf))
		e := writeWithContentEncoding(buf, req.Headers.Get("Accept-Encoding"), w, int(err.Code))
		if e != nil {
			handler.server.Logger.LogError(req, fmt.Errorf("Error writing response content: %v", e))
		}
		handler.server.Logger.LogRequest(req)

	}
}

// Specify the function for Server s to call when an error code is returned.
// It can return any type T, it will be delivered under the same rules as any given route's returned data type
func ApplyErrorHandler[T any, S any](s *Server[S], fn func(req *Request, code Error) T) {
	s.errorHandler = &errorHandler[S]{
		fn:         reflect.ValueOf(fn),
		implements: map[string]bool{},
		server:     s,
	}

	s.errorHandler.isReader = reflect.TypeOf(new(T)).Elem().Implements(rdrInterface)

	// function to be called when an error is returned
	// determine what content types T delivers as
	responseType := reflect.TypeOf(new(T)).Elem()
	for t, i := range s.contentTypeInterfaces {
		s.errorHandler.implements[t] = responseType.Implements(i)
	}
}

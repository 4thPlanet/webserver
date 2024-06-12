package webserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"
)

type Request struct {
	req       *http.Request
	startTime time.Time

	Session interface{}
	Verb    Verb
	Path    string
	Headers http.Header
	Cookies []*http.Cookie

	Body            interface{} // TODO: generic without breaking Request logic?
	Context         context.Context
	ResponseHeaders http.Header
	ResponseCode    int
	bodySize        uint
	responseSize    uint
}

func (req *Request) Start() time.Time {
	return req.startTime
}

func (req *Request) SetCookie(cookie http.Cookie) {
	req.ResponseHeaders.Set("set-cookie", cookie.String())
}

func (req *Request) BodySize() uint {
	return req.bodySize
}

func (req *Request) ResponseSize() uint {
	return req.responseSize
}

type bodySizeReader struct {
	Size int
}

func (rdr *bodySizeReader) Write(buf []byte) (n int, err error) {
	rdr.Size += len(buf)
	return len(buf), nil
}

// TODO: determine ahead of time if B implements the required interfaceDoes it implement interface for content type?
func readBody[B any](req *Request, body *B) *Error {
	sizer := new(bodySizeReader)
	defer func() {
		req.bodySize = uint(sizer.Size)
	}()

	teeBody := io.TeeReader(req.req.Body, sizer)
	bodyRdr := bufio.NewReader(teeBody)

	if _, err := bodyRdr.Peek(1); err == nil {
		mediaType, params, err := mime.ParseMediaType(req.Headers.Get("Content-Type"))
		if err != nil {
			return &Error{Code: http.StatusBadRequest, Error: err}
		}
		switch mediaType {
		case "application/x-www-form-urlencoded":
			parser, ok := (interface{}(body)).(FormDataParser)
			if ok {
				err := parser.ParseFormData(bodyRdr)
				if err != nil {
					return err
				}
			}
		case "multipart/form-data":
			parser, ok := (interface{}(body)).(MultipartFormDataParser)
			if ok {
				err := parser.ParseMultipartFormData(bodyRdr, params["boundary"])
				if err != nil {
					return err
				}
			}
		case "application/json":
			reqBody, err := io.ReadAll(bodyRdr)
			if err != nil {
				return &Error{Code: http.StatusInternalServerError, Error: err}
			}
			err = json.Unmarshal(reqBody, body)
			if err != nil {
				return &Error{Code: http.StatusBadRequest, Error: err}
			}
		case "text":
			parser, ok := (interface{}(body)).(PlainTextParser)
			if ok {
				err := parser.ParsePlainText(bodyRdr)
				if err != nil {
					return err
				}
			}

		default:
			return &Error{Code: http.StatusUnsupportedMediaType, Error: fmt.Errorf("Unsupported media type [%s] parsed from header [%s]", mediaType, req.Headers.Get("Content-Type"))}
		}
		req.Body = *body
	}
	return nil
}

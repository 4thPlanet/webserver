package webserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
)

type Request struct {
	req             *http.Request
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

// TODO: determine ahead of time if B implements the required interfaceDoes it implement interface for content type?
// TODO: Setup location for uploaded files to go
// TODO: Configurable max upload size
// TODO: File Uploads
func readBody[B any](req *Request, body *B) error {
	bodyRdr := bufio.NewReader(req.req.Body)
	if _, err := bodyRdr.Peek(1); err == nil {
		mediaType, params, err := mime.ParseMediaType(req.Headers.Get("Content-Type"))
		if err != nil {
			return err
			//			w.WriteHeader(http.StatusBadRequest)
		}
		switch mediaType {
		case "application/x-www-form-urlencoded":
			reqBody, err := io.ReadAll(bodyRdr)
			if err != nil {
				return err
				//				w.WriteHeader(http.StatusInternalServerError)
			}
			parser, ok := (interface{}(body)).(FormDataParser)
			if ok {
				err := parser.ParseFormData(reqBody)
				if err != nil {
					return err
					//					w.WriteHeader(http.StatusBadRequest)
				}
			}
		case "multipart/form-data":
			rd := multipart.NewReader(bodyRdr, params["boundary"])

			parser, ok := (interface{}(body)).(MultipartFormDataParser)
			if ok {
				err := parser.ParseMultipartFormData(rd)
				if err != nil {
					return err
					//			w.WriteHeader(http.StatusBadRequest)
				}

			}
		case "application/json":
			reqBody, err := io.ReadAll(bodyRdr)
			if err != nil {
				return err
				//				w.WriteHeader(http.StatusInternalServerError)
			}
			err = json.Unmarshal(reqBody, body)
			if err != nil {
				return err
				//				w.WriteHeader(http.StatusBadRequest)
			}
		case "text":
			reqBody, err := io.ReadAll(bodyRdr)
			if err != nil {
				return err
				//				w.WriteHeader(http.StatusInternalServerError)
			}
			parser, ok := (interface{}(body)).(PlainTextParser)
			if ok {
				err := parser.ParsePlainText(reqBody)
				if err != nil {
					return err
					//			w.WriteHeader(http.StatusBadRequest)
				}
			}

		default:
			return fmt.Errorf("Unsupported content type %s", req.Headers.Get("Content-Type"))
		}
		req.Body = *body
	}
	return nil
}

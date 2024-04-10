package webserver

import (
	"context"
	"net/http"
)

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

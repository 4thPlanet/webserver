package webserver

type Route[B any, T any] struct {
	path        string
	middlewares []Middleware
	websocket   WebsocketHandler
	eventStream EventStreamHandler
	handlers    map[Verb]func(req *Request) (T, *Error)
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

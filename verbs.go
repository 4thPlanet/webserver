package webserver

import (
	"fmt"
	"strings"
)

type Verb byte

const (
	GET Verb = iota + 1
	POST
	PUT
	DELETE
	HEAD
	OPTIONS
	CONNECT
	TRACE
	PATCH
)

func ParseVerb(in string) (Verb, error) {
	switch strings.ToUpper(in) {
	case "GET":
		return GET, nil
	case "POST":
		return POST, nil
	case "PUT":
		return PUT, nil
	case "DELETE":
		return DELETE, nil
	case "HEAD":
		return HEAD, nil
	case "OPTIONS":
		return OPTIONS, nil
	case "CONNECT":
		return CONNECT, nil
	case "TRACE":
		return TRACE, nil
	case "PATCH":
		return PATCH, nil
	default:
		return 0, fmt.Errorf("Invalid Verb")
	}

}

func (v Verb) String() string {
	switch v {
	case GET:
		return "GET"
	case POST:
		return "POST"
	case PUT:
		return "PUT"
	case DELETE:
		return "DELETE"
	case HEAD:
		return "HEAD"
	case OPTIONS:
		return "OPTIONS"
	case CONNECT:
		return "CONNECT"
	case TRACE:
		return "TRACE"
	case PATCH:
		return "PATCH"
	default:
		panic("Invalid verb")
	}

}

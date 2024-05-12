package webserver

import (
	"fmt"
	"log"
	"time"
)

type Logger interface {
	LogRequest(req *Request)
	LogMessage(req *Request, msg any)
	LogPanic(req *Request, p any)
	LogError(req *Request, err error)
}

type defaultLogger byte

var DefaultLogger defaultLogger

// Default Logger behavior is to use log.Print and fmt.Print* commands
func (logger defaultLogger) LogRequest(req *Request) {
	fmt.Printf("%v %s %s %v %d %d %s\n", time.Now().Format(time.RFC3339), req.Verb, req.Path, req.BodySize(), req.ResponseCode, req.responseSize, time.Since(req.Start()))
}
func (logger defaultLogger) LogMessage(req *Request, msg any) {
	fmt.Printf("%v %s %s %v\n", time.Now().Format(time.RFC3339), req.Verb, req.Path, msg)
}

func (logger defaultLogger) LogPanic(req *Request, p any) {
	log.Printf("panic() processing %s %s: %v", req.Verb, req.Path, p)
}

func (logger defaultLogger) LogError(req *Request, err error) {
	log.Printf("%s %s: %v", req.Verb, req.Path, err)
}

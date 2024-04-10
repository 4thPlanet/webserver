package webserver

import (
	"reflect"
)

type Htmler interface {
	AsHtml() []byte
}
type Csver interface {
	AsCsv() []byte
}
type Jsoner interface {
	AsJson() []byte
}

type EventStreamer interface {
	AsEventStream() []byte
}

var byteSlice = reflect.TypeOf([]byte{})

func deliverContentAsInterface[T any](content T, interfaceReflection reflect.Type) []byte {
	value := reflect.ValueOf(content).Convert(interfaceReflection)
	return value.Method(0).Call(nil)[0].Interface().([]byte)

}

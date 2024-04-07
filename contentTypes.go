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

var contentTypeInterfaces map[string]reflect.Type = make(map[string]reflect.Type)

func init() {

	RegisterContentTypeInterface("html", (*Htmler)(nil))
	RegisterContentTypeInterface("csv", (*Csver)(nil))
	RegisterContentTypeInterface("json", (*Jsoner)(nil))
}

var byteSlice = reflect.TypeOf([]byte{})

func RegisterContentTypeInterface(contentType string, i interface{}) {
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

	contentTypeInterfaces[contentType] = reflection
}

func deliverContentAsInterface[T any](content T, interfaceReflection reflect.Type) []byte {
	value := reflect.ValueOf(content).Convert(interfaceReflection)

	return value.Method(0).Call(nil)[0].Interface().([]byte)

}

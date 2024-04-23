package webserver

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
)

func BenchmarkServer(b *testing.B) {
	message := `Hello, World!`
	messageBytes := []byte(message)

	// setup webserver
	ws := New[Sessionless](Sessionless{})
	ApplyRoute(ws, "/", RequestBody{}, map[Verb]func(req *Request) (*bytes.Buffer, *ErrorCode){
		GET: func(req *Request) (*bytes.Buffer, *ErrorCode) {
			buf := new(bytes.Buffer)
			buf.Write(messageBytes)
			return buf, nil
		},
	})
	_, port, err := ws.Start("localhost:0")

	if err != nil {
		b.Fatalf("Unable to start server: %v", err)
	}

	// setup standard library server..
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		b.Fatalf("Unable to start std server: %v", err)
	}
	addrParts := strings.Split(l.Addr().String(), ":")
	var stdPort uint
	for _, r := range addrParts[1] {
		stdPort = (stdPort * 10) + uint(r-'0')
	}

	go func() {
		defer l.Close()
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Write(messageBytes)
		})
		server := &http.Server{
			Handler: mux,
		}
		server.Serve(l)
	}()

	client := http.Client{
		Transport: &http.Transport{
			DisableCompression: true,
		},
	}

	b.Run("webserver", func(b *testing.B) {
		req, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/", port), nil)
		for i := 0; i < b.N; i++ {
			resp, err := client.Do(req)
			if err != nil {
				b.Errorf("Unable to get root path: %v", err)
			}
			resp.Body.Close()
		}
	})
	b.Run("std-webserver", func(b *testing.B) {
		req, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/", stdPort), nil)
		for i := 0; i < b.N; i++ {
			resp, err := client.Do(req)
			if err != nil {
				b.Errorf("Unable to get root path: %v", err)
			}
			resp.Body.Close()
		}
	})

}

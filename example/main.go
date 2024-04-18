package main

import (
	"bytes"
	"html/template"
	"log"
	"strconv"
	"time"

	"encoding/json"
	"fmt"

	"github.com/4thPlanet/webserver"
)

var tpl *template.Template

func init() {
	var err error
	tpl, err = template.ParseGlob("./templates/*.html")
	if err != nil {
		log.Fatal("Unable to parse templates:", err)
	}
}

func injectContentToMainTemplate(subtemplate string, data any) []byte {
	var buf bytes.Buffer
	var content bytes.Buffer

	err := tpl.ExecuteTemplate(&content, subtemplate, data)
	if err != nil {
		log.Println("Error executing subtemplate:", err)
		return nil
	}

	err = tpl.ExecuteTemplate(&buf, "main", map[string]template.HTML{
		"Content": template.HTML(content.Bytes()),
	})
	if err != nil {
		log.Println("Error executing main template:", err)
		return nil
	}

	return buf.Bytes()

}

type HomePage string

func (page *HomePage) AsHtml() []byte {
	return injectContentToMainTemplate("home", page)
}

func (page *HomePage) AsCsv() []byte {
	return []byte(`Page,URL
Page View Counts,/counts
Server-Sent Event Countdown,/countdown
Web Socket Echo Server,/echo`)
}

func (page *HomePage) AsJson() []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"posted_string": *page,
	})
	return b
}

func (page *HomePage) Anything() []byte {
	return []byte(fmt.Sprintf("%v", *page))
}

// Body which could be used to override Site-Wide PageView count
type FooBody struct {
	SiteTotal uint
}

func (body *FooBody) ParsePlainText(data []byte) error {
	override, err := strconv.ParseUint(string(data), 10, 64)
	if err != nil {
		return err
	}
	body.SiteTotal = uint(override)
	return nil
}

type PageViews struct {
	Session uint
	Total   uint
	Site    uint
}

func (pv *PageViews) AsHtml() []byte {
	return injectContentToMainTemplate("pageviews", pv)
}

func (pv *PageViews) XML() []byte {
	return []byte(fmt.Sprintf(`<Views><Total>%d</Total><Session>%d</Session></Views>`, pv.Total, pv.Session))
}

type Anythinger interface {
	Anything() []byte
}
type XMLer interface {
	XML() []byte
}

type Session struct {
	Count uint
}

type Countdown uint

func (count Countdown) AsHtml() []byte {
	return injectContentToMainTemplate("countdown", count)
}

func (count Countdown) AsEventStream() string {
	return fmt.Sprintf("%d", count)
}

type Echo struct{}

func (echo *Echo) AsHtml() []byte {
	return injectContentToMainTemplate("echo", echo)
}

func main() {

	// Use the simple in-memory session store for sessions
	store := webserver.NewInMemorySessionStore[Session]()

	ws := webserver.New[Session](store)

	// static assets (JS/CSS/images)
	ws.PublicRoute("./public", "/")

	// Tell the webserver how to handle xml content type
	ws.RegisterContentTypeInterface("xml", (*XMLer)(nil))
	// Tell the webserver how to handle */* content type
	ws.RegisterContentTypeInterface("*/*", (*Anythinger)(nil))

	// Set up the homepage path, which accepts GET and POST methods
	postedString := HomePage("There have not been any messages posted to the home page yet.")
	home := webserver.ApplyRoute(ws, "/", webserver.RequestBody{}, map[webserver.Verb]func(req *webserver.Request) (*HomePage, error){
		webserver.GET: func(req *webserver.Request) (*HomePage, error) {
			return &postedString, nil
		},
		webserver.POST: func(req *webserver.Request) (*HomePage, error) {
			return &postedString, nil
		},
	})
	// Apply a middleware for the root path. Route-level middlewares are a great spot for permission checks, user input validation, etc.
	home.Middleware(func(req *webserver.Request) error {
		if req.Body != nil {
			body := req.Body.(webserver.RequestBody)
			if posted, isset := body["posted_string"]; isset {
				postedString = HomePage(posted[0])
			} else {
				postedString = "An invalid string was posted!"
			}
		}

		return nil
	})

	// Set up /views, which accepts GET and PUT requests
	pageCount := uint(0)
	siteCount := uint(0)
	views := webserver.ApplyRoute(ws, "/counts", FooBody{}, map[webserver.Verb]func(req *webserver.Request) (*PageViews, error){
		webserver.GET: func(req *webserver.Request) (*PageViews, error) {
			return &PageViews{
				Total:   pageCount,
				Session: req.Session.(*Session).Count,
				Site:    siteCount,
			}, nil

		},
		webserver.PUT: func(req *webserver.Request) (*PageViews, error) {
			siteCount = req.Body.(FooBody).SiteTotal

			return &PageViews{
				Total:   pageCount,
				Session: req.Session.(*Session).Count,
				Site:    siteCount,
			}, nil
		},
	})
	ws.Middleware(func(req *webserver.Request) error {
		// Keep track of total views of all pages on site
		siteCount++
		return nil
	})
	views.Middleware(func(req *webserver.Request) error {
		pageCount++
		session := req.Session.(*Session)
		session.Count++
		return nil
	})

	// Set up /countdown, which accepts GET requests, as well as text/event-stream requests
	countdown := webserver.ApplyRoute(ws, "/countdown", map[string]interface{}{}, map[webserver.Verb]func(req *webserver.Request) (Countdown, error){
		webserver.GET: func(req *webserver.Request) (Countdown, error) {
			return 20, nil
		},
	})

	// Our SSE will stream a countdown from 20 to 0.
	countdown.EventStream(func(req *webserver.Request) <-chan webserver.EventStreamer {
		count := Countdown(20)
		ch := make(chan webserver.EventStreamer)
		go func() {
			defer close(ch)
			for count > 0 {
				// If the user closes their tab before the countdown completes, we'll have a hanging goroutine - unless we listen for a context.Done() signal
				select {
				case <-req.Context.Done():
					return
				default:
					ch <- count
					count--
					time.Sleep(time.Second)
				}
			}
			ch <- count
		}()
		return ch
	})

	// Set up /echo, which accepts GET requests, as well as Upgrade: websocket requests
	echo := webserver.ApplyRoute(ws, "/echo", map[string]interface{}{}, map[webserver.Verb]func(req *webserver.Request) (*Echo, error){
		webserver.GET: func(req *webserver.Request) (*Echo, error) {
			return &Echo{}, nil
		},
	})
	echo.Websocket(func(req *webserver.Request, inFeed <-chan []byte) <-chan []byte {
		messages := make(chan []byte)
		go func() {
			defer close(messages)
			for in := range inFeed {
				messages <- in
			}
		}()
		return messages
	})

	// Set up a basic server-level middleware which logs the time of each request, along with verb (method) and path.
	ws.Middleware(func(req *webserver.Request) error {
		log.Println(time.Now(), req.Verb, req.Path)
		return nil
	})
	log.Println("Starting server...")
	ws.Start("localhost:8080")
}

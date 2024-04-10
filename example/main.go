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

type BAR string

func (bar *BAR) AsHtml() []byte {
	// Execute main
	// Pass in bar as Content
	var buf bytes.Buffer
	var content bytes.Buffer

	err := tpl.ExecuteTemplate(&content, "bar", bar)
	if err != nil {
		log.Println("Error executing bar template: ", err)
		return nil
	}

	err = tpl.ExecuteTemplate(&buf, "main", map[string]interface{}{
		"Content": template.HTML(content.Bytes()),
	})
	if err != nil {
		log.Println("Error executing main template:", err)
		return nil
	}
	return buf.Bytes()
}
func (bar *BAR) AsCsv() []byte {
	return []byte(fmt.Sprintf("COL1\n%s", *bar))
}
func (bar *BAR) AsJson() []byte {
	b, _ := json.Marshal(bar)
	return b
}
func (bar *BAR) Anything() []byte {
	return []byte(fmt.Sprintf("%v", *bar))
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
}

func (pv *PageViews) AsHtml() []byte {
	// Execute main
	// Pass in bar as Content
	var buf bytes.Buffer
	var content bytes.Buffer

	err := tpl.ExecuteTemplate(&content, "pageviews", pv)
	if err != nil {
		log.Println("Error executing bar template: ", err)
		return nil
	}

	err = tpl.ExecuteTemplate(&buf, "main", map[string]interface{}{
		"Content": template.HTML(content.Bytes()),
	})
	if err != nil {
		log.Println("Error executing main template:", err)
		return nil
	}
	return buf.Bytes()
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

func main() {

	// Use the simple in-memory session store for sessions
	store := webserver.NewInMemorySessionStore[Session]()

	ws := webserver.New[Session](store)

	// Tell the webserver how to handle xml content type
	ws.RegisterContentTypeInterface("xml", (*XMLer)(nil))
	// Tell the webserver how to handle */* content type
	ws.RegisterContentTypeInterface("*/*", (*Anythinger)(nil))

	viewCount := uint(0)

	// Describe the root path, which accepts GET and POST methods. They will return an object of type BAR, which implements Htmler, Csver, Jsoner, and Anythinger (but not XML)
	root := webserver.ApplyRoute(ws, "/", webserver.RequestBody{}, map[webserver.Verb]func(req *webserver.Request) (*BAR, error){
		webserver.GET: func(req *webserver.Request) (*BAR, error) {
			b := BAR("LOREMIPSUM")
			return &b, nil
		},
		webserver.POST: func(req *webserver.Request) (*BAR, error) {
			b := BAR("THE QUICK BROWN FOX JUMPS OVER THE LAZY DOG.")
			return &b, nil
		},
	})

	// Apply a middleware to the root path. Route-level middlewares are a great spot for permission checks, user input validation, etc
	root.Middleware(func(req *webserver.Request) error {
		log.Println("THE ROOT PATH HAS BEEN CALLED")
		return nil
	})

	// Setup a path at /foo, which accepts GET and PUT methods. They will return an object of type PageViews, which implement Htmler and XML interfaces (not Csver or Jsoner).
	webserver.ApplyRoute(ws, "/foo", FooBody{}, map[webserver.Verb]func(req *webserver.Request) (*PageViews, error){
		webserver.GET: func(req *webserver.Request) (*PageViews, error) {

			viewCount++
			session := req.Session.(*Session)
			session.Count++

			return &PageViews{
				Total:   viewCount,
				Session: session.Count,
			}, nil

		},
		webserver.PUT: func(req *webserver.Request) (*PageViews, error) {
			viewCount = req.Body.(FooBody).SiteTotal

			return &PageViews{
				Total:   viewCount,
				Session: req.Session.(*Session).Count,
			}, nil
		},
	})

	// Set up a basic server-level middleware which logs the time of each request, along with verb (method) and path.
	ws.Middleware(func(req *webserver.Request) error {
		log.Println(time.Now(), req.Verb, req.Path)
		return nil
	})
	log.Println("Starting server...")
	ws.Start("localhost:8080")
}

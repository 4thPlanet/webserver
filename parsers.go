package webserver

import (
	"mime/multipart"
	"net/url"
)

type FormDataParser interface {
	ParseFormData([]byte) error
}

type MultipartFormDataParser interface {
	ParseMultipartFormData(*multipart.Reader) error
}

type PlainTextParser interface {
	ParsePlainText([]byte) error
}

type RequestBody url.Values

func (body *RequestBody) ParseFormData(data []byte) error {
	values, err := url.ParseQuery(string(data))
	if err != nil {
		return err
	}
	*body = RequestBody(values)
	return nil

}

func (body *RequestBody) ParseMultipartFormData(r *multipart.Reader) error {
	form, err := r.ReadForm(1024 * 1024 * 10)
	if err != nil {
		return err
	}
	*body = RequestBody(form.Value)
	// Nothing is done with uploaded files..
	return nil
}

package webserver

import (
	"io"
	"mime/multipart"
	"net/url"
)

type FormDataParser interface {
	ParseFormData(io.Reader) error
}

type MultipartFormDataParser interface {
	ParseMultipartFormData(io.Reader, string) error
}

type PlainTextParser interface {
	ParsePlainText(io.Reader) error
}

type RequestBody struct {
	url.Values
	Files map[string][]multipart.File
}

func (body *RequestBody) ParseFormData(rdr io.Reader) error {
	data, err := io.ReadAll(rdr)
	if err != nil {
		return err
	}

	values, err := url.ParseQuery(string(data))
	if err != nil {
		return err
	}
	body.Values = values
	return nil

}

func (body *RequestBody) ParseMultipartFormData(rdr io.Reader, boundary string) error {
	mpr := multipart.NewReader(rdr, boundary)
	form, err := mpr.ReadForm(1024 * 1024 * 10)
	if err != nil {
		return err
	}
	body.Values = form.Value
	body.Files = make(map[string][]multipart.File)
	for key, headers := range form.File {
		body.Files[key] = make([]multipart.File, len(headers))
		for fdx, header := range headers {
			file, err := header.Open()
			if err != nil {
				return err
			}

			// This will need to be closed after being read.
			body.Files[key][fdx] = file
		}
	}
	return nil
}

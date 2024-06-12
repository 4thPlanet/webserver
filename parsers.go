package webserver

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
)

type FormDataParser interface {
	ParseFormData(io.Reader) *Error
}

type MultipartFormDataParser interface {
	ParseMultipartFormData(io.Reader, string) *Error
}

type PlainTextParser interface {
	ParsePlainText(io.Reader) *Error
}

type RequestBody struct {
	url.Values
	Files map[string][]multipart.File
}

func (body *RequestBody) ParseFormData(rdr io.Reader) *Error {
	data, err := io.ReadAll(rdr)
	if err != nil {
		return &Error{Code: http.StatusInternalServerError, Error: err}
	}

	values, err := url.ParseQuery(string(data))
	if err != nil {
		return &Error{Code: http.StatusBadRequest, Error: err}
	}
	body.Values = values
	return nil

}

func (body *RequestBody) ParseMultipartFormData(rdr io.Reader, boundary string) *Error {
	mpr := multipart.NewReader(rdr, boundary)
	form, err := mpr.ReadForm(1024 * 1024 * 10)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return &Error{Code: http.StatusRequestEntityTooLarge, Error: err}
		} else {
			return &Error{Code: http.StatusBadRequest, Error: err}
		}
	}
	body.Values = form.Value
	body.Files = make(map[string][]multipart.File)
	for key, headers := range form.File {
		body.Files[key] = make([]multipart.File, len(headers))
		for fdx, header := range headers {
			file, err := header.Open()
			if err != nil {
				return &Error{Code: http.StatusInternalServerError, Error: err}
			}

			// This will need to be closed after being read.
			body.Files[key][fdx] = file
		}
	}
	return nil
}

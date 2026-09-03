package middleware

// Xss is an in-repo port of github.com/araujo88/gin-gonic-xss-middleware
// (itself derived from github.com/dvwright/xss-mw), which only ships a Gin
// handler. The behaviour is unchanged: request payloads are sanitized with
// bluemonday's StrictPolicy before handlers see them.
//
// It is applied on GET, POST, PUT, and PATCH requests only, and supports three
// request types:
//
//   - JSON requests    - Content-Type application/json
//   - Form Encoded     - Content-Type application/x-www-form-urlencoded
//   - Multipart Form   - Content-Type multipart/form-data
//
// The "password" field is never sanitized.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/microcosm-cc/bluemonday"
)

// xssMw holds the sanitizer options. FieldsToSkip lists fields that are passed
// through unfiltered; "password" is always added by Xss.
type xssMw struct {
	FieldsToSkip []string
	policy       *bluemonday.Policy
}

func Xss() echo.MiddlewareFunc {
	mw := &xssMw{
		FieldsToSkip: []string{"password"},
		policy:       bluemonday.StrictPolicy(),
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if err := mw.xssRemove(c); err != nil {
				// Same as the Gin middleware: report and stop the chain.
				fmt.Printf("%v", err)
				return nil
			}
			return next(c)
		}
	}
}

// xssRemove processes the request body (or query string), removing html.
// Headers and other parts of the request are passed through unaltered.
func (mw *xssMw) xssRemove(c echo.Context) error {
	req := c.Request()
	reqMethod := req.Method

	ctHdr := req.Header.Get("Content-Type")
	ctLen, _ := strconv.Atoi(req.Header.Get("Content-Length"))

	if reqMethod == http.MethodPost || reqMethod == http.MethodPut || reqMethod == http.MethodPatch {
		switch {
		case ctLen > 1 && ctHdr == "application/json":
			return mw.handleJSON(c)
		case ctHdr == "application/x-www-form-urlencoded":
			return mw.handleXFormEncoded(c)
		case strings.Contains(ctHdr, "multipart/form-data"):
			return mw.handleMultiPartFormData(c, ctHdr)
		}
	} else if reqMethod == http.MethodGet {
		return mw.handleGETRequest(c)
	}
	// If here, all should be well or nothing was actually done,
	// either way return happily.
	return nil
}

// handleGETRequest sanitizes the query string.
func (mw *xssMw) handleGETRequest(c echo.Context) error {
	req := c.Request()
	queryParams := req.URL.Query()
	fieldToSkip := map[string]bool{}
	for _, fts := range mw.FieldsToSkip {
		fieldToSkip[fts] = true
	}
	for key, items := range queryParams {
		if fieldToSkip[key] {
			continue
		}
		queryParams.Del(key)
		for _, item := range items {
			queryParams.Set(key, mw.policy.Sanitize(item))
		}
	}
	req.URL.RawQuery = queryParams.Encode()
	return nil
}

// handleXFormEncoded handles Content-Type "application/x-www-form-urlencoded".
// It has been tested with basic param=value form fields only, not on
// file/data uploads.
func (mw *xssMw) handleXFormEncoded(c echo.Context) error {
	req := c.Request()
	if req.Body == nil {
		return nil
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(req.Body); err != nil {
		return err
	}

	m, uerr := url.ParseQuery(buf.String())
	if uerr != nil {
		return uerr
	}

	var bq bytes.Buffer
	for k, v := range m {
		bq.WriteString(k)
		bq.WriteByte('=')

		// do fields to skip
		fndFld := false
		for _, fts := range mw.FieldsToSkip {
			if k == fts {
				// don't sanitize these fields
				bq.WriteString(url.QueryEscape(v[0]))
				fndFld = true
				break
			}
		}
		if !fndFld {
			bq.WriteString(url.QueryEscape(mw.policy.Sanitize(v[0])))
		}
		bq.WriteByte('&')
	}

	if bq.Len() > 1 {
		bq.Truncate(bq.Len() - 1) // remove last '&'
		req.Body = io.NopCloser(bytes.NewBufferString(bq.String()))
	} else {
		req.Body = io.NopCloser(bytes.NewBuffer(buf.Bytes()))
	}

	return nil
}

// handleMultiPartFormData handles Content-Type "multipart/form-data". File
// parts are passed through unsanitized (defaulting to application/octet-stream
// when the part carries no Content-Type); the form-data name "password" is
// skipped as well.
func (mw *xssMw) handleMultiPartFormData(c echo.Context, ctHdr string) error {
	req := c.Request()
	boundary := ctHdr[strings.Index(ctHdr, "boundary=")+9:]

	reader := multipart.NewReader(req.Body, boundary)

	var multiPrtFrm bytes.Buffer
	// unknown, so make up some param limit - 100 max should be enough
	for i := 0; i < 100; i++ {
		part, err := reader.NextPart()
		if err != nil {
			break
		}

		var buf bytes.Buffer
		n, err := io.Copy(&buf, part)
		if err != nil {
			return err
		}
		if n <= 0 {
			return errors.New("error recreating Multipart form Request")
		}
		multiPrtFrm.WriteString(`--` + boundary + "\r\n")
		// don't sanitize file content
		if part.FileName() != "" {
			fn := part.FileName()
			mtype := part.Header.Get("Content-Type")
			multiPrtFrm.WriteString(`Content-Disposition: form-data; name="` + part.FormName() + "\"; ")
			multiPrtFrm.WriteString(`filename="` + fn + "\";\r\n")
			// default to application/octet-stream
			if mtype == "" {
				mtype = `application/octet-stream`
			}
			multiPrtFrm.WriteString(`Content-Type: ` + mtype + "\r\n\r\n")
			multiPrtFrm.WriteString(buf.String() + "\r\n")
		} else {
			multiPrtFrm.WriteString(`Content-Disposition: form-data; name="` + part.FormName() + "\";\r\n\r\n")
			if part.FormName() == "password" {
				multiPrtFrm.WriteString(buf.String() + "\r\n")
			} else {
				multiPrtFrm.WriteString(mw.policy.Sanitize(buf.String()) + "\r\n")
			}
		}
	}
	multiPrtFrm.WriteString("--" + boundary + "--\r\n")

	req.Body = io.NopCloser(bytes.NewBuffer(multiPrtFrm.Bytes()))

	return nil
}

// handleJSON handles Content-Type "application/json", covering plain
// key/value objects, values holding lists, arrays of records, and nested
// records.
func (mw *xssMw) handleJSON(c echo.Context) error {
	req := c.Request()
	jsonBod, err := decodeJSON(req.Body)
	if err != nil {
		return err
	}

	buff, err := mw.jsonToStringMap(bytes.Buffer{}, jsonBod)
	if err != nil {
		return err
	}

	if err := setRequestBodyJSON(c, buff); err != nil {
		return errors.New("set request.body error")
	}
	return nil
}

func decodeJSON(content io.Reader) (any, error) {
	var jsonBod any
	d := json.NewDecoder(content)
	d.UseNumber()
	if err := d.Decode(&jsonBod); err != nil {
		return nil, err
	}
	return jsonBod, nil
}

func (mw *xssMw) jsonToStringMap(buff bytes.Buffer, jsonBod any) (bytes.Buffer, error) {
	switch jbt := jsonBod.(type) {
	case map[string]any:
		var sbuff bytes.Buffer
		return mw.constructJSON(jbt, sbuff), nil
	case []any:
		var multiRec bytes.Buffer
		multiRec.WriteByte('[')
		for _, n := range jbt {
			xmj, ok := n.(map[string]any)
			if !ok {
				return bytes.Buffer{}, errors.New("unknown content type received")
			}
			var sbuff bytes.Buffer
			buff = mw.constructJSON(xmj, sbuff)
			multiRec.WriteString(buff.String())
			multiRec.WriteByte(',')
		}
		multiRec.Truncate(multiRec.Len() - 1) // remove last ','
		multiRec.WriteByte(']')
		return multiRec, nil
	default:
		return bytes.Buffer{}, errors.New("unknown content type received")
	}
}

// setRequestBodyJSON re-sets the http request body from the processed buffer.
func setRequestBodyJSON(c echo.Context, buff bytes.Buffer) error {
	bodOut := buff.String()

	enc := json.NewEncoder(io.Discard)
	if merr := enc.Encode(&bodOut); merr != nil {
		return merr
	}

	c.Request().Body = io.NopCloser(bytes.NewBufferString(bodOut))
	return nil
}

// constructJSON de-constructs the http request body, removes undesirable
// content, and keeps the good content to construct the cleaned body.
func (mw *xssMw) constructJSON(xmj map[string]any, buff bytes.Buffer) bytes.Buffer {
	buff.WriteByte('{')

	for k, v := range xmj {
		buff.WriteByte('"')
		buff.WriteString(k)
		buff.WriteByte('"')
		buff.WriteByte(':')

		// do fields to skip
		fndFld := false
		for _, fts := range mw.FieldsToSkip {
			if k == fts {
				buff.WriteString(fmt.Sprintf("%q", v))
				buff.WriteByte(',')
				fndFld = true
				break
			}
		}
		if fndFld {
			continue
		}

		var b bytes.Buffer
		apndBuff := mw.buildJSONApplyPolicy(v, b)
		buff.WriteString(apndBuff.String())
	}
	buff.Truncate(buff.Len() - 1) // remove last ','
	buff.WriteByte('}')

	return buff
}

func (mw *xssMw) buildJSONApplyPolicy(interf any, buff bytes.Buffer) bytes.Buffer {
	switch v := interf.(type) {
	case map[string]any:
		var sbuff bytes.Buffer
		scnd := mw.constructJSON(v, sbuff)
		buff.WriteString(scnd.String())
		buff.WriteByte(',')
	case []any:
		b := mw.unravelSlice(v)
		buff.WriteString(b.String())
		buff.WriteByte(',')
	case json.Number:
		buff.WriteString(mw.policy.Sanitize(fmt.Sprintf("%v", v)))
		buff.WriteByte(',')
	case string:
		buff.WriteString(fmt.Sprintf("%q", mw.policy.Sanitize(v)))
		buff.WriteByte(',')
	case float64:
		buff.WriteString(mw.policy.Sanitize(strconv.FormatFloat(v, 'g', 0, 64)))
		buff.WriteByte(',')
	default:
		if v == nil {
			buff.WriteString("null")
			buff.WriteByte(',')
		} else {
			buff.WriteString(mw.policy.Sanitize(fmt.Sprintf("%v", v)))
			buff.WriteByte(',')
		}
	}
	return buff
}

func (mw *xssMw) unravelSlice(slce []any) bytes.Buffer {
	var buff bytes.Buffer
	buff.WriteByte('[')
	for _, n := range slce {
		switch nn := n.(type) {
		case map[string]any:
			var sbuff bytes.Buffer
			scnd := mw.constructJSON(nn, sbuff)
			buff.WriteString(scnd.String())
			buff.WriteByte(',')
		case string:
			buff.WriteString(fmt.Sprintf("%q", mw.policy.Sanitize(nn)))
			buff.WriteByte(',')
		}
	}
	buff.Truncate(buff.Len() - 1) // remove last ','
	buff.WriteByte(']')
	return buff
}

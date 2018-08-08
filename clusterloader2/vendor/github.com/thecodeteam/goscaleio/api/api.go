package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	types "github.com/thecodeteam/goscaleio/types/v1"
)

const (
	HeaderKeyAccept                       = "Accept"
	HeaderKeyContentType                  = "Content-Type"
	HeaderValContentTypeJSON              = "application/json"
	headerValContentTypeBinaryOctetStream = "binary/octet-stream"
)

var (
	errNewClient = errors.New("missing endpoint")
	errSysCerts  = errors.New("Unable to initialize cert pool from system")
)

// Client is an API client.
type Client interface {

	// Do sends an HTTP request to the API.
	Do(
		ctx context.Context,
		method, path string,
		body, resp interface{}) error

	// DoWithHeaders sends an HTTP request to the API.
	DoWithHeaders(
		ctx context.Context,
		method, path string,
		headers map[string]string,
		body, resp interface{}) error

	// DoandGetREsponseBody sends an HTTP reqeust to the API and returns
	// the raw response body
	DoAndGetResponseBody(
		ctx context.Context,
		method, path string,
		headers map[string]string,
		body interface{}) (*http.Response, error)

	// Get sends an HTTP request using the GET method to the API.
	Get(
		ctx context.Context,
		path string,
		headers map[string]string,
		resp interface{}) error

	// Post sends an HTTP request using the POST method to the API.
	Post(
		ctx context.Context,
		path string,
		headers map[string]string,
		body, resp interface{}) error

	// Put sends an HTTP request using the PUT method to the API.
	Put(
		ctx context.Context,
		path string,
		headers map[string]string,
		body, resp interface{}) error

	// Delete sends an HTTP request using the DELETE method to the API.
	Delete(
		ctx context.Context,
		path string,
		headers map[string]string,
		resp interface{}) error

	// SetToken sets the Auth token for the HTTP client
	SetToken(token string)

	// GetToken gets the Auth token for the HTTP client
	GetToken() string

	// ParseJSONError parses the JSON in r into an error object
	ParseJSONError(r *http.Response) error
}

type client struct {
	http     *http.Client
	host     string
	token    string
	showHTTP bool
	debug    bool
}

// ClientOptions are options for the API client.
type ClientOptions struct {
	// Insecure is a flag that indicates whether or not to supress SSL errors.
	Insecure bool

	// UseCerts is a flag that indicates whether system certs should be loaded
	UseCerts bool

	// Timeout specifies a time limit for requests made by this client.
	Timeout time.Duration

	// ShowHTTP is a flag that indicates whether or not HTTP requests and
	// responses should be logged to stdout
	ShowHTTP bool
}

// New returns a new API client.
func New(
	ctx context.Context,
	host string,
	opts ClientOptions,
	debug bool) (Client, error) {

	if host == "" {
		return nil, errNewClient
	}

	host = strings.Replace(host, "/api", "", 1)

	c := &client{
		http: &http.Client{},
		host: host,
	}

	if opts.Timeout != 0 {
		c.http.Timeout = opts.Timeout
	}

	if opts.Insecure {
		c.http.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
	}

	if opts.UseCerts {
		pool, err := x509.SystemCertPool()
		if err != nil {
			return nil, errSysCerts
		}
		c.http.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:            pool,
				InsecureSkipVerify: opts.Insecure,
			},
		}
	}

	if opts.ShowHTTP {
		c.showHTTP = true
	}

	c.debug = debug

	return c, nil
}

func (c *client) Get(
	ctx context.Context,
	path string,
	headers map[string]string,
	resp interface{}) error {

	return c.DoWithHeaders(
		ctx, http.MethodGet, path, headers, nil, resp)
}

func (c *client) Post(
	ctx context.Context,
	path string,
	headers map[string]string,
	body, resp interface{}) error {

	return c.DoWithHeaders(
		ctx, http.MethodPost, path, headers, body, resp)
}

func (c *client) Put(
	ctx context.Context,
	path string,
	headers map[string]string,
	body, resp interface{}) error {

	return c.DoWithHeaders(
		ctx, http.MethodPut, path, headers, body, resp)
}

func (c *client) Delete(
	ctx context.Context,
	path string,
	headers map[string]string,
	resp interface{}) error {

	return c.DoWithHeaders(
		ctx, http.MethodDelete, path, headers, nil, resp)
}

func (c *client) Do(
	ctx context.Context,
	method, path string,
	body, resp interface{}) error {

	return c.DoWithHeaders(ctx, method, path, nil, body, resp)
}

func beginsWithSlash(s string) bool {
	return s[0] == '/'
}

func endsWithSlash(s string) bool {
	return s[len(s)-1] == '/'
}

func (c *client) DoWithHeaders(
	ctx context.Context,
	method, uri string,
	headers map[string]string,
	body, resp interface{}) error {

	res, err := c.DoAndGetResponseBody(
		ctx, method, uri, headers, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	// parse the response
	switch {
	case res == nil:
		return nil
	case res.StatusCode >= 200 && res.StatusCode <= 299:
		if resp == nil {
			return nil
		}
		dec := json.NewDecoder(res.Body)
		if err = dec.Decode(resp); err != nil && err != io.EOF {
			c.doLog(log.WithError(err).Error,
				fmt.Sprintf("Unable to decode response into %+v",
					resp))
			return err
		}
	default:
		return c.ParseJSONError(res)
	}

	return nil
}

func (c *client) DoAndGetResponseBody(
	ctx context.Context,
	method, uri string,
	headers map[string]string,
	body interface{}) (*http.Response, error) {

	var (
		err                error
		req                *http.Request
		res                *http.Response
		ubf                = &bytes.Buffer{}
		luri               = len(uri)
		hostEndsWithSlash  = endsWithSlash(c.host)
		uriBeginsWithSlash = beginsWithSlash(uri)
	)

	ubf.WriteString(c.host)

	if !hostEndsWithSlash && (luri > 0) {
		ubf.WriteString("/")
	}

	if luri > 0 {
		if uriBeginsWithSlash {
			ubf.WriteString(uri[1:])
		} else {
			ubf.WriteString(uri)
		}
	}

	u, err := url.Parse(ubf.String())
	if err != nil {
		return nil, err
	}

	var isContentTypeSet bool

	// marshal the message body (assumes json format)
	if r, ok := body.(io.ReadCloser); ok {
		req, err = http.NewRequest(method, u.String(), r)
		defer r.Close()
		if v, ok := headers[HeaderKeyContentType]; ok {
			req.Header.Set(HeaderKeyContentType, v)
		} else {
			req.Header.Set(
				HeaderKeyContentType, headerValContentTypeBinaryOctetStream)
		}
		isContentTypeSet = true
	} else if body != nil {
		buf := &bytes.Buffer{}
		enc := json.NewEncoder(buf)
		if err = enc.Encode(body); err != nil {
			return nil, err
		}
		req, err = http.NewRequest(method, u.String(), buf)
		if v, ok := headers[HeaderKeyContentType]; ok {
			req.Header.Set(HeaderKeyContentType, v)
		} else {
			req.Header.Set(HeaderKeyContentType, HeaderValContentTypeJSON)
		}
		isContentTypeSet = true
	} else {
		req, err = http.NewRequest(method, u.String(), nil)
	}

	if err != nil {
		return nil, err
	}

	if !isContentTypeSet {
		isContentTypeSet = req.Header.Get(HeaderKeyContentType) != ""
	}

	// add headers to the request
	for header, value := range headers {
		if header == HeaderKeyContentType && isContentTypeSet {
			continue
		}
		req.Header.Add(header, value)
	}

	// set the auth token
	if c.token != "" {
		req.SetBasicAuth("", c.token)
	}

	if c.showHTTP {
		logRequest(ctx, req, c.doLog)
	}

	// send the request
	req = req.WithContext(ctx)
	if res, err = c.http.Do(req); err != nil {
		return nil, err
	}

	if c.showHTTP {
		logResponse(ctx, res, c.doLog)
	}

	return res, err
}

func (c *client) SetToken(token string) {
	c.token = token
}

func (c *client) GetToken() string {
	return c.token
}

func (c *client) ParseJSONError(r *http.Response) error {

	jsonError := &types.Error{}
	if err := json.NewDecoder(r.Body).Decode(jsonError); err != nil {
		return err
	}

	jsonError.HTTPStatusCode = r.StatusCode
	if jsonError.Message == "" {
		jsonError.Message = r.Status
	}

	return jsonError
}

func (c *client) doLog(
	l func(args ...interface{}),
	msg string) {

	if c.debug {
		l(msg)
	}
}

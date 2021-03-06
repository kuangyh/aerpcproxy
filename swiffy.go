// Package swiffy exposes gRPC style handler function as HTTP handler that can directly handles
// HTTP requests from Web apps using plain JSON.
//
// gRPC style handler is like f(context, *requestProto) (*responseProto, error)
// From Web app, it can be called as a POST request like /api/foo?method=Bar&format=json and the body is
// simply JSON that can be handled by github.com/golang/protobuf/jsonpb
// For such request, the response will be Status 200 and the plain JSON object as result, or
// any HTTP status code for error conditions.
package swiffy

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
)

// WithHTTPStatus interface can report an HTTP StatusCode the object associated with.
// Often used with an error that needed to be coresponded to a certain HTTP Code
type WithHTTPStatus interface {
	HTTPStatus() int
}

// WithMessage interface can report structured data for error.
// If error returned from handler implements this interface, we return encoded result of
// Message() instead of plain text by String()
type WithMessage interface {
	Message() interface{}
}

type errorWith struct {
	status  int
	text    string
	message interface{}
}

func (e *errorWith) Error() string {
	if e.text == "" {
		return http.StatusText(e.status)
	}
	return e.text
}

func (e *errorWith) HTTPStatus() int {
	return e.status
}

func (e *errorWith) Message() interface{} {
	return e.message
}

// Error returns an error with corresponding HTTP status code, when text
// emtpy, the default HTTP status text will be used.
func Error(status int, text string, message interface{}) error {
	return &errorWith{
		status:  status,
		text:    text,
		message: message,
	}
}

// Handler describes generalize form of gRPC style functions swiffy can serve.
// The actual handler provided to NewServiceHandler can use any types that conforms to encoder/decoder
type Handler func(ctx context.Context, req interface{}) (res interface{}, err error)

// Middleware wraps a handler and do its processing before or after calling underliring handler.
type Middleware func(handler Handler) Handler

// RequestDecoder decodes src into dst.
type RequestDecoder func(dst interface{}, src []byte, format string) error

// ResponseEncoder writes encoded result of src to w.
type ResponseEncoder func(w http.ResponseWriter, status int, src interface{}, format string) error

// Options contains options like encoder/decoder.
type Options struct {
	RequestDecoder  RequestDecoder
	ResponseEncoder ResponseEncoder
	Middleware      Middleware
}

type methodHandler struct {
	// The backend function to call
	backend Handler
	reqType reflect.Type
	decoder RequestDecoder
	encoder ResponseEncoder
}

func newMethodHandler(fn interface{}, opt *Options) *methodHandler {
	fnt := reflect.TypeOf(fn)
	if fnt.Kind() != reflect.Func {
		panic("fn is not a function")
	}
	ctxType := reflect.TypeOf((*context.Context)(nil)).Elem()
	errType := reflect.TypeOf((*error)(nil)).Elem()
	switch {
	case fnt.NumIn() != 2,
		fnt.NumOut() != 2,
		!fnt.In(0).Implements(ctxType),
		// To allow create instance of input.
		fnt.In(1).Kind() != reflect.Ptr,
		fnt.Out(1) != errType:
		panic("fn should be like func(context.Context, *requestProto) (*responesProto, error)")
	}
	fnv := reflect.ValueOf(fn)
	bh := func(ctx context.Context, req interface{}) (interface{}, error) {
		ret := fnv.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(req)})
		res := ret[0].Interface()
		err, _ := ret[1].Interface().(error)
		return res, err
	}
	if opt.Middleware != nil {
		bh = opt.Middleware(bh)
	}
	return &methodHandler{
		backend: bh,
		reqType: fnt.In(1).Elem(),
		decoder: opt.RequestDecoder,
		encoder: opt.ResponseEncoder,
	}
}

func (h *methodHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var err error
	format := r.FormValue("format")
	if format == "" {
		format = "json"
	}
	var rb []byte
	if s := r.FormValue("request"); s != "" {
		rb = ([]byte)(s)
	} else {
		rb, err = ioutil.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("Read request from HTTP body failed, %v", err), 400)
			return
		}
	}

	ctx := r.Context()
	req := reflect.New(h.reqType).Interface()
	if err := h.decoder(req, rb, format); err != nil {
		http.Error(w, fmt.Sprintf("Decode request failed, %v", err), 400)
		return
	}
	res, err := h.backend(ctx, req)

	w.Header().Set("X-Content-Type-Options", "nosniff")
	if err != nil {
		st := 500
		if e, ok := err.(WithHTTPStatus); ok {
			st = e.HTTPStatus()
		}
		if e, ok := err.(WithMessage); ok {
			if m := e.Message(); m != nil && h.encoder(w, st, m, format) == nil {
				return
			}
			// When we cannot encode message provided, we fallback to use err.String()
			// This might not be the best strategy because client may blindly trying to
			// parse the pure text and blow up. But we should blame client for blow up
			// handling plain text HTTP error message then.
		}
		fmt.Fprintln(w, err)
		return
	}
	if err := h.encoder(w, 200, res, format); err != nil {
		http.Error(w, fmt.Sprintf("Encode response failed, %v", err), 500)
		return
	}
}

// ProtoDecoder implements RequestDecoder for protobuf.
func ProtoDecoder(dst interface{}, src []byte, format string) error {
	if len(src) == 0 {
		return nil
	}
	dstProto, ok := dst.(proto.Message)
	if !ok {
		return fmt.Errorf("Decode destination is not proto")
	}
	switch format {
	case "json":
		return jsonpb.Unmarshal(bytes.NewBuffer(src), dstProto)
	case "proto":
		return proto.Unmarshal(src, dstProto)
	case "text":
		return proto.UnmarshalText(string(src), dstProto)
	default:
		return fmt.Errorf("Unknown format %s", format)
	}
}

// ProtoEncoder implements ResponseEncoder for protobuf.
func ProtoEncoder(w http.ResponseWriter, status int, src interface{}, format string) error {
	srcProto, ok := src.(proto.Message)
	if !ok {
		return fmt.Errorf("Encode source is not proto")
	}
	switch format {
	case "json":
		w.Header().Add("Content-Type", "text/json; charset=utf-8")
		w.WriteHeader(status)
		m := jsonpb.Marshaler{}
		return m.Marshal(w, srcProto)
	case "proto":
		w.Header().Add("Content-Type", "application/x-protobuf")
		w.WriteHeader(status)
		rb, err := proto.Marshal(srcProto)
		if err != nil {
			return err
		}
		_, err = w.Write(rb)
		return err
	case "text":
		w.Header().Add("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		return proto.MarshalText(w, srcProto)
	default:
		return fmt.Errorf("Unknown format %s", format)
	}
}

type serviceHandler struct {
	methods map[string]http.Handler
}

// NewServiceHandler creates an http.Handler that serves all public method of serv.
// These public methods must conforms to Handler, but their req and res can be any types that implements proto.Message,
// NewServiceHandler handles them using reflect.
//
// Note that RegisterService exports all public method of serv, it would generally be safer to pass in an interface
// instead of struct, to avoid unintentially exports methods that's not intended to serve externally.
func NewServiceHandler(serv interface{}, opt *Options) http.Handler {
	if opt == nil {
		opt = &Options{}
	}
	if opt.RequestDecoder == nil {
		opt.RequestDecoder = ProtoDecoder
	}
	if opt.ResponseEncoder == nil {
		opt.ResponseEncoder = ProtoEncoder
	}

	methods := map[string]http.Handler{}
	servVal := reflect.ValueOf(serv)
	servType := reflect.TypeOf(serv)
	for i := 0; i < servType.NumMethod(); i++ {
		mn := servType.Method(i).Name
		methods[mn] = newMethodHandler(servVal.MethodByName(mn).Interface(), opt)
	}
	return &serviceHandler{methods: methods}
}

func (h *serviceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	method := r.FormValue("method")
	if method == "" {
		http.Error(w, "No method parameter", 400)
		return
	}
	var mh http.Handler
	var ok bool
	if mh, ok = h.methods[method]; !ok {
		http.Error(w, "Method not found", 404)
		return
	}
	mh.ServeHTTP(w, r)
}

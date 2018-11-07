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

// HTTPStatus interface can report an HTTP StatusCode the object associated with.
// Often used with an error that needed to be coresponded to a certain HTTP Code
type HTTPStatus interface {
	HTTPStatus() int
}

type errorWithStatus struct {
	status        int
	customMessage string
}

func (e *errorWithStatus) Error() string {
	if e.customMessage == "" {
		return http.StatusText(e.status)
	}
	return e.customMessage
}

func (e *errorWithStatus) HTTPStatus() int {
	return e.status
}

// Error returns an error with corresponding HTTP status code, when custom message
// emtpy, the default HTTP status text will be used.
func Error(status int, customMessage string) error {
	return &errorWithStatus{
		status:        status,
		customMessage: customMessage,
	}
}

// RequestInterceptor intercepts request before main handler, it can modify request as needed.
// If non-nil res returned, all request interceptors after and main handler will be skipped but response interceptor will be run as normal.
// If non-nil err returned, all processing will be skipped and error will be returnd to client.
//
// This happend to be compatible with gprc's UnaryHandler
type RequestInterceptor func(ctx context.Context, req interface{}) (res interface{}, err error)

// ResponseInterceptor intercepts response after main handler, modify response as needed.
// If non-nil err returned, all response interceptors after will be skipped and error wil be returned to client.
type ResponseInterceptor func(ctx context.Context, res interface{}) error

// Options contains options like interceptors.
type Options struct {
	RequestInterceptors  []RequestInterceptor
	ResponseInterceptors []ResponseInterceptor
}

type methodHandler struct {
	// The backend function to call
	backend reflect.Value
	reqType reflect.Type
	opt     *Options
}

func newMethodHandler(fn interface{}, opt *Options) *methodHandler {
	fnt := reflect.TypeOf(fn)
	if fnt.Kind() != reflect.Func {
		panic("fn is not a function")
	}
	ctxType := reflect.TypeOf((*context.Context)(nil)).Elem()
	protoType := reflect.TypeOf((*proto.Message)(nil)).Elem()
	errType := reflect.TypeOf((*error)(nil)).Elem()
	switch {
	case fnt.NumIn() != 2,
		fnt.NumOut() != 2,
		!fnt.In(0).Implements(ctxType),
		fnt.In(1).Kind() != reflect.Ptr,
		!fnt.In(1).Implements(protoType),
		!fnt.Out(0).Implements(protoType),
		fnt.Out(1) != errType:
		panic("fn should be like func(context.Context, *requestProto) (*responesProto, error)")
	}
	return &methodHandler{
		backend: reflect.ValueOf(fn),
		reqType: fnt.In(1).Elem(),
		opt:     opt,
	}
}

func (h *methodHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var err error
	format := "json"
	if v := r.Form["format"]; len(v) > 0 {
		format = v[0]
	}
	var rb []byte
	if v := r.Form["request"]; len(v) > 0 {
		rb = ([]byte)(v[0])
	} else {
		rb, err = ioutil.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("read request from HTTP body failed, %v", err), 400)
			return
		}
	}

	ctx := r.Context()
	req := reflect.New(h.reqType).Interface()
	if err := h.decode(req.(proto.Message), rb, format); err != nil {
		http.Error(w, fmt.Sprintf("parse request failed, %v", err), 400)
		return
	}
	var res interface{}
	for _, ri := range h.opt.RequestInterceptors {
		if res, err = ri(ctx, req); res != nil || err != nil {
			break
		}
	}
	if res == nil && err == nil {
		ret := h.backend.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(req)})
		res = ret[0].Interface()
		err, _ = ret[1].Interface().(error)
	}
	if err == nil {
		for _, ri := range h.opt.ResponseInterceptors {
			if err = ri(ctx, res); err != nil {
				break
			}
		}
	}
	if err == nil {
		err = h.encode(w, res, format)
	}
	if err != nil {
		statusCode := 500
		if s, ok := err.(HTTPStatus); ok {
			statusCode = s.HTTPStatus()
		}
		http.Error(w, err.Error(), statusCode)
	}
}

func (h *methodHandler) decode(dst proto.Message, src []byte, format string) error {
	if len(src) == 0 {
		return nil
	}
	switch format {
	case "json":
		return jsonpb.Unmarshal(bytes.NewBuffer(src), dst)
	case "proto":
		return proto.Unmarshal(src, dst)
	case "text":
		return proto.UnmarshalText(string(src), dst)
	default:
		return fmt.Errorf("unknown format %s", format)
	}
}

func (h *methodHandler) encode(w http.ResponseWriter, res interface{}, format string) error {
	var resProto proto.Message
	var ok bool
	if resProto, ok = res.(proto.Message); !ok || resProto == nil {
		return fmt.Errorf("Cannot encode non proto or nil response")
	}
	switch format {
	case "json":
		w.Header().Add("Content-Type", "text/json; charset=utf-8")
		m := jsonpb.Marshaler{}
		m.Marshal(w, resProto)
	case "proto":
		w.Header().Add("Content-Type", "application/x-protobuf")
		rb, _ := proto.Marshal(resProto)
		w.Write(rb)
	case "text":
		w.Header().Add("Content-Type", "text/plain; charset=utf-8")
		proto.MarshalText(w, resProto)
	}
	return nil
}

type serviceHandler struct {
	methods map[string]http.Handler
}

// NewServiceHandler creates an http.Handler that serves all public method of serv.
//
// Note that RegisterService exports all public method of serv, it would generally be safer to pass in an interface
// instead of struct, to avoid unintentially exports methods that's not intended to serve externally.
func NewServiceHandler(serv interface{}, opt *Options) http.Handler {
	if opt == nil {
		opt = &Options{}
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
	r.ParseForm()
	var method string
	if v := r.Form["method"]; len(v) > 0 {
		method = v[0]
	}
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

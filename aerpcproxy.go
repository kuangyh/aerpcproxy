// Package aerpcproxy provides simple proxy and common middleware from an AppEngine HTTP request to Go function.
// It supports gRPC style handler function: f(context, *requestProto) (*responseProto, error)
package aerpcproxy

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"google.golang.org/appengine"
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

type proxyHandler struct {
	// The backend function to call
	backend reflect.Value
	reqType reflect.Type
}

func (h *proxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	format := "proto"
	if v := r.Form["format"]; len(v) > 0 {
		format = v[0]
	}
	var rb []byte
	if v := r.Form["request"]; len(v) > 0 {
		rb = ([]byte)(v[0])
	} else {
		var err error
		rb, err = ioutil.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("read request from HTTP body failed, %v", err), 500)
			return
		}
	}

	req := reflect.New(h.reqType).Interface().(proto.Message)
	if len(rb) > 0 {
		var err error
		switch format {
		case "json":
			err = jsonpb.Unmarshal(bytes.NewBuffer(rb), req)
		case "proto":
			err = proto.Unmarshal(rb, req)
		case "text":
			err = proto.UnmarshalText(string(rb), req)
		default:
			err = fmt.Errorf("unknown format %s", format)
		}
		if err != nil {
			http.Error(w, fmt.Sprintf("failed parsing request, %v", err), 500)
			return
		}
	}
	ctx := appengine.NewContext(r)
	ret := h.backend.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(req)})
	if err, ok := ret[1].Interface().(error); ok && err != nil {
		statusCode := 500
		if s, ok := err.(HTTPStatus); ok {
			statusCode = s.HTTPStatus()
		}
		http.Error(w, err.Error(), statusCode)
		return
	}
	res := ret[0].Interface().(proto.Message)
	switch format {
	case "json":
		w.Header().Add("Content-Type", "text/json; charset=utf-8")
		m := jsonpb.Marshaler{}
		m.Marshal(w, res)
	case "proto":
		w.Header().Add("Content-Type", "application/x-protobuf")
		rb, _ := proto.Marshal(res)
		w.Write(rb)
	case "text":
		w.Header().Add("Content-Type", "text/plain; charset=utf-8")
		proto.MarshalText(w, res)
	}
}

// Proxy wraps a function to HTTP handler that can be directly serves AppEngine requests.
// Function should look like func(context.Context, *requestProto) (*responesProto, error)
func Proxy(fn interface{}) http.Handler {
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
	return &proxyHandler{
		backend: reflect.ValueOf(fn),
		reqType: fnt.In(1).Elem(),
	}
}

package main

import (
	"context"
	"log"
	"net/http"

	"github.com/golang/protobuf/proto"
	"github.com/kuangyh/swiffy"
	"github.com/kuangyh/swiffy/examples/hello"
)

func logInterceptor(ctx context.Context, req interface{}) (interface{}, error) {
	if reqProto, ok := req.(proto.Message); ok {
		log.Printf("Request: %s", proto.MarshalTextString(reqProto))
	}
	return nil, nil
}

func main() {
	opt := &swiffy.Options{
		RequestInterceptors: []swiffy.RequestInterceptor{
			logInterceptor,
		},
	}

	var helloServ hello.HelloServiceServer = &hello.Service{}
	http.Handle("/api/hello", swiffy.NewServiceHandler(helloServ, opt))

	log.Fatal(http.ListenAndServe(":8080", nil))
}

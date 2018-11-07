package main

import (
	"context"
	"log"
	"net/http"

	"github.com/golang/protobuf/proto"
	"github.com/kuangyh/swiffy"
	"github.com/kuangyh/swiffy/examples/hello"
)

func protoLogger(h swiffy.Handler) swiffy.Handler {
	return func(ctx context.Context, req interface{}) (interface{}, error) {
		if reqProto, ok := req.(proto.Message); ok {
			log.Printf("Request: %s", proto.MarshalTextString(reqProto))
		}
		res, err := h(ctx, req)
		if err != nil {
			log.Printf("Response: error %v", err)
		} else if resProto, ok := res.(proto.Message); ok {
			log.Printf("Response: %s", proto.MarshalTextString(resProto))
		}
		return res, err
	}
}

func main() {
	opt := &swiffy.Options{Middleware: protoLogger}

	var helloServ hello.HelloServiceServer = &hello.Service{}
	http.Handle("/api/hello", swiffy.NewServiceHandler(helloServ, opt))

	log.Fatal(http.ListenAndServe(":8080", nil))
}

//go:generate protoc -Iprotos --go_out=plugins=grpc:protos protos/hello_service.proto
package main

import (
	"context"
	"log"
	"fmt"
	"net/http"

	"github.com/golang/protobuf/proto"
	"yuheng.io/swiffy"
	pb "yuheng.io/swiffy/examples/protos"
)

// protoLogger is an example of swiffy.Middleware usage: intercept and print request and responese,
// you can do more like modify request before calling underliring handler etc.
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

// helloServ implements gRPC Hello service
type helloServ struct{}

// Hello implements gRPC call Hello()
func (h *helloServ) Hello(ctx context.Context, req *pb.HelloRequest) (*pb.HelloResponse, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("No name")
	}
	return &pb.HelloResponse{
		Message: fmt.Sprintf("Hello %s", req.Name),
	}, nil
}

func main() {
	opt := &swiffy.Options{Middleware: protoLogger}
	http.Handle("/api/hello", swiffy.NewServiceHandler(pb.HelloServer(&helloServ{}), opt))
	log.Fatal(http.ListenAndServe(":8080", nil))
}

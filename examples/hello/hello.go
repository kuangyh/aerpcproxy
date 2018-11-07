// Package hello implements simplest hello service.
// go:generate protoc --go_out=plugins=grpc:. hello_service.proto
package hello

import (
	"context"
	"fmt"
)

// Service implements gRPC HelloService
type Service struct{}

// Hello implements gRPC call Hello()
func (h *Service) Hello(ctx context.Context, req *HelloRequest) (*HelloResponse, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("No name")
	}
	return &HelloResponse{
		Message: fmt.Sprintf("Hello %s", req.Name),
	}, nil
}

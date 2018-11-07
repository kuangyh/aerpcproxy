package main

import (
	"log"
	"net/http"

	"github.com/kuangyh/swiffy"
	"github.com/kuangyh/swiffy/examples/hello"
)

func main() {
	var helloServ hello.HelloServiceServer = &hello.Service{}
	swiffy.RegisterService(nil, "/api/hello", helloServ)

	log.Fatal(http.ListenAndServe(":8080", nil))
}

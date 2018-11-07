package main

import (
	"log"
	"net/http"

	"github.com/kuangyh/swiffy"
	"github.com/kuangyh/swiffy/examples/hello"
)

func main() {
	var helloServ hello.HelloServiceServer = &hello.Service{}
	http.Handle("/api/hello", swiffy.NewServiceHandler(helloServ, nil))

	log.Fatal(http.ListenAndServe(":8080", nil))
}

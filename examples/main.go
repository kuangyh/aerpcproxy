package main

import (
	"log"
	"net/http"

	"github.com/kuangyh/swiffy"
	"github.com/kuangyh/swiffy/examples/hello"
)

func main() {
	var helloServ hello.HelloServiceServer = &hello.Service{}
	helloMux := http.NewServeMux()
	swiffy.RegisterService(helloMux, nil, helloServ)
	http.Handle("/api/hello/", http.StripPrefix("/api/hello", helloMux))

	log.Fatal(http.ListenAndServe(":8080", nil))
}

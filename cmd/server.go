package main

import (
	"github.com/podded/bouncer/server"
	"log"
)

func main() {
	svr, err := server.NewServer("TEST PLEASE IGNORE", "127.0.0.1:111")
	if err != nil {
		log.Fatalln(err)
	}

	svr.RunServer(13270)
}

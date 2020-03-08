package main

import (
	"github.com/podded/bouncer/server"
	"log"
)

func main() {
	svr, err := server.NewServer("PoddedBouncer - Crypta Electrica", "127.0.0.1:11211")
	if err != nil {
		log.Fatalln(err)
	}

	svr.RunServer(13270)
}

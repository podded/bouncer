package main

import (
	"log"

	"github.com/podded/bouncer/server"
)

func main() {
	err := server.RunServer("PoddedBouncer - Crypta Electrica", "127.0.0.1:11211", 13271)
	if err != nil {
		log.Fatalln(err)
	}
}

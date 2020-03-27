package main

import (
	"log"
	"strconv"

	"github.com/gobuffalo/envy"

	"github.com/podded/bouncer/server"
)

func main() {

	envy.Load()

	userAgent := envy.Get("BOUNCER_USER_AGENT", "PoddedBouncer - Crypta Electrica")
	memcachedAddress := envy.Get("BOUNCER_MEMCACHED_ADDRESS", "127.0.0.1:11211")
	portEnv := envy.Get("BOUNCER_PORT", "13271")

	ratelimitEnv := envy.Get("BOUNCER_RATE_LIMIT", "50")

	port := 13271
	i, err := strconv.Atoi(portEnv)
	if err == nil {
		port = i
	}

	ratelimit := 13271
	i, err = strconv.Atoi(ratelimitEnv)
	if err == nil {
		ratelimit = i
	}


	err = server.RunServer(userAgent, memcachedAddress, port, ratelimit)
	if err != nil {
		log.Fatalln(err)
	}
}

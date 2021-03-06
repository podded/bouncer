package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/podded/bouncer"
	"github.com/podded/bouncer/client"
)

func main() {

	// Create the client. Expect the server running on the same host
	// TODO Make this configurable
	bc, version, err := client.NewBouncer("http://127.0.0.1:13271", 10*time.Second, "Test")
	if err != nil {
		log.Fatalln(err)
	}
	fmt.Printf("Connected to bouncer server version %#v\n", version)

	// We are going to test out the status endpoint
	url := "https://esi.evetech.net/v1/status/?datasource=tranquility"
	type statusResponse struct {
		Players       int       `json:"players"`
		ServerVersion string    `json:"server_version"`
		StartTime     time.Time `json:"start_time"`
	}

	var resp statusResponse

	req := bouncer.Request{
		URL:         url,
		Method:      "GET",
		Body:        []byte{},
		MaxWait:     5,
		AccessToken: "",
	}

	tries := 5
	for tries > 0 {
		start := time.Now()
		res, code, err := bc.MakeRequest(req)
		end := time.Now()
		fmt.Printf("Client Request took: %v\n", end.Sub(start))
		if err != nil {
			log.Fatalln(err)
		}
		if code != 200 {
			fmt.Printf("Non 200 status code: %d\n", code)
		}
		err = json.Unmarshal(res.Body, &resp)
		if err != nil {
			log.Fatalln(err)
		}
		fmt.Printf("%#v\n", resp)
		tries--
	}

}

package client

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/pkg/errors"
	"github.com/podded/bouncer"
)

type (
	BouncerClient struct {
		serverAddress string
		client        http.Client
		descriptor    string
	}
)

func NewBouncer(ServerAddress string, MaxTimeout time.Duration, Descriptor string) (bouncerClient *BouncerClient, version string, err error) {

	// Set up our http client
	client := http.Client{
		Timeout: MaxTimeout,
	}

	// Make sure that we have a connection to the bouncer server.
	res, err := client.Get(ServerAddress + "/ping")
	if err != nil {
		return nil, "", errors.Wrap(err, "Failed to contact bouncer server")
	}
	defer res.Body.Close()

	dec := json.NewDecoder(res.Body)
	var ver bouncer.Version
	err = dec.Decode(&ver)
	if err != nil {
		return nil, "", errors.Wrap(err, "Failed to decode bouncer server version")
	}

	return &BouncerClient{
		serverAddress: ServerAddress,
		client:        client,
		descriptor:    Descriptor,
	}, ver.String(), nil
}

func (bc *BouncerClient) MakeRequest(request bouncer.Request) (res bouncer.Response, status int, err error) {

	body, err := json.Marshal(request)
	if err != nil {
		return bouncer.Response{}, 1, errors.Wrap(err, "Failed to marshal request into json")
	}
	br := bytes.NewReader(body)

	req, err := http.NewRequest("GET", bc.serverAddress, br)
	if err != nil {
		return bouncer.Response{}, 1, errors.Wrap(err, "Failed to build http request")
	}

	response, err := bc.client.Do(req)
	if err != nil {
		return bouncer.Response{}, 1, errors.Wrap(err, "Error making request to bouncer")
	}
	defer response.Body.Close()

	resbytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return bouncer.Response{}, 1, errors.Wrap(err, "Error reading response from bouncer")
	}

	etag := response.Header.Get("ETag")

	return bouncer.Response{Body: resbytes, StatusCode: response.StatusCode, ETag: etag}, response.StatusCode, nil

}

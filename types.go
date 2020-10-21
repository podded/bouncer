package bouncer

import "time"

type (
	Request struct {
		URL         string        `json:"url"`
		Method      string        `json:"method"`
		Body        []byte        `json:"body"`
		MaxWait     time.Duration `json:"max_wait"` // TODO implement timeout handling
		AccessToken string        `json:"access_token"`
		ETag        string        `json:"etag"`
		Descriptor  string        `json:"descriptor"`
	}

	Response struct {
		Body       []byte
		ETag       string
		StatusCode int
	}
)

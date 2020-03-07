package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/gregjones/httpcache"
	httpmemcache "github.com/gregjones/httpcache/memcache"
	"github.com/pkg/errors"
	"github.com/podded/bouncer"

	"contrib.go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/zpages"

	"github.com/beefsack/go-rate"
)

type (
	Server struct {
		UserAgent   string
		Client      http.Client
		RateLimiter *rate.RateLimiter
		RetryCount  int
	}
)

func NewServer(UserAgent string, MemcachedAddress string) (serve *Server, err error) {

	// Firstly, we'll register ochttp Client views
	err = view.Register(
		ochttp.ClientCompletedCount,
		ochttp.ClientReceivedBytesDistribution,
		ochttp.ClientRoundtripLatencyDistribution,
	)

	if err != nil {
		return nil, errors.Wrap(err, "Failed to register client views for HTTP metrics")
	}

	// Enable observability to extract and examine traces and metrics.
	err = enableObservabilityAndExporters()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to setup prometheus exporter")
	}

	cache := memcache.New(MemcachedAddress)

	// Create a memcached http client for the CCP APIs.
	transport := httpcache.NewTransport(httpmemcache.NewWithClient(cache))
	transport.Transport = &http.Transport{Proxy: http.ProxyFromEnvironment}
	octr := &ochttp.Transport{Base: transport}
	client := http.Client{Transport: octr}

	// Set up the rate limiter
	rt := rate.New(50, time.Second)

	svr := &Server{
		UserAgent:   UserAgent,
		Client:      client,
		RateLimiter: rt,
		RetryCount:  10,
	}

	return svr, nil

}

func enableObservabilityAndExporters() (err error) {
	// Stats exporter: Prometheus
	pe, err := prometheus.NewExporter(prometheus.Options{
		Namespace: "ochttp_tutorial",
	})
	if err != nil {
		return errors.Wrap(err, "Failed to create the Prometheus stats exporter")
	}

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", pe)
		log.Fatal(http.ListenAndServe(":8888", mux))
	}()

	go func() {
		mux := http.NewServeMux()
		zpages.Handle(mux, "/")
		addr := ":8889"
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatalf("Failed to serve zPages")
		}
	}()

	return nil
}

func (svr *Server) RunServer(port int) {

	hserv := http.NewServeMux()
	hserv.HandleFunc("/", svr.handleServerRequest)
	hserv.HandleFunc("/ping", svr.handlePingRequest)
	http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", port), hserv)

}

func (svr *Server) handlePingRequest(w http.ResponseWriter, r *http.Request) {

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(bouncer.BuiltVersion)
	log.Println("Sent ping response")
}

func (svr *Server) handleServerRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "Only get requests accepted here :)")
		return
	}

	// Begin the business logic

	var req bouncer.Request

	// First decode the request that is being made
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(&req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(err)
		return
	}
	defer r.Body.Close()

	// TODO Handle timeouts
	//if req.MaxWait > 0 {
	//	ctx, _ = context.WithTimeout(ctx, req.MaxWait)
	//}

	// Parse the url to ensure it is correct
	u, err := url.Parse(req.URL)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(errors.Wrap(err, "Invalid URL Supplied"))
		return
	}

	if req.Method != "GET" && req.Method != "POST" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode("Only GET and POST requests are accepted")
		return
	}

	log.Printf("New request for: %s\n", req.URL)
	log.Printf("New request for: %s\n", u.RequestURI())

	// Need a reader for the bytes body
	br := bytes.NewReader(req.Body)

	// Build the new request to make
	requ, err := http.NewRequest(req.Method, u.String(), br)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(errors.Wrap(err, "Failed to build request"))
		return
	}
	if len(req.Descriptor) > 0 {
		requ.Header.Set("User-Agent", fmt.Sprintf("PoddedBouncer - Crypta Electrica - %s", req.Descriptor))
	} else {
		requ.Header.Set("User-Agent", "PoddedBouncer - Crypta Electrica - naked")
	}

	// If we have an access token then add it as a header.
	if len(req.AccessToken) > 0 {
		requ.Header.Add("Authorization", fmt.Sprintf("Bearer %s", req.AccessToken))
	}

	// Now the logic to actually make the request

	retryCount := svr.RetryCount
	for retryCount > 0 {
		// Block on our rate limiter
		svr.RateLimiter.Wait()

		sr, err := svr.Client.Do(requ)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(errors.Wrap(err, "Error trying to execute request"))
			log.Printf("Error making request: %s", err)
			return
		}
		defer sr.Body.Close()
		// Handle the various status codes we may get from CCP/AWS
		// Some are worth retrying for some we shouldn't.
		switch sr.StatusCode {
		// 400s are generally something we should handle as a valid response // but not for now
		case 400:
			fallthrough
		case 404:
			fallthrough
		case 422:
			fallthrough
		// Valid response, directly send what we have back
		case 200:
			w.WriteHeader(sr.StatusCode)
			_, err = io.Copy(w, sr.Body) // TODO better error handling
			if err != nil {
				log.Fatalln(err)
			}
			w.Header().Set("X-Retries-Taken", fmt.Sprintf("%d", svr.RetryCount-retryCount))
			return

		default:
			continue
		}
	}

	log.Printf("Maximum retries exceeded for url: %s", u.String())
	// If we get to here... Then we have run out of retries....
	w.WriteHeader(http.StatusTeapot)
	json.NewEncoder(w).Encode(errors.New("Maximum retries exceeded"))
	return

}

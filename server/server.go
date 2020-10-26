package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/gregjones/httpcache"
	httpmemcache "github.com/gregjones/httpcache/memcache"
	"github.com/pkg/errors"
	"github.com/podded/bouncer"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/ratelimit"
)

type (
	Server struct {
		UserAgent   string
		Client      http.Client
		RateLimiter ratelimit.Limiter
		RetryCount  int

		mut          sync.Mutex
		errorLimited bool
	}
)

var (
	histogram = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "bouncer_requests",
		Help:    "A histogram of bouncer response times (will roughly equate to ESI response times)",
		Buckets: []float64{0.1, 0.2, 0.5, 1, 2, 5, 10},
	}, []string{"code"})
)

func RunServer(UserAgent string, MemcachedAddress string, port int, rate int) (err error) {

	cache := memcache.New(MemcachedAddress)

	// Create a memcached http client for the CCP APIs.
	transport := httpcache.NewTransport(httpmemcache.NewWithClient(cache))
	transport.Transport = &http.Transport{Proxy: http.ProxyFromEnvironment}
	client := http.Client{Transport: transport}

	// Set up the rate limiter
	rt := ratelimit.New(rate)

	svr := &Server{
		UserAgent:   UserAgent,
		Client:      client,
		RateLimiter: rt,
		RetryCount:  10,
	}

	mserv := http.NewServeMux()
	mserv.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(":8888", mserv)

	hserv := http.NewServeMux()
	hserv.HandleFunc("/", svr.serveESIRequest())
	hserv.HandleFunc("/ping", svr.handlePingRequest)

	return http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", port), hserv)

}

func (svr *Server) handlePingRequest(w http.ResponseWriter, r *http.Request) {

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(bouncer.BuiltVersion)
	log.Println("Sent ping response")
}

func (svr *Server) serveESIRequest() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		start := time.Now()
		defer r.Body.Close()
		code := 500

		defer func() {
			httpDuration := time.Since(start)
			histogram.WithLabelValues(fmt.Sprintf("%d", code)).Observe(httpDuration.Seconds())
			//histogram.Wil
		}()

		if r.Method != "GET" {
			code = http.StatusTeapot
			w.WriteHeader(code)
			fmt.Fprint(w, "Only get requests accepted here :)")
			return
		}

		// Begin the business logic

		var req bouncer.Request

		// First decode the request that is being made
		dec := json.NewDecoder(r.Body)
		err := dec.Decode(&req)
		if err != nil {
			code = http.StatusInternalServerError
			w.WriteHeader(code)
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
			code = http.StatusTeapot
			w.WriteHeader(code)
			json.NewEncoder(w).Encode(errors.Wrap(err, "Invalid URL Supplied"))
			return
		}

		if req.Method != "GET" && req.Method != "POST" {
			code = http.StatusTeapot
			w.WriteHeader(code)
			json.NewEncoder(w).Encode("Only GET and POST requests are accepted")
			return
		}

		//log.Printf("New request for: %s\n", req.URL)

		// Need a reader for the bytes body
		br := bytes.NewReader(req.Body)

		// Build the new request to make
		requ, err := http.NewRequest(req.Method, u.String(), br)
		if err != nil {
			code = http.StatusTeapot
			w.WriteHeader(code)
			json.NewEncoder(w).Encode(errors.Wrap(err, "Failed to build request"))
			return
		}
		if len(req.Descriptor) > 0 {
			requ.Header.Set("User-Agent", fmt.Sprintf("%s - %s", svr.UserAgent, req.Descriptor))
		} else {
			requ.Header.Set("User-Agent", fmt.Sprintf("%s - naked", svr.UserAgent))
		}

		// If we have an access token then add it as a header.
		if len(req.AccessToken) > 0 {
			requ.Header.Add("Authorization", fmt.Sprintf("Bearer %s", req.AccessToken))
		}

		// If we have an ETag then send it as a header
		if len(req.ETag) > 0 {
			requ.Header.Add("If-None-Match", req.ETag)
		}

		// Add context in order to get cancellation propgation
		requ = requ.WithContext(r.Context())

		// Now the logic to actually make the request

		retryCount := svr.RetryCount
		for retryCount > 0 {
			// Wait until error limit gone
			limited := true
			for limited {
				svr.mut.Lock()
				if !svr.errorLimited {
					limited = false
				}
				svr.mut.Unlock()
			}

			// Block on our rate limiter
			svr.RateLimiter.Take()

			sr, err := svr.Client.Do(requ)
			if err != nil {
				code = http.StatusTeapot
				w.WriteHeader(code)
				json.NewEncoder(w).Encode(errors.Wrap(err, "Error trying to execute request"))
				log.Printf("Error making request: %s", err)
				return
			}
			eh := sr.Header.Get("X-ESI-Error-Limit-Remain")
			if eh != "" {
				elimit, err := strconv.Atoi(eh)
				if err == nil {
					if elimit < 50 {
						log.Printf("WARN: ERROR LIMITING: REMAIN %v\n", elimit)
						svr.mut.Lock()
						svr.errorLimited = true
						svr.mut.Unlock()
						go func() {
							time.Sleep(60 * time.Second)
							svr.mut.Lock()
							svr.errorLimited = false
							svr.mut.Unlock()
						}()
					}
				}
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
				log.Printf("DEBUG-%v-%v", sr.StatusCode, req.URL)
				log.Printf("x-esi-request-id: %v", sr.Header.Get("x-esi-request-id"))
				bd, err := ioutil.ReadAll(sr.Body)
				w.WriteHeader(sr.StatusCode)
				w.Header().Set("X-Retries-Taken", fmt.Sprintf("%d", svr.RetryCount-retryCount))
				if err == nil {
					log.Printf("DEBUG-Body-%v", string(bd))
				}
				return
			// Valid response, directly send what we have back
			case 200:
				code = sr.StatusCode
				w.WriteHeader(http.StatusOK)
				_, err = io.Copy(w, sr.Body) // TODO better error handling
				if err != nil {
					log.Fatalln(err)
				}
				w.Header().Set("X-Retries-Taken", fmt.Sprintf("%d", svr.RetryCount-retryCount))
				return
			case 304:
				code = sr.StatusCode
				w.WriteHeader(http.StatusNotModified)
				w.Header().Set("X-Retries-Taken", fmt.Sprintf("%d", svr.RetryCount-retryCount))
				return
			case 429:
				log.Printf("DEBUG-429-%v", req.URL)
				fallthrough
			default:
				log.Printf("BAD CODE - %v\n", sr.StatusCode)
				log.Printf("x-esi-request-id: %v", sr.Header.Get("x-esi-request-id"))
				w.Header().Set("X-Retries-Taken", fmt.Sprintf("%d", svr.RetryCount-retryCount))
				bd, err := ioutil.ReadAll(sr.Body)
				if err == nil {
					log.Printf("DEBUG-Body-%v", string(bd))
				}
				continue
			}
		}

		log.Printf("Maximum retries exceeded for url: %s", u.String())

		// If we get to here... Then we have run out of retries....
		code = http.StatusTeapot
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(errors.New("Maximum retries exceeded"))
		return

	}

}

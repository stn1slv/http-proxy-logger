package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync/atomic"

	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "http-proxy-logger",
		Usage: "This is HTTP proxy which prints http requests and responses to console including http body.",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:     "port",
				Aliases:  []string{"p", "listen-port"},
				Required: true,
				Usage:    "Listening port number",
			},
			&cli.StringFlag{
				Name:     "target-address",
				Aliases:  []string{"t", "target"},
				Usage:    "Proxy target address",
				Required: true,
			},
		},
		Action: func(c *cli.Context) error {
			port := c.Int("port")
			target :=
				c.String("target")
			targetURL, err := url.Parse(target)
			if err != nil {
				return err
			}

			Proxy(port, targetURL)

			return nil
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

// Proxy
// --------------------------------

// Request counter
var reqCounter int32

type DebugTransport struct{}

func (DebugTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	counter := atomic.AddInt32(&reqCounter, 1)

	requestDump, err := httputil.DumpRequestOut(r, true)
	if err != nil {
		return nil, err
	}
	log.Printf("---REQUEST %d---\n\n%s\n\n", counter, string(requestDump))

	response, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	responseDump, err := httputil.DumpResponse(response, true)
	if err != nil {
		// copying the response body did not work
		return nil, err
	}

	log.Printf("---RESPONSE %d---\n\n%s\n\n", counter, string(responseDump))
	return response, err
}

func Proxy(listenPort int, targetURL *url.URL) error {
	listenAddr := fmt.Sprintf(":%d", listenPort)
	log.Printf("Forwarding %s -> %s\n", listenAddr, targetURL.String())

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	proxy.Transport = DebugTransport{}

	d := proxy.Director
	proxy.Director = func(r *http.Request) {
		d(r) // call default director

		r.Host = targetURL.Host // set Host header as expected by target
	}

	return http.ListenAndServe(listenAddr, proxy)
}

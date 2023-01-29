package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/shirobrak/h2s-proxy/domain"
	"golang.org/x/net/proxy"
)

// https://datatracker.ietf.org/doc/html/rfc9110#section-7.6.1
var hopByHopHeaders = []string{
	"Proxy-Connection",
	"Keep-Alive",
	"TE",
	"Transfer-Encoding",
	"Upgrade",
}

func removeHopByHopHeader(header http.Header) {
	for _, h := range hopByHopHeaders {
		header.Del(h)
	}
}

func addHost2XForwardHeader(header http.Header, host string) {
	var nextValue = ""
	if prior, ok := header["X-Forwarded-For"]; ok {
		nextValue = strings.Join(prior, ", ") + ", " + host
	}
	header.Set("X-Forwarded-For", nextValue)
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

type H2SProxyServer struct {
	profile *domain.Profile
}

func NewH2SProxyServer(profile *domain.Profile) *H2SProxyServer {
	return &H2SProxyServer{
		profile: profile,
	}
}

func (s *H2SProxyServer) proxyHandler(wr http.ResponseWriter, req *http.Request) {
	log.Printf("remoteAddr: %v, Method: %v, URL: %v\n", req.RemoteAddr, req.Method, req.URL)

	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		msg := "unsupported protocal scheme " + req.URL.Scheme
		http.Error(wr, msg, http.StatusBadRequest)
		log.Println(msg)
		return
	}

	host, _, err := net.SplitHostPort(req.URL.Host)
	if err != nil {
		log.Fatalf("failed to splitHostPort: %v", err)
		http.Error(wr, "unexpected error", http.StatusInternalServerError)
		return
	}

	removeHopByHopHeader(req.Header)
	addHost2XForwardHeader(req.Header, host)

	rule, err := s.profile.MatchRule(host)
	if err != nil && err != domain.ErrNotFoundRule {
		http.Error(wr, "unexpected error", http.StatusInternalServerError)
		return
	}

	if req.RequestURI != "" {
		// http://golang.org/src/pkg/net/http/client.go
		// It is an error to set this field in an HTTP client request.
		req.RequestURI = ""
	}

	var client http.Client
	if err == nil {
		fmt.Printf("Exec rule name: %v\n", rule.Name)
		socksDialer, err := proxy.SOCKS5("tcp", fmt.Sprintf("%v:%v", rule.ProxyIP, rule.Port), nil, proxy.Direct)
		if err != nil {
			http.Error(wr, "unexpected error", http.StatusInternalServerError)
			return
		}
		tr := http.Transport{
			Dial: socksDialer.Dial,
		}
		client = http.Client{
			Transport: &tr,
		}
	} else {
		// err == domain.ErrNotFoundRule
		client = http.Client{}
	}
	res, err := client.Do(req)
	if err != nil {
		http.Error(wr, "unexpected error", http.StatusInternalServerError)
		return
	}
	defer res.Body.Close()

	removeHopByHopHeader(res.Header)
	copyHeader(wr.Header(), res.Header)
	wr.WriteHeader(res.StatusCode)
	_, err = io.Copy(wr, res.Body)
	if err != nil {
		http.Error(wr, "unexpected error", http.StatusInternalServerError)
		return
	}
}

func (s *H2SProxyServer) Run() error {
	var handler http.Handler
	http.HandleFunc("/", s.proxyHandler)
	fmt.Printf("Start H2SProxy!!! Listen [%v]...\n", s.profile.GetServerAddr())
	return http.ListenAndServe(s.profile.GetServerAddr(), handler)
}

func loadProfile(path string) (*domain.Profile, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	bytesFile, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	var profile domain.Profile
	json.Unmarshal(bytesFile, &profile)
	return &profile, nil
}

func main() {
	var profilePath = flag.String("profile", "./profile.json", "profile path")
	flag.Parse()
	profile, err := loadProfile(*profilePath)
	if err != nil {
		log.Fatalf("failed to load profile: %v\n", err)
	}
	h2sProxyServer := NewH2SProxyServer(profile)
	if err := h2sProxyServer.Run(); err != nil {
		log.Fatalf("H2SProxyServer down: %v\n", err)
	}
}

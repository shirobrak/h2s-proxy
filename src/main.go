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
	"go.uber.org/zap"
	"golang.org/x/net/proxy"
)

var logoFigure string = `
_   _ ____  ____  ____
| | | |___ \/ ___||  _ \ _ __ _____  ___   _
| |_| | __) \___ \| |_) | '__/ _ \ \/ / | | |
|  _  |/ __/ ___) |  __/| | | (_) >  <| |_| |
|_| |_|_____|____/|_|   |_|  \___/_/\_\\__, |
                                       |___/
`

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
	logger  *zap.SugaredLogger
}

func NewH2SProxyServer(profile *domain.Profile, logger *zap.SugaredLogger) *H2SProxyServer {
	return &H2SProxyServer{
		profile: profile,
		logger:  logger,
	}
}

func (s *H2SProxyServer) proxyHandler(wr http.ResponseWriter, req *http.Request) {
	s.logger.Debugf("remoteAddr: %v, Method: %v, URL: %v\n", req.RemoteAddr, req.Method, req.URL)

	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		msg := "unsupported protocal scheme " + req.URL.Scheme
		s.logger.Error(msg)
		http.Error(wr, msg, http.StatusBadRequest)
		return
	}

	host, _, err := net.SplitHostPort(req.URL.Host)
	if err != nil {
		s.logger.Errorf("failed to splitHostPort: %v", err)
		http.Error(wr, "unexpected error", http.StatusInternalServerError)
		return
	}

	removeHopByHopHeader(req.Header)
	addHost2XForwardHeader(req.Header, host)

	rule, err := s.profile.MatchRule(host)
	if err != nil && err != domain.ErrNotFoundRule {
		s.logger.Errorf("failed to match rule: %v", err)
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
		socksDialer, err := proxy.SOCKS5("tcp", fmt.Sprintf("%v:%v", rule.ProxyIP, rule.Port), nil, proxy.Direct)
		if err != nil {
			s.logger.Errorf("failed to create socksDailer: %v", err)
			http.Error(wr, "unexpected error", http.StatusInternalServerError)
			return
		}
		tr := http.Transport{
			Dial: socksDialer.Dial,
		}
		client = http.Client{
			Transport: &tr,
		}
		s.logger.Infow("proxy", "rule", rule.Name, "url", req.URL, "proxyType", rule.ProxyType, "proxyIP", rule.ProxyIP, "proxyPort", rule.Port)
	} else {
		// err == domain.ErrNotFoundRule
		client = http.Client{}
		s.logger.Infow("proxy", "rule", "default", "url", req.URL)
	}
	res, err := client.Do(req)
	if err != nil {
		s.logger.Error("failed to do req: %v", err)
		http.Error(wr, "unexpected error", http.StatusInternalServerError)
		return
	}
	defer res.Body.Close()

	removeHopByHopHeader(res.Header)
	copyHeader(wr.Header(), res.Header)
	wr.WriteHeader(res.StatusCode)
	_, err = io.Copy(wr, res.Body)
	if err != nil {
		s.logger.Error("failed to copy body: %v", err)
		http.Error(wr, "unexpected error", http.StatusInternalServerError)
		return
	}
}

func (s *H2SProxyServer) Run() error {
	var handler http.Handler
	http.HandleFunc("/", s.proxyHandler)
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
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Sync()

	h2sProxyServer := NewH2SProxyServer(profile, logger.Sugar())
	fmt.Println(logoFigure)
	fmt.Printf("H2SProxy server start, listening [%v]...\n", profile.GetServerAddr())
	if err := h2sProxyServer.Run(); err != nil {
		log.Fatalf("H2SProxyServer down: %v\n", err)
	}
}

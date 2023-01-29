package domain

import (
	"errors"
	"fmt"
	"net"
)

var ErrNotFoundRule = errors.New("not found rule")

type Profile struct {
	ServerHost string `json:"host"`
	ServerPort string `json:"port"`
	Rules      []Rule `json:"rules"`
}

type Rule struct {
	Name      string   `json:"name"`
	ProxyType string   `json:"proxy_type"` // socks5 only
	ProxyIP   string   `json:"proxy_ip"`
	Port      string   `json:"port"`
	Patterns  []string `json:"patterns"`
}

func (p *Profile) GetServerAddr() string {
	return fmt.Sprintf("%v:%v", p.ServerHost, p.ServerPort)
}

func (p *Profile) MatchRule(path string) (Rule, error) {
	for _, rule := range p.Rules {
		for _, ptn := range rule.Patterns {
			ip := net.ParseIP(path)
			_, ipNet, err := net.ParseCIDR(ptn)
			if err != nil {
				return Rule{}, err
			}
			if ipNet.Contains(ip) {
				return rule, nil
			}
		}
	}
	return Rule{}, ErrNotFoundRule
}

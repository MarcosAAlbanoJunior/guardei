package page

import (
	"net"
	"net/url"
)

func mustParse(s string) *url.URL { u, _ := url.Parse(s); return u }
func parseIP(s string) net.IP     { return net.ParseIP(s) }

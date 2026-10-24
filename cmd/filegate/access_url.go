package main

import (
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

func parseBaseURL(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	invalid := errors.New("--base-url requires an http(s) origin without credentials, path, query or fragment")
	if err != nil || u.Opaque != "" || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(value, "#") {
		return nil, invalid
	}
	// Literal IPv6 and unspecified destinations cannot be used by this service.
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && (!ip.Is4() || ip.IsUnspecified()) {
		return nil, errors.New("--base-url must identify a reachable IPv4 server or DNS name, not an IPv6 or unspecified address")
	}
	if strings.Contains(u.Hostname(), ":") {
		return nil, invalid
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, invalid
		}
	}
	if strings.HasSuffix(u.Host, ":") {
		return nil, invalid
	}
	u.Path = "/access"
	u.RawPath = ""
	return u, nil
}

func accessURL(base *url.URL, code string) string {
	u := *base
	u.RawQuery = url.Values{"code": {code}}.Encode()
	return u.String()
}

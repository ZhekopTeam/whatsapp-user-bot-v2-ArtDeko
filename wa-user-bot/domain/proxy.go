package domain

import "fmt"

// Proxy holds a proxy configuration read from the admin bot database.
type Proxy struct {
	ID        string
	Name      string
	ProxyType string // socks5, socks4, http, https
	Host      string
	Port      int
	Username  string
	Password  string
}

// URL builds the proxy URL string for whatsmeow's SetProxyAddress.
// Format: socks5://user:pass@host:port  or  socks5://host:port
func (p *Proxy) URL() string {
	scheme := p.ProxyType
	if scheme == "" {
		scheme = "socks5"
	}
	if p.Username != "" {
		return fmt.Sprintf("%s://%s:%s@%s:%d", scheme, p.Username, p.Password, p.Host, p.Port)
	}
	return fmt.Sprintf("%s://%s:%d", scheme, p.Host, p.Port)
}

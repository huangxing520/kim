package container

import (
	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/wire/pkt"
)

// HashSelector HashSelector
type HashSelector struct {
}

// Lookup a server
func (s *HashSelector) Lookup(header *pkt.Header, srvs []kim.Service) string {
	ll := len(srvs)
	if ll == 0 {
		return ""
	}
	code := HashCode(header.ChannelId)
	return srvs[code%ll].ServiceID()
}

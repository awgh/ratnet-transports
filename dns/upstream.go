package dns

import (
	"fmt"

	mdns "github.com/miekg/dns"

	"github.com/awgh/ratnet/api"
	"github.com/awgh/ratnet/api/events"
)

/*
**  UPSTREAM DIRECTION:  FROM THIS CLIENT OUTBOUND TO A REMOTE SERVER
 */

// WriteUpstream - Writes KCP data in DNS Request form to the channel headed outbound from the client
func (m *Module) WriteUpstream(buf []byte, size int) {
	b := make([]byte, size)
	copy(b, buf[:size])
	m.upstreamKCPData <- b
}

// returns true if this should be called again
func (m *Module) feedUpstream(sendEmpty bool) bool {
	req := new(mdns.Msg)
	var buf []byte
	select {
	case buf = <-m.upstreamKCPData:
		// base32 encode, then dotify / "DNS chop"
		b32s, err := Dotify(buf)
		if err != nil {
			events.Error(m.node, err)
			return false
		}
		req.SetQuestion(b32s, mdns.TypeCNAME)
	default:
		if !sendEmpty {
			return false
		}
		req.SetQuestion("mail.", mdns.TypeMX) // send no data, just get response
	}

	req.RecursionDesired = true
	// req.Compress = true

	dnsClient := &mdns.Client{Net: "udp", ReadTimeout: clientTimeout, WriteTimeout: clientTimeout, SingleInflight: true}
	r, _, err := dnsClient.Exchange(req, m.UpstreamStr)
	if err == nil {
		for _, value := range r.Answer {
			bufd, errb := Undotify(value.Header().Name)
			if errb != nil {
				events.Warning(m.node, errb)
				return false
			}
			events.Info(m.node, "feedUpstream sending", string(bufd))

			m.clientMutex.Lock()
			m.kcpClient.Input(bufd, true, false)
			m.clientMutex.Unlock()
		}
	} else {
		events.Warning(m.node, "DNS exchange failed in feedUpstream: ", m.UpstreamStr, err.Error())
	}

	m.debouncerClientUpdate.Trigger()
	return true
}

// pulls from kcpClient (user data received) and pushes to client responses channel
func (m *Module) clientUpdate() {
	buffer := make([]byte, maxMsgSize)
	m.clientMutex.Lock()
	n := m.kcpClient.Recv(buffer)
	m.clientMutex.Unlock()

	if n > 0 {

		b := buffer[:n]
		rr, err := api.RemoteResponseFromBytes(&b)
		if err != nil {
			events.Warning(m.node, "dns rpc decode failed: "+err.Error())
			return
		}
		// blocking push
		m.respchan <- *rr

		events.Info(m.node, fmt.Sprintf("clientUpdate received response: %s, %+v\n", (*rr).Error, (*rr).Value))
	}
}

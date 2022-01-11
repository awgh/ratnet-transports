package dns

import (
	"fmt"
	"math"
	"net"
	"time"

	mdns "github.com/miekg/dns"

	"github.com/awgh/ratnet/api"
	"github.com/awgh/ratnet/api/events"
)

/*
**  DOWNSTREAM DIRECTION:  FROM THIS SERVER OUTBOUND TO A REMOTE CLIENT
 */

// WriteDownstream - Writes KCP data to the DNS Client via DNS Responses
func (m *Module) WriteDownstream(buf []byte, size int) {
	b := make([]byte, size)
	copy(b, buf[:size])
	m.downstreamKCPData <- b
}

func (m *Module) handleDNS(w mdns.ResponseWriter, req *mdns.Msg) {
	events.Info(m.node, fmt.Sprintf("\n***\n***handleDNS called:  client:%x server:%x\n***\n", m.ClientConv, m.ServerConv))

	msg := new(mdns.Msg)
	msg.SetReply(req)
	msg.SetRcode(req, mdns.RcodeSuccess)

	for i := range req.Question {
		if req.Question[i].Qtype == mdns.TypeMX {
			events.Info(m.node, "handleDNS Server got MX type, continuing...")
			continue
		}
		// undotify then base32 decode
		data, err := Undotify(req.Question[i].Name)
		if err != nil {
			events.Error(m.node, "handleDNS error:", err)
		} else {
			// pass the incoming data into kcp
			m.serverMutex.Lock()
			m.kcpServer.Input(data, true, false)
			m.serverMutex.Unlock()
		}
	}

	// scrub out the original name to save space, matching transaction ID is all you need anyway
	msg.Question = make([]mdns.Question, 1)
	msg.Question[0] = mdns.Question{Name: "mail.", Qtype: req.Question[0].Qtype, Qclass: mdns.ClassINET}

	// fetch outbound data from kcp and load into response
	var answers []mdns.RR
	var item []byte

	select {
	case item = <-m.downstreamKCPData:

		// base32 encode, then dotify / "DNS chop"
		b32s, err := Dotify(item)
		if err != nil {
			events.Error(m.node, err)
			return
		}
		rr := new(mdns.A)
		rr.Hdr = mdns.RR_Header{Name: b32s, Rrtype: mdns.TypeA, Class: mdns.ClassINET, Ttl: 0}
		rr.A = net.ParseIP("192.168.1.1") // todo: this should be data
		answers = append(answers, rr)
		break
	case <-time.After(serverTimeout): // nothing's ready to go, send empty response
		// no answer, nothing to send
	}
	msg.Answer = answers

	maxItemSize := math.Ceil(1.6*float64(mtu)) + 15 // base32 overhead plus 15 bytes per-answer overhead
	ok := true
	// opportunistically grab up to 10 more, without exceeding max DNS message length of 512
	// practically speaking, this will usually only grab up 1 or 2 more
	for i := 0; ok && i < 10; i++ { // ten is max number of answers
		if float64(512-msg.Len()) < maxItemSize {
			break
		}

		select {
		case item = <-m.downstreamKCPData:
			ok = true
			// base32 encode, then dotify / "DNS chop"
			b32s, err := Dotify(item)
			if err != nil {
				events.Error(m.node, err)
				return
			}
			rr := new(mdns.A)
			rr.Hdr = mdns.RR_Header{Name: b32s, Rrtype: mdns.TypeA, Class: mdns.ClassINET, Ttl: 0}
			rr.A = net.ParseIP("192.168.1.1") // todo: this should be data
			answers = append(answers, rr)
		default:
			ok = false
		}
		msg.Answer = answers
	}

	events.Info(m.node, "handleDNS Server packed answers:", len(answers), " msg len: ", msg.Len())
	w.WriteMsg(msg)

	m.debouncerServerUpdate.Trigger()
}

// pulls from kcpServer (userdata), passes to node, responses to kcpServer (userdata)
func (m *Module) serverUpdate() {
	buffer := make([]byte, maxMsgSize)
	m.serverMutex.Lock()
	n := m.kcpServer.Recv(buffer)
	m.serverMutex.Unlock()
	if n > 0 {
		// handle response
		am, err := api.RemoteCallFromBytes(&buffer)
		if err != nil {
			events.Warning(m.node, "dns Server Recv decode failed: "+err.Error())
			return
		}

		events.Info(m.node, fmt.Sprintf("serverUpdate received: %d, %+v\n", (*am).Action, (*am).Args))

		rr := api.RemoteResponse{}
		if m.node != nil {
			var result interface{}
			if m.adminMode {
				result, err = m.node.AdminRPC(m, *am)
			} else {
				result, err = m.node.PublicRPC(m, *am)
			}
			if err != nil {
				rr.Error = err.Error()
			}
			if result != nil {
				rr.Value = result
			}
		} else {
			rr.Error = "No Node Assigned to Transport!"
		}

		events.Info(m.node, fmt.Sprintf("serverUpdate returned Error: %s, Value: %+v", rr.Error, rr.Value))
		outb := api.RemoteResponseToBytes(&rr)

		m.serverMutex.Lock()
		m.kcpServer.Send(*outb)
		m.serverMutex.Unlock()
	}
}

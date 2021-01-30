package dns

import (
	"fmt"
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
	// serverMetrics.RawBytesOut(size)
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

	// fetch outbound data from kcp and load into response
	var answers []mdns.RR
	var item []byte
	itemCount := 0

	select {
	case item = <-m.downstreamKCPData:
		itemCount++
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
	case <-time.After(ServerTimeout): // nothing's ready to go, send empty response
		// no answer, nothing to send
	}
	/*
		ok := true
		// opportunistically grab up to 1 more
		for i := 0; ok && i < 1; i++ { // ten is max number of answers
			select {
			case item = <-m.downstreamKCPData:
				ok = true
				itemCount++
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
		}
	*/
	msg.Answer = answers

	events.Info(m.node, "handleDNS Server packed answers:", len(answers))

	// scrub out the original name to save space, matching transaction ID is all you need anyway
	msg.Question = make([]mdns.Question, 1)
	msg.Question[0] = mdns.Question{Name: "mail.", Qtype: req.Question[0].Qtype, Qclass: mdns.ClassINET}

	w.WriteMsg(msg)

	m.debouncerServerUpdate.Trigger()
}

// pulls from kcpServer (userdata), passes to node, responses to kcpServer (userdata)
func (m *Module) serverUpdate() {
	buffer := make([]byte, MaxMsgSize)
	m.serverMutex.Lock()
	n := m.kcpServer.Recv(buffer)
	m.serverMutex.Unlock()
	// events.Info(m.node, "kcpServer.Recv read: ", n)
	if n > 0 {
		// events.Info(m.node, "kcpServer.Recv got raw: ", buffer[:n], string(buffer[:n]), len(buffer[:n]))

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

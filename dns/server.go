package dns

import (
	"log"
	"net"

	mdns "github.com/miekg/dns"
)

//
//  DOWNSTREAM
//

func (m *Module) handleDNS(w mdns.ResponseWriter, req *mdns.Msg) {

	//log.Printf("\n***\n***handleDNS called:  client:%x (%p)  server:%x (%p)\n***\n", m.ClientConv, m.kcpClient, m.ServerConv, m.kcpServer)
	msg := new(mdns.Msg)
	msg.SetReply(req)
	msg.SetRcode(req, mdns.RcodeSuccess)

	for i := range req.Question {
		if req.Question[i].Qtype == mdns.TypeMX {
			//log.Println("handleDNS Server got MX type, continuing...")
			continue
		}
		// undotify then base32 decode
		data, err := Undotify(req.Question[i].Name)
		if err != nil {
			log.Println("handleDNS error:", err)
		} else {
			// pass the incoming data into kcp
			//log.Println("handleDNS -> kcpServer.Input()", len(data))
			m.kcpServer.Input(data, true, false)
			serverMetrics.RawBytesIn(len(data))
		}
	}

	// fetch outbound data from kcp and load into response
	var answers []mdns.RR
	ok := true
	var item []byte
	itemCount := 0
	for i := 0; ok && i < 1; i++ { // ten is max number of answers, but anything higher than 1 is too long sometimes, so we're hitting a different limit
		select {
		case item = <-m.txchan: // txchan = output of kcpServer
			serverMetrics.RawBytesOut(len(item))
			ok = true
			itemCount++
			// base32 encode, then dotify / "DNS chop"
			b32s, err := Dotify(item)
			if err != nil {
				log.Fatal(err)
			}

			rr := new(mdns.A)
			rr.Hdr = mdns.RR_Header{Name: b32s, Rrtype: mdns.TypeA, Class: mdns.ClassINET, Ttl: 0}
			rr.A = net.ParseIP("192.168.1.1")
			answers = append(answers, rr)

			//predicateIsMX := len(req.Question) > i && req.Question[i].Qtype == mdns.TypeMX
			//log.Printf("handleDNS %d - mx:%v - %d\t%+v\n%+v\n", i, predicateIsMX, len(b32s), b32s, rr.Hdr)
		default:
			ok = false
		}
	}
	msg.Answer = answers

	// scrub out the original name to save space, matching transaction ID is all you need anyway
	msg.Question = make([]mdns.Question, 1)
	msg.Question[0] = mdns.Question{Name: "mail.", Qtype: req.Question[0].Qtype, Qclass: mdns.ClassINET}

	w.WriteMsg(msg)
}

// WriteDownstream - Writes user data to the DNS Client via DNS Responses
func (m *Module) WriteDownstream(buf []byte, size int) {

	//log.Println("WriteDownstream:", buf, buf[:size])
	b := make([]byte, size)
	copy(b, buf[:size])

	//blocking push:
	m.txchan <- b

	/*
		//non-blocking push:
		var ok bool
		select {
		case txchan <- buf[:size]:
			ok = true //enqueued without blocking
		default:
			ok = false //not enqueued, would have blocked because of queue full
		}
		if !ok {
			log.Println("WriteDownstream failed to enqueue")
		}
	*/
	//log.Println("WriteDownstream called", string(buf[:size]), len(buf[:size]))
}

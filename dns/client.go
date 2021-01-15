package dns

import (
	"bytes"
	"encoding/gob"
	"errors"
	"log"
	"time"

	"github.com/awgh/ratnet/api"
	mdns "github.com/miekg/dns"
)

//
//  UPSTREAM
//

// RPC : transmit data via DNS
func (m *Module) RPC(host string, method api.Action, args ...interface{}) (interface{}, error) {

	log.Printf("\n***\n***RPC %d called: %s  client:%x (%p)  server:%x (%p)\n***\n", method, host, m.ClientConv, m.kcpClient, m.ServerConv, m.kcpServer)
	//log.Println(args)

	// send, then start to avoid sending spurious empty MX requests
	if !m.isRunningClient {
		m.startClient()
		m.UpstreamStr = host
		//log.Println("RPC Starting")
	}

	var a api.RemoteCall
	a.Action = method
	a.Args = args

	//todo: chunkify here if len > 2447 -> update, no you have to chunkify at the node.Send level or Pickup won't work

	//use default gob encoder, then zlib, then base32, then dotify...
	var buffer bytes.Buffer
	//w := zlib.NewWriter(&buffer)
	enc := gob.NewEncoder(&buffer)
	if err := enc.Encode(a); err != nil {
		log.Println("RPC gob encode failed: " + err.Error())
		return nil, err
	}
	//w.Close()

	if buffer.Len() > 2889 { // lkg: 2889 bytes here
		log.Fatal(buffer.Bytes())
	}

	if n := m.kcpClient.Send(buffer.Bytes()); n >= 0 {
		clientMetrics.SentBytes(buffer.Len())
		clientMetrics.Print()
	} else {
		log.Fatal("kcpClient Send failed")
	}

	clientMetrics.SentMsgs()

	var rr api.RemoteResponse
	select {
	case rr = <-m.respchan:
		break
	case <-time.After(15 * time.Second):
		return nil, errors.New("RPC Timed Out")
	}

	//log.Printf("\n***\n***RPC %s returned: %+v\n***\n", method, rr)

	// todo: shut these off at some point
	//m.isRunningClient = false
	//m.UpstreamStr = ""

	if rr.IsErr() {
		return nil, errors.New(rr.Error)
	}
	if rr.IsNil() {
		return nil, nil
	}
	return rr.Value, nil
}

// WriteUpstream - Writes user data to the DNS Server via DNS Requests
func (m *Module) WriteUpstream(buf []byte, size int) {

	//log.Println("WriteUpstream called", size, "bytes")

	clientMetrics.RawBytesOut(size)

	req := new(mdns.Msg)
	if buf != nil && size > 0 {
		// base32 encode, then dotify / "DNS chop"
		b32s, err := Dotify(buf[:size])
		if err != nil {
			log.Fatal(err)
		}
		//log.Println("WriteUpstream post-dotify wants to send:", b32s)
		req.SetQuestion(b32s, mdns.TypeCNAME)
	} else {
		//log.Println("WriteUpstream post-dotify wants to send nothing")
		req.SetQuestion("mail.", mdns.TypeMX) // send no data, just get response
	}
	req.RecursionDesired = true
	//req.Compress = true

	// blocking push
	m.upstreamReqs <- req

	/*
		//non-blocking push:
		var ok bool
		select {
		case upstreamReqs <- req:
			ok = true //enqueued without blocking
		default:
			ok = false //not enqueued, would have blocked because of queue full
		}
		if !ok {
			log.Fatal("WriteUpstream failed to enqueue")
		}
	*/
}

func (m *Module) writeUpstreamInternal() {

	var req *mdns.Msg
	select {
	case req = <-m.upstreamReqs:
		break
	default:
		req = new(mdns.Msg)
		req.SetQuestion("mail.", mdns.TypeMX) // send no data, just get response
		req.RecursionDesired = true
		//req.Compress = true
	}
	dnsClient := &mdns.Client{Net: "udp", ReadTimeout: 4 * time.Second, WriteTimeout: 4 * time.Second, SingleInflight: true}
	r, _, err := dnsClient.Exchange(req, m.UpstreamStr)
	if err == nil {
		for _, value := range r.Answer {
			bufd, errb := Undotify(value.Header().Name)
			if errb != nil {
				log.Fatal(errb)
			}
			//log.Println("writeUpstreamInternal -> kcpClient.Input()", bufd, len(bufd), string(bufd), value.Header().Name)
			m.kcpClient.Input(bufd, true, false)
			clientMetrics.RawBytesIn(len(bufd))
		}
	} else {
		log.Println("DNS exchange failed in writeUpstreamInternal: " + err.Error())
		log.Fatalf("%+v\n", req)
	}
}

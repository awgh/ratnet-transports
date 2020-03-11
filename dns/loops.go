package dns

import (
	"bytes"
	"encoding/gob"
	"log"
	"time"

	"github.com/awgh/ratnet/api"
)

func (m *Module) loopClientToServer() {
	for m.isRunningClient {
		m.writeUpstreamInternal() // regular requests to server to keep traffic moving
		time.Sleep(40 * time.Millisecond)
	}
	log.Println("loopClientToServer exiting")
}

// Message pump loop: pulls from kcpClient (user data received) and pushes to client responses channel
func (m *Module) loopClientUpdate() {
	for m.isRunningClient {
		m.kcpClient.Update()

		buffer := make([]byte, MaxMsgSize)

		n := m.kcpClient.Recv(buffer)
		if n > 0 {
			clientMetrics.RecvBytes(n)
			var rr api.RemoteResponse
			dec := gob.NewDecoder(bytes.NewReader(buffer[:n]))
			if err := dec.Decode(&rr); err != nil {
				log.Println("rpc gob decode failed: " + err.Error())
			}
			//log.Println("Client Rcv'ed", buffer[:n], n, rr.Value, rr.Error)
			//non-blocking push:
			var ok bool
			select {
			case m.respchan <- rr:
				ok = true //enqueued without blocking
				clientMetrics.RecvMsgs()
			default:
				ok = false //not enqueued, would have blocked because of queue full
			}
			if !ok {
				log.Fatal("Client message pump failed to enqueue")
			}
			//log.Printf("respchan got %v %v\n", rr, ok)
		}
		time.Sleep(1 * time.Millisecond)
	}
	log.Println("loopClientUpdate exiting")
}

// Message pump loop: pulls from kcpServer (userdata), passes to node, responses to kcpServer (userdata)
func (m *Module) loopServerUpdate(adminMode bool) {

	for m.isRunningServer {
		m.kcpServer.Update()

		buffer := make([]byte, MaxMsgSize)
		n := m.kcpServer.Recv(buffer)
		if n > 0 {
			serverMetrics.RecvBytes(n)

			// handle response
			var am api.RemoteCall
			dec := gob.NewDecoder(bytes.NewReader(buffer))
			if err := dec.Decode(&am); err != nil {
				log.Println("Server Recv gob decode failed: "+err.Error(), n)
				return
			}
			serverMetrics.RecvMsgs()

			if m.node != nil {
				var result interface{}
				var err error
				if adminMode {
					//log.Println("AdminRPC:", m, am)
					result, err = m.node.AdminRPC(m, am)
				} else {
					//log.Println("PublicRPC:", m, am)
					result, err = m.node.PublicRPC(m, am)
				}
				//log.Printf("result type %T %v\n", result, err)

				rr := api.RemoteResponse{}
				if err != nil {
					rr.Error = err.Error()
				}
				if result != nil { // gob cannot encode typed Nils, only interface{} Nils...wtf?
					rr.Value = result
				}
				//log.Printf("rr %+v\n", rr)

				var outb bytes.Buffer
				enc := gob.NewEncoder(&outb)
				if err := enc.Encode(rr); err != nil {
					log.Println("listen gob encode failed 1: " + err.Error())
					break
				}
				//log.Println("kcpServer sending", string(outb.Bytes()), len(outb.Bytes()))
				if n := m.kcpServer.Send(outb.Bytes()); n >= 0 {
					serverMetrics.SentBytes(outb.Len())
					serverMetrics.SentMsgs()
				}

			} else {
				// no node, testing case
				rr := api.RemoteResponse{}
				rr.Error = "No Node Assigned to Transport!"
				var outb bytes.Buffer
				enc := gob.NewEncoder(&outb)
				if err := enc.Encode(rr); err != nil {
					log.Println("listen gob encode failed 2: " + err.Error())
					break
				}
				m.kcpServer.Send(outb.Bytes())
				serverMetrics.SentBytes(outb.Len())
				serverMetrics.SentMsgs()
			}
		}
		time.Sleep(1 * time.Millisecond)
	}
}

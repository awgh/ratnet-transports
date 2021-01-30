package dns

import (
	"errors"
	"fmt"
	"time"

	"github.com/awgh/ratnet/api"
	"github.com/awgh/ratnet/api/events"
)

var (
	RPCTimeout    = 30 * time.Second
	ClientTimeout = 4 * time.Second
	ServerTimeout = 3 * time.Second
)

//
//  UPSTREAM
//

// RPC : transmit data via DNS
func (m *Module) RPC(host string, method api.Action, args ...interface{}) (interface{}, error) {
	events.Info(m.node, fmt.Sprintf("\n***\n***RPC %d called: %s  client:%x server:%x\n***\n", method, host, m.ClientConv, m.ServerConv))

	// Send, then start to avoid sending spurious empty MX requests
	if !m.IsRunningClient() {
		m.UpstreamStr = host
		m.startClient()
	}

	var a api.RemoteCall
	a.Action = method
	a.Args = args

	// Note: Chunking happens at the node.Send level, inside ratnet, otherwise Pickup won't work

	// use serializer, (todo: then zlib), then base32, then dotify...
	// w := zlib.NewWriter(&buffer)

	buffer := api.RemoteCallToBytes(&a)
	if len(*buffer) > 2889 { // lkg: 2889 bytes here
		events.Warning(m.node, "dns trying to send large buffer: ", len(*buffer))
	}

	m.clientMutex.Lock()
	if n := m.kcpClient.Send(*buffer); n >= 0 {
		// clientMetrics.SentBytes(len(*buffer))
		// clientMetrics.Print()
	} else {
		return nil, errors.New("kcpClient Send failed")
	}
	m.clientMutex.Unlock()
	// clientMetrics.SentMsgs()

	var rr api.RemoteResponse
	rr = <-m.respchan
	/*
		select {
		case rr = <-m.respchan:
			break
		case <-time.After(RPCTimeout):
			return nil, errors.New("RPC Timed Out")
		}
	*/

	events.Info(m.node, fmt.Sprintf("\n***\n***RPC %d returned Error: %s, Value: %+v\n***\n", method, rr.Error, rr.Value))

	m.stopClient()

	if rr.IsErr() {
		return nil, errors.New(rr.Error)
	}
	if rr.IsNil() {
		return nil, nil
	}
	return rr.Value, nil
}

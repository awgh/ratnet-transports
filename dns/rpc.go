package dns

import (
	"errors"
	"fmt"

	"github.com/awgh/ratnet/api"
	"github.com/awgh/ratnet/api/events"
)

//
//  UPSTREAM
//

// RPC : transmit data via DNS
func (m *Module) RPC(host string, method api.Action, args ...interface{}) (interface{}, error) {
	events.Info(m.node, fmt.Sprintf("\n***\n***RPC %d called: %s  client:%x server:%x\n***\n", method, host, m.ClientConv, m.ServerConv))

	if !m.IsRunningClient() {
		m.UpstreamStr = host
		m.initClient()
		m.startClient()
	}

	var a api.RemoteCall
	a.Action = method
	a.Args = args

	// Note: Chunking happens at the node.Send level, inside ratnet, otherwise Pickup won't work

	buffer := api.RemoteCallToBytes(&a)
	if len(*buffer) > 2889 { // lkg: 2889 bytes here
		events.Warning(m.node, "dns trying to send large buffer: ", len(*buffer))
	}

	m.clientMutex.Lock()
	m.kcpClient.Send(*buffer)
	m.clientMutex.Unlock()

	rr := <-m.respchan

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

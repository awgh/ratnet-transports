package dns

import (
	"encoding/json"
	"log"

	mdns "github.com/miekg/dns"
	kcp "github.com/xtaci/kcp-go"

	"github.com/awgh/ratnet"
	"github.com/awgh/ratnet/api"
)

// MTU - the effective MTU inside the DNS tunnel
const MTU = 150

// MaxMsgSize - the maximum size of a single message
const MaxMsgSize = 2889

func init() {
	ratnet.Transports["dns"] = NewFromMap // register this module by name (for deserialization support)
}

// Module : DNS Implementation of a Transport module
type Module struct {
	node                             api.Node
	isRunningClient, isRunningServer bool
	byteLimit                        int64

	ListenStr, UpstreamStr string
	ClientConv, ServerConv uint32

	kcpClient, kcpServer *kcp.KCP

	// channels
	txchan       chan []byte
	respchan     chan api.RemoteResponse
	upstreamReqs chan *mdns.Msg
}

// NewFromMap : Makes a new instance of this transport module from a map of arguments (for deserialization support)
func NewFromMap(node api.Node, t map[string]interface{}) api.Transport {

	listenStr := ""
	upstreamStr := ""
	clientConv := uint32(0x11223344)
	serverConv := uint32(0x11223344)
	if _, ok := t["ListenStr"]; ok {
		listenStr = t["ListenStr"].(string)
	}
	if _, ok := t["UpstreamStr"]; ok {
		upstreamStr = t["UpstreamStr"].(string)
	}
	if _, ok := t["ClientConv"]; ok {
		clientConv = t["ClientConv"].(uint32)
	}
	if _, ok := t["ServerConv"]; ok {
		serverConv = t["ServerConv"].(uint32)
	}

	instance := New(node, clientConv, serverConv)
	instance.UpstreamStr = upstreamStr
	instance.ListenStr = listenStr

	return instance
}

// New : Makes a new instance of this transport module
func New(node api.Node, clientConv uint32, serverConv uint32) *Module {

	instance := new(Module)
	instance.node = node
	instance.isRunningClient = false
	instance.isRunningServer = false

	instance.ClientConv = clientConv
	instance.ServerConv = serverConv

	instance.txchan = make(chan []byte, 200)
	instance.respchan = make(chan api.RemoteResponse, 200)
	instance.upstreamReqs = make(chan *mdns.Msg, 200) // upstream DNS reqs from WriteUpstream

	// Client is for client connections (from me) and server responses (from remote)
	// Server is for server connections (from remote) and my responses (from me)

	instance.kcpClient = kcp.NewKCP(clientConv, func(buf []byte, size int) {
		instance.WriteUpstream(buf, size)
	})
	instance.kcpClient.SetMtu(MTU) // ((5/8) * 253) -8

	instance.kcpServer = kcp.NewKCP(serverConv, func(buf []byte, size int) {
		instance.WriteDownstream(buf, size)
	})
	instance.kcpServer.SetMtu(MTU)

	instance.byteLimit = 2410

	// NoDelay options
	// fastest: ikcp_nodelay(kcp, 1, 20, 2, 1)
	// nodelay: 0:disable(default), 1:enable
	// interval: internal update timer interval in millisec, default is 100ms
	// resend: 0:disable fast resend(default), 1:enable fast resend
	// nc: 0:normal congestion control(default), 1:disable congestion control
	instance.kcpClient.NoDelay(1, 20, 2, 2)
	instance.kcpServer.NoDelay(1, 20, 2, 1)

	return instance
}

// Name : Returns name of module
func (m *Module) Name() string {
	return "dns"
}

// MarshalJSON : Create a serialied representation of the config of this module
func (m *Module) MarshalJSON() (b []byte, e error) {
	return json.Marshal(map[string]interface{}{
		"Transport": "dns"})
}

// ByteLimit - get limit on bytes per bundle for this transport
func (m *Module) ByteLimit() int64 { return m.byteLimit }

// SetByteLimit - set limit on bytes per bundle for this transport
func (m *Module) SetByteLimit(limit int64) { m.byteLimit = limit }

// Listen : opens a UDP socket and listens
func (m *Module) Listen(listen string, adminMode bool) {
	// make sure we don't run twice
	if m.isRunningServer {
		return
	}
	m.ListenStr = listen
	m.startServer(adminMode)

	//go serve("tcp", listen)
	go m.serve("udp", listen)
}

// Stop : Stops module
func (m *Module) Stop() {
	m.isRunningClient = false
	m.isRunningServer = false
}

// Private / Internal Methods

func (m *Module) serve(net, addr string) {

	serveMux := mdns.NewServeMux()
	serveMux.HandleFunc(".", func(w mdns.ResponseWriter, req *mdns.Msg) {
		m.handleDNS(w, req)
	})

	server := &mdns.Server{Addr: addr, Net: net, TsigSecret: nil, Handler: serveMux}
	err := server.ListenAndServe()
	if err != nil {
		log.Fatalf("Failed to setup the %s server: %v\n", net, err)
	}
}

func (m *Module) startClient() {
	m.isRunningClient = true

	/*
		m.kcpClient = kcp.NewKCP(0x11223344, func(buf []byte, size int) {
			m.WriteUpstream(buf, size)
		})
		m.kcpClient.SetMtu(MTU) // ((5/8) * 253) -8
		m.kcpClient.NoDelay(1, 100, 1, 1)
	*/
	go m.loopClientToServer()
	go m.loopClientUpdate()
	/*
		go func() {
			for m.isRunningClient {
				clientMetrics.Print()
				time.Sleep(2 * time.Second)
			}
		}()
	*/
}

func (m *Module) startServer(adminMode bool) {
	m.isRunningServer = true
	go m.loopServerUpdate(adminMode)
	/*
		go func() {
			for m.isRunningServer {
				serverMetrics.Print()
				time.Sleep(2 * time.Second)
			}
		}()
	*/
}

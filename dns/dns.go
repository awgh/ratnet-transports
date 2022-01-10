package dns

import (
	"encoding/json"
	"log"
	"sync"
	"sync/atomic"
	"time"

	mdns "github.com/miekg/dns"
	kcp "github.com/xtaci/kcp-go"

	"github.com/awgh/debouncer"
	"github.com/awgh/ratnet"
	"github.com/awgh/ratnet/api"
	"github.com/awgh/ratnet/api/events"
)

const (
	// mtu - the effective mtu inside the DNS tunnel
	mtu = 150

	// maxMsgSize - the maximum size of a single message
	maxMsgSize = 2889
)

var (
	clientTimeout = 4 * time.Second
	serverTimeout = 3 * time.Second
)

func init() {
	ratnet.Transports["dns"] = NewFromMap // register this module by name (for deserialization support)
}

// Module : DNS Implementation of a Transport module
type Module struct {
	node                             api.Node
	isRunningClient, isRunningServer uint32
	byteLimit                        int64

	ListenStr, UpstreamStr string
	ClientConv, ServerConv uint32

	kcpClient, kcpServer *kcp.KCP
	server               *mdns.Server
	wgClient             sync.WaitGroup
	wgServer             sync.WaitGroup
	clientsByHost        map[string]*kcp.KCP

	debouncerClientUpdate *debouncer.Debouncer
	debouncerServerUpdate *debouncer.Debouncer
	adminMode             bool

	// channels
	upstreamKCPData   chan []byte
	downstreamKCPData chan []byte
	respchan          chan api.RemoteResponse

	// mutexes
	clientMutex sync.Mutex
	serverMutex sync.Mutex
}

// NewFromMap : Makes a new instance of this transport module from a map of arguments (for deserialization support)
func NewFromMap(node api.Node, t map[string]interface{}) api.Transport {
	listenStr := ""
	upstreamStr := ""
	clientConv := uint32(0xFFFFFFFF)
	serverConv := uint32(0xFFFFFFFF)
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

	instance.ClientConv = clientConv
	instance.ServerConv = serverConv

	// size of for all channels created
	channelSize := 200

	instance.respchan = make(chan api.RemoteResponse, channelSize)

	instance.upstreamKCPData = make(chan []byte, channelSize)
	instance.downstreamKCPData = make(chan []byte, channelSize)

	// Client is for client connections (from me) and server responses (from remote)
	// Server is for server connections (from remote) and my responses (from me)
	instance.clientsByHost = make(map[string]*kcp.KCP)

	instance.byteLimit = 2410

	instance.debouncerServerUpdate = debouncer.New(20*time.Millisecond, func() {
		instance.serverUpdate()
	})
	instance.debouncerClientUpdate = debouncer.New(20*time.Millisecond, func() {
		instance.clientUpdate()
	})

	return instance
}

// Name : Returns name of module
func (m *Module) Name() string {
	return "dns"
}

// MarshalJSON : Create a serialied representation of the config of this module
func (m *Module) MarshalJSON() (b []byte, e error) {
	return json.Marshal(map[string]interface{}{
		"Transport": "dns",
	})
}

// ByteLimit - get limit on bytes per bundle for this transport
func (m *Module) ByteLimit() int64 { return m.byteLimit }

// SetByteLimit - set limit on bytes per bundle for this transport
func (m *Module) SetByteLimit(limit int64) { m.byteLimit = limit }

// Listen : opens a UDP socket and listens
func (m *Module) Listen(listen string, adminMode bool) {
	m.ListenStr = listen
	m.adminMode = adminMode
	go m.serve("udp", listen, adminMode)
}

// Stop : Stops module
func (m *Module) Stop() {
	m.stopServer()
}

// Private / Internal Methods

func (m *Module) serve(net, addr string, adminMode bool) {
	m.kcpServer = kcp.NewKCP(m.ServerConv,
		func(buf []byte, size int) {
			if size > 0 {
				b := make([]byte, size)
				copy(b, buf[:size])
				m.downstreamKCPData <- b

			}
		})
	m.kcpServer.SetMtu(mtu) // ((5/8) * 253) -8
	// NoDelay options
	// fastest: ikcp_nodelay(kcp, 1, 20, 2, 1)
	// nodelay: 0:disable(default), 1:enable
	// interval: internal update timer interval in millisec, default is 100ms
	// resend: 0:disable fast resend(default), 1:enable fast resend
	// nc: 0:normal congestion control(default), 1:disable congestion control
	// m.kcpServer.NoDelay(1, 20, 2, 1)
	m.kcpServer.NoDelay(0, 20, 0, 1)

	m.wgServer.Add(1)
	m.setIsRunningServer(true)

	go func() {
		defer m.wgServer.Done()

		for m.IsRunningServer() {
			time.Sleep(time.Millisecond * 15)
			m.serverMutex.Lock()
			m.kcpServer.Update()
			m.serverMutex.Unlock()
		}
	}()

	serveMux := mdns.NewServeMux()
	serveMux.HandleFunc(".", func(w mdns.ResponseWriter, req *mdns.Msg) {
		m.handleDNS(w, req)
	})

	m.serverMutex.Lock()
	m.server = &mdns.Server{Addr: addr, Net: net, TsigSecret: nil, Handler: serveMux}
	m.serverMutex.Unlock()
	err := m.server.ListenAndServe()
	if err != nil {
		log.Fatalf("Failed to setup the %s server: %v\n", net, err)
	}
}

func (m *Module) initClient() {
	if m.UpstreamStr == "" {
		log.Fatal("Upstream not set")
	}

	if !m.IsRunningClient() {

		client, ok := m.clientsByHost[m.UpstreamStr]

		if !ok {
			kcpClient := kcp.NewKCP(m.ClientConv,
				func(buf []byte, size int) {
					if size > 0 {
						b := make([]byte, size)
						copy(b, buf[:size])
						m.upstreamKCPData <- b

					}
				})
			kcpClient.SetMtu(mtu) // ((5/8) * 253) -8
			kcpClient.NoDelay(0, 20, 0, 1)
			m.clientMutex.Lock()
			m.kcpClient = kcpClient
			m.clientMutex.Unlock()
			m.clientsByHost[m.UpstreamStr] = kcpClient
		} else {
			m.clientMutex.Lock()
			m.kcpClient = client
			m.clientMutex.Unlock()
		}
	}
}

func (m *Module) startClient() {
	if !m.IsRunningClient() {

		events.Info(m.node, "Starting Client")

		m.setIsRunningClient(true)

		m.wgClient.Add(1)
		go func() {
			defer m.wgClient.Done()
			for m.IsRunningClient() {
				time.Sleep(time.Millisecond * 15)
				m.clientMutex.Lock()
				m.kcpClient.Update()
				m.clientMutex.Unlock()
			}
			events.Info(m.node, "Client Update Loop Stopped")
		}()

		m.wgClient.Add(1)
		go func() {
			defer m.wgClient.Done()
			for m.IsRunningClient() {
				m.feedUpstream(true)
				time.Sleep(20 * time.Millisecond)
			}
			events.Info(m.node, "feedUpstream Loop Stopped")
		}()
	}
}

func (m *Module) stopClient() {
	if m.IsRunningClient() {
		events.Info(m.node, "Stopping Client")
		m.setIsRunningClient(false)
		m.wgClient.Wait()

		for m.feedUpstream(false) { // these are the ACKs, they need to go out
			time.Sleep(20 * time.Millisecond)
			m.clientMutex.Lock()
			m.kcpClient.Update()
			m.clientMutex.Unlock()
		}

		m.UpstreamStr = ""
		events.Info(m.node, "Client Stopped")
	}
}

func (m *Module) stopServer() {
	if m.IsRunningServer() {
		m.setIsRunningServer(false)
		m.wgServer.Wait()
		m.server.Shutdown()
	}
}

// IsRunningClient - returns true if the client is running
func (m *Module) IsRunningClient() bool {
	return atomic.LoadUint32(&m.isRunningClient) == 1
}

func (m *Module) setIsRunningClient(b bool) {
	var running uint32 = 0
	if b {
		running = 1
	}
	atomic.StoreUint32(&m.isRunningClient, running)
}

// IsRunningServer - returns true if the server is running
func (m *Module) IsRunningServer() bool {
	return atomic.LoadUint32(&m.isRunningServer) == 1
}

func (m *Module) setIsRunningServer(b bool) {
	var running uint32 = 0
	if b {
		running = 1
	}
	atomic.StoreUint32(&m.isRunningServer, running)
}

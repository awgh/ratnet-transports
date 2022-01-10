package main

import (
	"bytes"
	"errors"
	"log"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/awgh/bencrypt/bc"
	"github.com/awgh/bencrypt/ecc"
	"github.com/awgh/ratnet-transports/dns"
	"github.com/awgh/ratnet/api"
	"github.com/awgh/ratnet/api/events/defaultlogger"
	"github.com/awgh/ratnet/nodes/db"
	"github.com/awgh/ratnet/nodes/fs"
	"github.com/awgh/ratnet/nodes/qldb"
	"github.com/awgh/ratnet/nodes/ram"
	"github.com/awgh/ratnet/policy/server"
)

type TransportType string
type NodeType string
type TestNode struct {
	Node   api.Node
	Public api.Transport
	Admin  api.Transport
	Type   NodeType
	Number int
}

const (
	RAM NodeType      = "RAM"
	QL                = "QL"
	FS                = "FS"
	DB                = "DB"
	DNS TransportType = "DNS"

	// Test Messages
	testMessage1 = `'In THAT direction,' the Cat said, waving its right paw round, 'lives a Hatter: and in THAT direction,' waving the other paw, 'lives a March Hare. Visit either you like: they're both mad.'
	'But I don't want to go among mad people,' Alice remarked.
	'Oh, you can't help that,' said the Cat: 'we're all mad here. I'm mad. You're mad.'
	'How do you know I'm mad?' said Alice.
	'You must be,' said the Cat, 'or you wouldn't have come here.'`

	testMessage2 = `The spiders have always been slandered
	in the idiotic pages
	of exasperating simplifiers
	who take the fly's point of view,
	who describe them as devouring,
	carnal, unfaithful, lascivious.
	For me, that reputation
	discredits just those who concocted it:
	the spider is an engineer,
	a divine maker of watches,
	for one fly more or less
	let the imbeciles detest them.
	I want to have a talk with the spider,
	I want her to weave me a star.`

	// ECC TEST KEYS
	pubprivkeyb64Ecc = "Tcksa18txiwMEocq7NXdeMwz6PPBD+nxCjb/WCtxq1+dln3M3IaOmg+YfTIbBpk+jIbZZZiT+4CoeFzaJGEWmg=="
	pubkeyb64Ecc     = "Tcksa18txiwMEocq7NXdeMwz6PPBD+nxCjb/WCtxq18="
)

var (
	TransportTypes []TransportType
	NodeTypes      []NodeType
)

func init() {
	TransportTypes = []TransportType{DNS}
	NodeTypes = []NodeType{RAM, FS, QL} //, DB}
}

// Get preferred outbound ip of this machine
func GetOutboundIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP
}

func serve(transportPublic api.Transport, transportAdmin api.Transport, node api.Node, listenPublic string, listenAdmin string) {
	node.SetPolicy(
		server.New(transportPublic, listenPublic, false),
		server.New(transportAdmin, listenAdmin, true))

	log.Println("Public Server starting: ", listenPublic)
	log.Println("Control Server starting: ", listenAdmin)

	node.Start()
}

func initNode(n int, nodeType NodeType, transportType TransportType, p2pMode bool) TestNode {
	num := strconv.Itoa(n)
	var testNode TestNode
	testNode.Type = nodeType
	testNode.Number = n
	if nodeType == RAM {
		// RamNode Mode:
		testNode.Node = ram.New(new(ecc.KeyPair), new(ecc.KeyPair))
	} else if nodeType == QL {
		// QLDB Mode
		s := qldb.New(new(ecc.KeyPair), new(ecc.KeyPair))
		if err := os.RemoveAll("qltmp" + num); err != nil {
			log.Printf("error removing directory %s: %s\n", "qltmp"+num, err.Error())
		}
		os.Mkdir("qltmp"+num, os.FileMode(int(0755)))
		dbfile := "qltmp" + num + "/ratnet_test" + num + ".ql"
		s.BootstrapDB(dbfile)
		s.FlushOutbox(0)
		testNode.Node = s
	} else if nodeType == DB {
		// DB Mode
		s := db.New(new(ecc.KeyPair), new(ecc.KeyPair))
		if err := os.RemoveAll("dbtmp" + num); err != nil {
			log.Printf("error removing directory %s: %s\n", "dbtmp"+num, err.Error())
		}
		os.Mkdir("dbtmp"+num, os.FileMode(int(0755)))
		dbfile := "file://dbtmp" + num + "/ratnet_test" + num + ".ql"
		s.BootstrapDB("ql", dbfile)
		s.FlushOutbox(0)
		testNode.Node = s
	} else if nodeType == FS {
		testNode.Node = fs.New(new(ecc.KeyPair), new(ecc.KeyPair), "queue")
	}
	if transportType == DNS {
		testNode.Public = dns.New(testNode.Node, 0xFFFFFFFF, 0xFFFFFFFF)
		testNode.Admin = dns.New(testNode.Node, 0xFFFFFFFF, 0xFFFFFFFF)
	} else {
		log.Fatal("unsupported transport for this test")
	}
	defaultlogger.StartDefaultLogger(testNode.Node, api.Info)

	go serve(testNode.Public, testNode.Admin, testNode.Node, "localhost:3000"+num, "localhost:30"+num+"0"+num)

	time.Sleep(2 * time.Second)
	return testNode
}

func (n *TestNode) Destroy(t *testing.T) {
	n.Node.Stop()
	time.Sleep(1 * time.Second)
	switch n.Type {
	case QL:
		dirName := "qltmp" + strconv.Itoa(n.Number)
		if err := os.RemoveAll(dirName); err != nil {
			t.Errorf("error removing directory %s: %s\n", dirName, err.Error())
		}
	case DB:
		dirName := "dbtmp" + strconv.Itoa(n.Number)
		if err := os.RemoveAll(dirName); err != nil {
			t.Errorf("error removing directory %s: %s\n", dirName, err.Error())
		}
	case FS:
		dirName := "queue"
		if err := os.RemoveAll(dirName); err != nil {
			t.Errorf("error removing directory %s: %s\n", dirName, err.Error())
		}
	}
}

func test(fn func(*testing.T, NodeType, TransportType), t *testing.T) {
	for _, transportType := range TransportTypes {
		for _, nodeType := range NodeTypes {
			t.Logf("Running node type %v with transport type %v\n", nodeType, transportType)
			fn(t, nodeType, transportType)
			t.Logf("Passed with type %v and transport %v\n", nodeType, transportType)
			time.Sleep(1 * time.Second)
		}
	}
}

func Test_server_ID_1(ot *testing.T) {
	test(func(t *testing.T, nodeType NodeType, transportType TransportType) {
		server1 := initNode(1, nodeType, transportType, false)
		defer server1.Destroy(t)
		var err error
		var r1, r2 interface{}
		if r1, err = server1.Public.RPC("localhost:30001", api.ID); err != nil {
			t.Fatal(err.Error())
		} else {
			t.Logf("%+v\n", r1)
		}
		// should work on both interfaces
		if r2, err = server1.Public.RPC("localhost:30101", api.ID); err != nil {
			t.Error(err.Error())
		} else {
			t.Logf("%+v\n", r2)
		}
		r1k, ok := r1.(bc.PubKey)
		if !ok {
			t.Fatal("Public RPC did not return a PubKey")
		}
		r2k, ok := r2.(bc.PubKey)
		if !ok {
			t.Fatal("Admin RPC did not return a PubKey")
		}
		if !bytes.Equal(r1k.ToBytes(), r2k.ToBytes()) {
			t.Fatal(errors.New("Public and Admin interfaces returned different results"))
		}
	}, ot)
}

func Test_server_CID_1(ot *testing.T) {
	test(func(t *testing.T, nodeType NodeType, transportType TransportType) {
		server1 := initNode(1, nodeType, transportType, false)
		defer server1.Destroy(t)
		// should not work on public interface
		t.Log("Trying CID on Public interface")
		_, err := server1.Public.RPC("localhost:30001", api.CID)
		if err == nil {
			t.Fatal(errors.New("CID was accessible on Public network interface"))
		}
		t.Log("Trying CID on Admin interface")
		_, err = server1.Public.RPC("localhost:30101", api.CID)
		if err != nil {
			t.Fatal(err.Error())
		}
	}, ot)
}

func Test_server_AddContact_1(ot *testing.T) {
	test(func(t *testing.T, nodeType NodeType, transportType TransportType) {
		server1 := initNode(1, nodeType, transportType, false)
		defer server1.Destroy(t)
		server2 := initNode(2, nodeType, transportType, false)
		defer server2.Destroy(t)

		p1, err := server2.Public.RPC("localhost:30202", api.CID)
		if err != nil {
			t.Fatal(err.Error())
		}
		t.Logf("Got CID: %+v\n", p1)
		r1 := p1.(bc.PubKey)
		t.Logf("CID cast to PubKey: %+v -> %s\n", r1, r1.ToB64())

		t.Log("Trying AddContact on Public interface")
		// should not work on public interface
		if _, erra := server1.Public.RPC("localhost:30001", api.AddContact, "destname1", r1.ToB64()); erra == nil {
			t.Fatal(errors.New("AddContact was accessible on Public network interface"))
		}

		t.Log("Trying AddContact on Admin interface")
		_, errb := server1.Public.RPC("localhost:30101", api.AddContact, "destname1", r1.ToB64())
		if errb != nil {
			t.Fatal(errb.Error())
		}
		t.Log("Trying AddContact on local interface")
		if errc := server1.Node.AddContact("destname1", r1.ToB64()); errc != nil {
			t.Fatal(errc.Error())
		}
	}, ot)
}

func Test_server_GetContact_1(ot *testing.T) {
	test(func(t *testing.T, nodeType NodeType, transportType TransportType) {
		server1 := initNode(1, nodeType, transportType, false)
		defer server1.Destroy(t)
		if errc := server1.Node.AddContact("destname1", pubkeyb64Ecc); errc != nil {
			t.Fatal(errc.Error())
		}

		t.Log("Trying GetContact on Public interface")
		// should not work on public interface
		if _, erra := server1.Public.RPC("localhost:30001", api.GetContact, "destname1"); erra == nil {
			t.Fatal(errors.New("GetContact was accessible on Public network interface"))
		}

		t.Log("Trying GetContacts on Public interface")
		// should not work on public interface
		if _, erra := server1.Public.RPC("localhost:30001", api.GetContacts); erra == nil {
			t.Fatal(errors.New("GetContacts was accessible on Public network interface"))
		}

		t.Log("Trying GetContact on Admin interface")
		contact, err := server1.Public.RPC("localhost:30101", api.GetContact, "destname1")
		if err != nil {
			t.Fatal(err.Error())
		}
		t.Logf("Got Contact: %+v\n", contact)

		time.Sleep(2 * time.Second)

		t.Log("Trying GetContacts on Admin interface")
		contactsRaw, err := server1.Public.RPC("localhost:30101", api.GetContacts)
		if err != nil {
			t.Fatal(err.Error())
		}
		contacts := contactsRaw.([]api.Contact)
		t.Logf("Got Contacts: %+v\n", contacts)
		if len(contacts) < 1 {
			t.Fail()
		}
	}, ot)
}

func Test_server_GetContact_2(ot *testing.T) {
	test(func(t *testing.T, nodeType NodeType, transportType TransportType) {
		server1 := initNode(1, nodeType, transportType, false)
		defer server1.Destroy(t)
		if errc := server1.Node.AddContact("destname1", pubkeyb64Ecc); errc != nil {
			t.Fatal(errc.Error())
		}

		t.Log("Trying GetContact on Admin interface")
		contact, err := server1.Public.RPC("localhost:30101", api.GetContact, "destname1")
		if err != nil {
			t.Fatal(err.Error())
		}
		t.Logf("Got Contact: %+v\n", contact)

		time.Sleep(5 * time.Second)

		t.Log("Trying GetContacts on Admin interface")
		contactsRaw, err := server1.Public.RPC("localhost:30101", api.GetContacts)
		if err != nil {
			t.Fatal(err.Error())
		}
		contacts := contactsRaw.([]api.Contact)
		t.Logf("Got Contacts: %+v\n", contacts)
		if len(contacts) < 1 {
			t.Fail()
		}
	}, ot)
}

func Test_server_AddChannel_1(ot *testing.T) {
	test(func(t *testing.T, nodeType NodeType, transportType TransportType) {
		server1 := initNode(1, nodeType, transportType, false)
		defer server1.Destroy(t)
		// todo: add RSA test?
		chankey := pubprivkeyb64Ecc

		t.Log("Trying AddChannel on Public interface")
		// should not work on public interface
		_, err := server1.Public.RPC("localhost:30001", api.AddChannel, "channel1", chankey)
		if err == nil {
			t.Fatal(errors.New("AddChannel was accessible on Public network interface"))
		}

		t.Log("Trying AddChannel on Admin interface")
		_, err = server1.Public.RPC("localhost:30101", api.AddChannel, "channel1", chankey)
		if err != nil {
			t.Fatal(err.Error())
		}
	}, ot)
}

func Test_server_GetChannel_1(ot *testing.T) {
	test(func(t *testing.T, nodeType NodeType, transportType TransportType) {
		server1 := initNode(1, nodeType, transportType, false)
		defer server1.Destroy(t)
		server1.Node.AddChannel("channel1", pubprivkeyb64Ecc)
		t.Log("Trying GetChannel on Public interface")
		// should not work on public interface
		if _, erra := server1.Public.RPC("localhost:30001", api.GetChannel, "channel1"); erra == nil {
			t.Fatal(errors.New("GetChannel was accessible on Public network interface"))
		}

		t.Log("Trying GetChannels on Public interface")
		// should not work on public interface
		if _, erra := server1.Public.RPC("localhost:30001", api.GetChannels); erra == nil {
			t.Fatal(errors.New("GetChannels was accessible on Public network interface"))
		}

		t.Log("Trying GetChannel on Admin interface")
		channel, err := server1.Public.RPC("localhost:30101", api.GetChannel, "channel1")
		if err != nil {
			t.Fatal(err.Error())
		}
		t.Logf("Got Channel: %+v\n", channel)
		if channel == nil {
			t.Fail()
		}

		t.Log("Trying GetChannels on Admin interface")
		channelsRaw, err := server1.Public.RPC("localhost:30101", api.GetChannels)
		if err != nil {
			t.Fatal(err.Error())
		}
		channels := channelsRaw.([]api.Channel)
		t.Logf("Got Channels: %+v\n", channels)
		if len(channels) < 1 {
			t.Fail()
		}
	}, ot)
}

func Test_server_AddProfile_1(ot *testing.T) {
	test(func(t *testing.T, nodeType NodeType, transportType TransportType) {
		server1 := initNode(1, nodeType, transportType, false)
		defer server1.Destroy(t)
		t.Log("Trying AddProfile on Public interface")
		// should not work on public interface
		if _, erra := server1.Public.RPC("localhost:30001", api.AddProfile, "profile1", "false"); erra == nil {
			t.Fatal(errors.New("AddProfile was accessible on Public network interface"))
		}

		t.Log("Trying AddProfile on Admin interface")
		_, errb := server1.Public.RPC("localhost:30101", api.AddProfile, "profile1", "false")
		if errb != nil {
			t.Fatal(errb.Error())
		}
	}, ot)
}

func Test_server_GetProfile_1(ot *testing.T) {
	test(func(t *testing.T, nodeType NodeType, transportType TransportType) {
		server1 := initNode(1, nodeType, transportType, false)
		defer server1.Destroy(t)
		server1.Node.AddProfile("profile1", false)
		// todo: AddProfile twice in a row on ramnode is a bug
		//t.Log("Trying AddProfile on Admin interface")
		//_, errb := server1.AdminClient.RPC("localhost:30101", "AddProfile", "profile1", "false")
		//if errb != nil {
		//	t.Fatal(errb.Error())
		//}

		t.Log("Trying GetProfile on Public interface")
		// should not work on public interface
		if _, erra := server1.Public.RPC("localhost:30001", api.GetProfile, "profile1"); erra == nil {
			t.Fatal(errors.New("GetProfile was accessible on Public network interface"))
		}

		t.Log("Trying GetProfiles on Public interface")
		// should not work on public interface
		if _, erra := server1.Public.RPC("localhost:30001", api.GetProfiles); erra == nil {
			t.Fatal(errors.New("GetProfiles was accessible on Public network interface"))
		}

		t.Log("Trying GetProfile on Admin interface")
		profile, err := server1.Public.RPC("localhost:30101", api.GetProfile, "profile1")
		if err != nil {
			t.Fatal(err.Error())
		}
		t.Logf("Got Profile: %+v\n", profile)
		if profile == nil {
			t.Fail()
		}

		t.Log("Trying GetProfiles on Admin interface")
		profilesRaw, err := server1.Public.RPC("localhost:30101", api.GetProfiles)
		if err != nil {
			t.Fatal(err.Error())
		}
		profiles := profilesRaw.([]api.Profile)
		t.Logf("Got Profiles: %+v\n", profiles)
		if len(profiles) < 1 {
			t.Fail()
		}
	}, ot)
}

func Test_server_AddPeer_1(ot *testing.T) {
	test(func(t *testing.T, nodeType NodeType, transportType TransportType) {
		server1 := initNode(1, nodeType, transportType, false)
		defer server1.Destroy(t)
		t.Log("Trying AddPeer on Public interface")
		// should not work on public interface
		if _, erra := server1.Public.RPC("localhost:30001", api.AddPeer, "peer1", "false", "https://1.2.3.4:443"); erra == nil {
			t.Fatal(errors.New("AddPeer was accessible on Public network interface"))
		}

		t.Log("Trying AddPeer on Admin interface")
		_, errb := server1.Public.RPC("localhost:30101", api.AddPeer, "peer1", "false", "https://1.2.3.4:443")
		if errb != nil {
			t.Fatal(errb.Error())
		}

		t.Log("Trying AddPeer on Admin interface with a group name")
		_, errc := server1.Public.RPC("localhost:30101", api.AddPeer, "peer2", "false", "https://2.3.4.5:123", "groupnametest")
		if errc != nil {
			t.Fatal(errc.Error())
		}

		t.Log("Trying AddPeer on Admin interface with a group name that already exists")
		_, errd := server1.Public.RPC("localhost:30101", api.AddPeer, "peer3", "false", "https://3.4.5.6:234", "groupnametest")
		if errd != nil {
			t.Fatal(errd.Error())
		}
	}, ot)
}

func Test_server_GetPeer_1(ot *testing.T) {
	test(func(t *testing.T, nodeType NodeType, transportType TransportType) {
		server1 := initNode(1, nodeType, transportType, false)
		defer server1.Destroy(t)
		server1.Node.AddPeer("peer1", false, "https://1.2.3.4:443")
		server1.Node.AddPeer("peer2", false, "https://1.2.3.4:443", "groupnametest")
		server1.Node.AddPeer("peer3", false, "https://1.2.3.4:443", "groupnametest")

		t.Log("Trying GetPeer on Public interface")
		// should not work on public interface
		if _, erra := server1.Public.RPC("localhost:30001", api.GetPeer, "peer1"); erra == nil {
			t.Fatal(errors.New("GetPeer was accessible on Public network interface"))
		}

		t.Log("Trying GetPeers on Public interface")
		// should not work on public interface
		if _, erra := server1.Public.RPC("localhost:30001", api.GetPeers); erra == nil {
			t.Fatal(errors.New("GetPeers was accessible on Public network interface"))
		}

		t.Log("Trying GetPeer on Admin interface")
		peer, err := server1.Public.RPC("localhost:30101", api.GetPeer, "peer1")
		if err != nil {
			t.Fatal(err.Error())
		}
		t.Logf("Got Peer: %+v\n", peer)
		if peer == nil {
			t.Fatal(errors.New("GetPeer on Admin interface failed"))
		}

		t.Log("Trying GetPeers on Admin interface")
		peersRaw, err := server1.Public.RPC("localhost:30101", api.GetPeers)
		if err != nil {
			t.Fatal(err.Error())
		}
		peers := peersRaw.([]api.Peer)
		t.Logf("Got Peers: %+v\n", peers)
		if len(peers) != 1 {
			t.Fatal(errors.New("GetPeers on Admin interface failed"))
		}

		t.Log("Trying GetPeers on Admin interface with a group that has no peers")
		groupedPeers, err := server1.Public.RPC("localhost:30101", api.GetPeers, "not-a-group")
		if err != nil {
			t.Fatal(err.Error())
		}
		groupPeers := groupedPeers.([]api.Peer)
		t.Logf("Got Peers: %+v\n", groupPeers)
		if len(groupPeers) != 0 {
			t.Fatal(errors.New("GetPeers with a group with no peers returned results"))
		}

		t.Log("Trying GetPeers on Admin interface with a group that has peers")
		peers2, err := server1.Public.RPC("localhost:30101", api.GetPeers, "groupnametest")
		if err != nil {
			t.Fatal(err.Error())
		}
		peergroup := peers2.([]api.Peer)
		t.Logf("Got Peers: %+v\n", peergroup)
		if len(peergroup) != 2 {
			t.Fatal(errors.New("GetPeers with a group with peers did not return two results"))
		}
	}, ot)
}

func Test_server_Send_1(ot *testing.T) {
	test(func(t *testing.T, nodeType NodeType, transportType TransportType) {
		server1 := initNode(1, nodeType, transportType, false)
		defer server1.Destroy(t)
		server1.Node.AddContact("destname1", pubkeyb64Ecc)
		// should not work on public interface
		_, err := server1.Public.RPC("localhost:30001", api.Send, "destname1", []byte(testMessage1))
		if err == nil {
			t.Fatal(errors.New("Send was accessible on Public network interface"))
		}

		_, err = server1.Public.RPC("localhost:30101", api.Send, "destname1", []byte(testMessage1))
		if err != nil {
			t.Fatal(err.Error())
		}
	}, ot)
}

func Test_server_SendChannel_1(ot *testing.T) {
	test(func(t *testing.T, nodeType NodeType, transportType TransportType) {
		server1 := initNode(1, nodeType, transportType, false)
		defer server1.Destroy(t)
		server1.Node.AddChannel("channel1", pubprivkeyb64Ecc)
		// should not work on public interface
		_, err := server1.Public.RPC("localhost:30001", api.SendChannel, "channel1", []byte(testMessage2))
		if err == nil {
			t.Fatal(errors.New("SendChannel was accessible on Public network interface"))
		}

		_, err = server1.Public.RPC("localhost:30101", api.SendChannel, "channel1", []byte(testMessage2))
		if err != nil {
			t.Fatal(err.Error())
		}
	}, ot)
}

func Test_server_PickupDropoff_1(ot *testing.T) {
	test(func(t *testing.T, nodeType NodeType, transportType TransportType) {
		server1 := initNode(1, nodeType, transportType, false)
		defer server1.Destroy(t)
		server2 := initNode(2, nodeType, transportType, false)
		defer server2.Destroy(t)

		go func() {
			msg := <-server2.Node.Out()
			t.Log("server2.Out Got: ")
			t.Log(msg)
		}()

		server1.Node.AddChannel("channel1", pubprivkeyb64Ecc)
		server1.Node.SendChannel("channel1", []byte(testMessage2))

		t.Log("Trying AddChannel on server2 Admin interface")
		_, err := server2.Public.RPC("localhost:30202", api.AddChannel, "channel1", pubprivkeyb64Ecc)
		if err != nil {
			t.Fatal(err.Error())
		}

		pubsrv, err := server1.Public.RPC("localhost:30002", api.ID)
		if err != nil {
			t.Fatal("XXX:" + err.Error())
		}
		bundle, err := server1.Public.RPC("localhost:30001", api.Pickup, pubsrv, int64(31536000)) // 31536000 seconds in a year
		if err != nil {
			t.Fatal("YYY:" + err.Error())
		}

		_, err = server1.Public.RPC("localhost:30002", api.Dropoff, bundle)
		if err != nil {
			t.Fatal("ZZZ:" + err.Error())
		}
	}, ot)
}

func Test_server_PickupDropoff_2(ot *testing.T) {
	test(func(t *testing.T, nodeType NodeType, transportType TransportType) {
		server1 := initNode(1, nodeType, transportType, false)
		defer server1.Destroy(t)
		server2 := initNode(2, nodeType, transportType, false)
		defer server2.Destroy(t)

		go func() {
			msg := <-server2.Node.Out()
			t.Log("server2.Out Got: ")
			t.Log(msg)
		}()

		server1.Node.AddChannel("channel1", pubprivkeyb64Ecc)
		server1.Node.SendChannel("channel1", []byte(testMessage2))

		t.Log("Trying AddChannel on server2 Admin interface")
		_, err := server2.Public.RPC("localhost:30202", api.AddChannel, "channel1", pubprivkeyb64Ecc)
		if err != nil {
			t.Fatal(err.Error())
		}

		pubsrv, err := server1.Public.RPC("localhost:30002", api.ID)
		if err != nil {
			t.Fatal("XXX:" + err.Error())
		}
		time.Sleep(1 * time.Second)
		bundle, err := server1.Public.RPC("localhost:30001", api.Pickup, pubsrv, int64(31536000), "channel1") // 31536000 seconds in a year
		if err != nil {
			t.Fatal("YYY:" + err.Error())
		}

		_, err = server1.Public.RPC("localhost:30002", api.Dropoff, bundle)
		if err != nil {
			t.Fatal("ZZZ:" + err.Error())
		}
	}, ot)
}

func Test_p2p_Basic_1(ot *testing.T) {
	test(func(t *testing.T, nodeType NodeType, transportType TransportType) {
		p2p1 := initNode(1, nodeType, transportType, true)
		defer p2p1.Destroy(t)
		p2p2 := initNode(2, nodeType, transportType, true)
		defer p2p2.Destroy(t)

		for p2p1.Node == nil || p2p2.Node == nil {
			time.Sleep(1 * time.Second)
		}

		go func() {
			msg := <-p2p2.Node.Out()
			t.Log("p2p2.Out Got: ")
			t.Log(msg)
		}()

		if err := p2p1.Node.AddChannel("test1", pubprivkeyb64Ecc); err != nil {
			t.Fatal(err.Error())
		}
		if err := p2p2.Node.AddChannel("test1", pubprivkeyb64Ecc); err != nil {
			t.Fatal(err.Error())
		}
		if err := p2p1.Node.SendChannel("test1", []byte(testMessage1)); err != nil {
			t.Fatal(err.Error())
		}
	}, ot)
}

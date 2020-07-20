// Copyright (c) 2020, Oracle and/or its affiliates.

package s3transport

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/awgh/bencrypt/ecc"
	"github.com/awgh/ratnet/api"
	"github.com/awgh/ratnet/api/events/defaultlogger"
	"github.com/awgh/ratnet/nodes/ram"
	"github.com/awgh/ratnet/policy"
)

// ECC Test Keys
var pubprivkeyb64Ecc = "<Add a priv key>"
var pubkeyb64Ecc = "<Add a pub key>"

// Access keys for S3
var accessKey = "<Add AWS API Access Key>"
var secretKey = "<Add AWS API Secret Key>"

// Namespace settings
var namespace = "<Add your namespace>"
var region = "<Add your region>"

// AWS S3 API Endpoint. This is different for each cloud provider, and is default to Oraclecloud for testing.
var endPoint = fmt.Sprintf("%s.compat.objectstorage.%s.oraclecloud.com", namespace, region)

// What buckets to use for c2 and the monotonic timer.
// These must be separate, and empty, buckets at the time of testing and using of the S3 transport
var c2bucket = "awgh"
var timebucket = "awgh-time"

func Test_single_node(t *testing.T) {

	// make content key for the luggage tag
	keypair := new(ecc.KeyPair)
	_ = keypair.FromB64(pubprivkeyb64Ecc)
	node := ram.New(keypair, keypair)

	// put a little data in the outbox
	node.AddContact("testcontact", pubkeyb64Ecc)

	node.Send("testcontact", []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15})

	transport := New(namespace, region, node, accessKey, secretKey, pubkeyb64Ecc, endPoint, c2bucket, timebucket)

	toRemote, err := node.Pickup(transport.RoutingPubKey, 0 /* timestamp */, transport.ByteLimit())
	if err != nil {
		t.Error(node, "local pickup error: "+err.Error())
	}
	t.Log("pollServer Pickup Local result len: ", len(toRemote.Data))
	t.Logf("%+v\n", toRemote)

	_, err = transport.RPC("hoststring", "Dropoff", toRemote)

	bundle, err := transport.RPC("hoststring", "Pickup", toRemote, int64(-1))

	if err != nil {
		t.Error(node, "remote pickup error: "+err.Error())
	}
	t.Logf("%+v\n", bundle)
	if bundle.(api.Bundle).Data != nil {
		err = node.Dropoff(bundle.(api.Bundle))

		if err != nil {
			t.Error(node, "local dropoff error: "+err.Error())
		}
	}

	node.SetPolicy(policy.NewPoll(transport, node, 500, 100))

	go func() {
		for {
			msg := <-node.Out()
			mb := msg.Content.Bytes()
			t.Log("I got stuff ", string(mb))
		}
	}()

	node.Start()
	time.Sleep(10 * time.Second)
}

func Test_two_nodes_end_to_end(t *testing.T) {

	keypair := new(ecc.KeyPair)
	_ = keypair.FromB64(pubprivkeyb64Ecc)
	node1 := ram.New(nil, keypair)
	node2 := ram.New(nil, keypair)

	defaultlogger.StartDefaultLogger(node1, api.Debug)
	defaultlogger.StartDefaultLogger(node2, api.Debug)

	transport1 := New(namespace, region, node1, accessKey, secretKey, pubkeyb64Ecc, endPoint, c2bucket, timebucket)
	transport2 := New(namespace, region, node1, accessKey, secretKey, pubkeyb64Ecc, endPoint, c2bucket, timebucket)

	node1.SetPolicy(policy.NewPoll(transport1, node1, 5, 100))
	node2.SetPolicy(policy.NewPoll(transport2, node2, 5, 100))

	err := node1.Start()
	if err != nil {
		t.Error(node1, "node1 start error: "+err.Error())
	}
	err = node2.Start()
	if err != nil {
		t.Error(node2, "node2 start error: "+err.Error())
	}

	node1CID, err := node1.CID()
	if err != nil {
		t.Error(node1, "node1 CID error: "+err.Error())
	}

	node2CID, err := node2.CID()
	if err != nil {
		t.Error(node1, "node2 CID error: "+err.Error())
	}

	node1.AddContact("node2", node2CID.ToB64())
	node2.AddContact("node1", node1CID.ToB64())

	node1.AddPeer("node2", true, "")
	node2.AddPeer("node1", true, "")

	// Node 1 send a message to node 2
	msg := api.Msg{}
	msg.Name = "node2"
	msg.PubKey = node2CID
	msgString := fmt.Sprintf("Does this even work to node 2?")
	buf := bytes.NewBufferString(msgString)
	msg.Content = buf

	err = node1.SendMsg(msg)
	if err != nil {
		t.Error(node1, "node2 CID error: "+err.Error())
	}

	// Node 1 send a message to node 2
	msg = api.Msg{}
	msg.Name = "node1"
	msg.PubKey = node1CID
	msgString = fmt.Sprintf("But can I also send to node 1?")
	buf = bytes.NewBufferString(msgString)
	msg.Content = buf

	err = node1.SendMsg(msg)
	if err != nil {
		t.Error(node1, "node1 CID error: "+err.Error())
	}

	// Node 2 listen
	go func() {
		for {
			msg := <-node2.Out()
			mb := msg.Content.Bytes()
			t.Log("Node 2 got a message ", string(mb))
		}
	}()

	// Node 1 listen
	go func() {
		for {
			msg := <-node1.Out()
			mb := msg.Content.Bytes()
			t.Log("Node 1 got a message ", string(mb))
		}
	}()
	time.Sleep(10 * time.Second)
}

func Test_three_nodes_end_to_end(t *testing.T) {

	keypair := new(ecc.KeyPair)
	_ = keypair.FromB64(pubprivkeyb64Ecc)
	node1 := ram.New(nil, keypair)
	node2 := ram.New(nil, keypair)
	node3 := ram.New(nil, keypair)

	defaultlogger.StartDefaultLogger(node1, api.Debug)
	defaultlogger.StartDefaultLogger(node2, api.Debug)

	transport1 := New(namespace, region, node1, accessKey, secretKey, pubkeyb64Ecc, endPoint, c2bucket, timebucket)
	transport2 := New(namespace, region, node2, accessKey, secretKey, pubkeyb64Ecc, endPoint, c2bucket, timebucket)
	transport3 := New(namespace, region, node3, accessKey, secretKey, pubkeyb64Ecc, endPoint, c2bucket, timebucket)

	node1.SetPolicy(policy.NewPoll(transport1, node1, 5, 100))
	node2.SetPolicy(policy.NewPoll(transport2, node2, 5, 100))
	node3.SetPolicy(policy.NewPoll(transport3, node3, 5, 100))

	err := node1.Start()
	if err != nil {
		t.Error(node1, "node1 start error: "+err.Error())
	}
	err = node2.Start()
	if err != nil {
		t.Error(node2, "node2 start error: "+err.Error())
	}

	err = node3.Start()
	if err != nil {
		t.Error(node3, "node3 start error: "+err.Error())
	}

	node1CID, err := node1.CID()
	if err != nil {
		t.Error(node1, "node1 CID error: "+err.Error())
	}

	node2CID, err := node2.CID()
	if err != nil {
		t.Error(node1, "node2 CID error: "+err.Error())
	}

	node3CID, err := node3.CID()
	if err != nil {
		t.Error(node1, "node3 CID error: "+err.Error())
	}

	node1.AddContact("node2", node2CID.ToB64())
	node1.AddContact("node3", node3CID.ToB64())

	node2.AddContact("node1", node1CID.ToB64())
	node2.AddContact("node3", node3CID.ToB64())

	node3.AddContact("node1", node1CID.ToB64())
	node3.AddContact("node2", node2CID.ToB64())

	node1.AddPeer("node2", true, "")
	node1.AddPeer("node3", true, "")

	node2.AddPeer("node1", true, "")
	node2.AddPeer("node3", true, "")

	node3.AddPeer("node1", true, "")
	node3.AddPeer("node3", true, "")

	// Node 1 send a message to node 2
	msg := api.Msg{}
	msg.Name = "node2"
	msg.PubKey = node2CID
	msgString := fmt.Sprintf("**** Node 1 sending to Node 2!")
	buf := bytes.NewBufferString(msgString)
	msg.Content = buf

	err = node1.SendMsg(msg)
	if err != nil {
		t.Error(node1, "node2 CID error: "+err.Error())
	}

	// Node 1 send a message to node 3
	msg = api.Msg{}
	msg.Name = "node3"
	msg.PubKey = node3CID
	msgString = fmt.Sprintf("**** Node 1 sending to node 3!")
	buf = bytes.NewBufferString(msgString)
	msg.Content = buf

	err = node1.SendMsg(msg)
	if err != nil {
		t.Error(node1, "node1 CID error: "+err.Error())
	}

	// Node 2 send a message to node 1
	msg = api.Msg{}
	msg.Name = "node1"
	msg.PubKey = node1CID
	msgString = fmt.Sprintf("**** Node 2 sending to node 1!")
	buf = bytes.NewBufferString(msgString)
	msg.Content = buf

	err = node2.SendMsg(msg)
	if err != nil {
		t.Error(node1, "Node1 CID error: "+err.Error())
	}

	// Node 2 send a message  to node 3
	msg = api.Msg{}
	msg.Name = "node3"
	msg.PubKey = node3CID
	msgString = fmt.Sprintf(" **** Node 2 sending to node 3!")
	buf = bytes.NewBufferString(msgString)
	msg.Content = buf

	err = node2.SendMsg(msg)
	if err != nil {
		t.Error(node1, "node2 CID error: "+err.Error())
	}

	// Node 3 send a message to node 1
	msg = api.Msg{}
	msg.Name = "node1"
	msg.PubKey = node1CID
	msgString = fmt.Sprintf("**** Node 3 sending to node 1!")
	buf = bytes.NewBufferString(msgString)
	msg.Content = buf

	err = node3.SendMsg(msg)
	if err != nil {
		t.Error(node1, "node3 CID error: "+err.Error())
	}

	// Node 3 send a message to node 2
	msg = api.Msg{}
	msg.Name = "node2"
	msg.PubKey = node2CID
	msgString = fmt.Sprintf("**** Node 3 sending to node 2!")
	buf = bytes.NewBufferString(msgString)
	msg.Content = buf

	err = node3.SendMsg(msg)
	if err != nil {
		t.Error(node1, "node3 CID error: "+err.Error())
	}

	// Node 1 listen
	go func() {
		for {
			msg := <-node1.Out()
			mb := msg.Content.Bytes()
			t.Log("[*] Node 1 got a message ", string(mb))
		}
	}()

	// Node 2 listen
	go func() {
		for {
			msg := <-node2.Out()
			mb := msg.Content.Bytes()
			t.Log("[*] Node 2 got a message ", string(mb))
		}
	}()

	// Node 3 listen
	go func() {
		for {
			msg := <-node3.Out()
			mb := msg.Content.Bytes()
			t.Log("[*] Node 3 got a message ", string(mb))
		}
	}()

	time.Sleep(30 * time.Second)
}

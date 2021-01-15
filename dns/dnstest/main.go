package main

import (
	"log"
	"time"

	transport "github.com/awgh/ratnet-transports/dns"

	"github.com/awgh/bencrypt/bc"
	"github.com/awgh/bencrypt/ecc"
	"github.com/awgh/ratnet/api"
	"github.com/awgh/ratnet/nodes/ram"
)

func main() {

	a := new(ecc.KeyPair)
	b := new(ecc.KeyPair)
	c := new(ecc.KeyPair)
	d := new(ecc.KeyPair)
	a.GenerateKey()
	b.GenerateKey()
	c.GenerateKey()
	d.GenerateKey()
	nodeClient := ram.New(a, b)
	nodeServer := ram.New(c, d)

	clientTransport := transport.New(nodeClient, 0x11223344, 0x55667788)

	serverTransport := transport.New(nodeServer, 0x55667788, 0x11223344) //0x55667788

	go serverTransport.Listen(":53350", true)

	//
	go clientTransport.Listen(":53351", true)
	//

	//go func() {
	i := 0
	for { //+fmt.Sprintf("%d", i)

		p1, err := serverTransport.RPC("127.0.0.1:53351", api.CID)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("Got CID: %+v\n", p1)
		r1 := p1.(bc.PubKey)
		log.Printf("CID cast to PubKey: %+v -> %s\n", r1, r1.ToB64())

		result, err := clientTransport.RPC("127.0.0.1:53350", api.AddContact, "destname1", r1.ToB64()) // "FNORD"+fmt.Sprintf("%d", i))
		if err != nil {
			log.Fatal(err)
		} else {
			log.Printf("received: %v\n", result)
			//serverTransport.Stop()
			//clientTransport.Stop()
			//return
		}
		/*
			result, err = serverTransport.RPC("127.0.0.1:53351", "CID")
			if err != nil {
				log.Fatal(err)
			} else {
				log.Printf("received: %v\n", result)
			}
		*/
		i++
		//break
		time.Sleep(5000 * time.Millisecond)
	}
	//}()
}

// Copyright (c) 2020, Oracle and/or its affiliates.

package s3transport

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"strconv"
	"sync"

	"github.com/awgh/bencrypt/bc"
	"github.com/awgh/bencrypt/ecc"
	"github.com/awgh/ratnet"
	"github.com/awgh/ratnet/api"
	"github.com/awgh/ratnet/api/events"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

// Module : S3 Implementation of a Transport module
type Module struct {
	node       api.Node
	isRunning  bool
	wg         sync.WaitGroup
	byteLimit  int64
	Region     string
	Namespace  string
	AccessKey  string
	SecretKey  string
	EndPoint   string
	C2Bucket   string
	TimeBucket string
	s3Session  s3.S3

	RoutingPubKey bc.PubKey
}

func init() {
	ratnet.Transports["s3obj"] = NewFromMap // register this module by name (for deserialization support)
}

// NewFromMap : Makes a new instance of this transport module from a map of arguments (for deserialization support)
func NewFromMap(node api.Node, t map[string]interface{}) api.Transport {

	var region, namespace, accessKey, secretKey, routingPubKey, endPoint, c2bucket, timeBucket string

	if _, ok := t["Region"]; ok {
		region = t["Region"].(string)
	}

	if _, ok := t["Namespace"]; ok {
		namespace = t["Namespace"].(string)
	}

	if _, ok := t["AccessKey"]; ok {
		accessKey = t["AccessKey"].(string)
	}

	if _, ok := t["SecretKey"]; ok {
		secretKey = t["SecretKey"].(string)
	}

	if _, ok := t["RoutingPubKey"]; ok {
		routingPubKey = t["RoutingPubKey"].(string)
	}

	if _, ok := t["EndPoint"]; ok {
		endPoint = t["EndPoint"].(string)
	}

	if _, ok := t["C2Bucket"]; ok {
		c2bucket = t["C2Bucket"].(string)
	}

	if _, ok := t["TimeBucket"]; ok {
		timeBucket = t["TimeBucket"].(string)
	}

	return New(namespace, region, node, accessKey, secretKey, routingPubKey, endPoint, c2bucket, timeBucket)
}

// New : Makes a new instance of this transport module
func New(namespace string, region string, node api.Node, accessKey string, secretKey string, pubkey string, endpoint string, c2Bucket string, timeBucket string) *Module {

	objectStorage := new(Module)
	objectStorage.Region = region
	objectStorage.Namespace = namespace
	objectStorage.AccessKey = accessKey
	objectStorage.SecretKey = secretKey
	objectStorage.node = node
	objectStorage.byteLimit = 8000 * 1024
	objectStorage.EndPoint = endpoint
	objectStorage.C2Bucket = c2Bucket
	objectStorage.TimeBucket = timeBucket

	pk := new(ecc.PubKey)
	pk.FromB64(pubkey)
	objectStorage.RoutingPubKey = pk

	s3Config := &aws.Config{
		Credentials:      credentials.NewStaticCredentials(accessKey, secretKey, ""),
		Endpoint:         aws.String(endpoint),
		Region:           aws.String(region),
		S3ForcePathStyle: aws.Bool(true),
	}

	newSession := session.New(s3Config)
	s3Session := *s3.New(newSession)

	objectStorage.s3Session = s3Session

	return objectStorage
}

// Name : Returns name of module
func (s3obj *Module) Name() string {
	return "s3obj"
}

// MarshalJSON : Create a serialied representation of the config of this module
func (s3obj *Module) MarshalJSON() (b []byte, e error) {
	// make sure the fields from NewFromMap are marshalled
	return json.Marshal(map[string]interface{}{
		"Transport":     "s3obj",
		"Region":        s3obj.Region,
		"Tenancy":       s3obj.Namespace,
		"Accesskey":     s3obj.AccessKey,
		"SecretKey":     s3obj.SecretKey,
		"RoutingPubKey": s3obj.RoutingPubKey,
		"EndPoint":      s3obj.EndPoint,
		"C2Bucket":      s3obj.C2Bucket,
		"TimeBucket":    s3obj.TimeBucket,
	})
}

// ByteLimit - get limit on bytes per bundle for this transport
func (s3obj *Module) ByteLimit() int64 {
	return s3obj.byteLimit
}

// SetByteLimit - set limit on bytes per bundle for this transport
func (s3obj *Module) SetByteLimit(limit int64) {
	s3obj.byteLimit = limit
}

// Listen : We do not need this for S3
func (s3obj *Module) Listen(listen string, adminMode bool) {
	return
}

// getCounter will search for the "time" object in time bucket in order to
// Determine what our current monotonic timercounter is set to
func (s3obj *Module) getCounter() int64 {
	objects, _ := s3obj.s3Session.ListObjectsV2(&s3.ListObjectsV2Input{
		Bucket: aws.String(s3obj.TimeBucket),
	})

	// Get the time object if it exists
	if objects.Contents != nil {
		object, err := s3obj.s3Session.GetObject(&s3.GetObjectInput{
			Bucket: aws.String(s3obj.TimeBucket),
			Key:    aws.String("time"),
		})

		if err != nil {
			return 0
		}

		// convert []byte to int64
		buf := bytes.NewBuffer(nil)
		if _, err := io.Copy(buf, object.Body); err != nil {
			return 0
		}
		timeCounter, _ := binary.Varint(buf.Bytes())

		return int64(timeCounter)
	}
	return 0
}

func (s3obj *Module) setCounter() (int64, error) {

	objects, _ := s3obj.s3Session.ListObjectsV2(&s3.ListObjectsV2Input{
		Bucket: aws.String(s3obj.TimeBucket),
	})

	var timeCounter int64

	if objects.Contents != nil {
		if len(objects.Contents) > 1 {
			log.Fatal("There should never have more than 1 object in ", s3obj.TimeBucket)
		}

		timeCounter = s3obj.getCounter()
		timeCounter += int64(rand.Intn(1337))

	} else {
		timeCounter = 0x1337b33f
	}

	// convert int64 to []byte
	buf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutVarint(buf, timeCounter)
	timeBytes := buf[:n]

	_, err := s3obj.s3Session.PutObject(&s3.PutObjectInput{
		Bucket:             aws.String(s3obj.TimeBucket),
		Key:                aws.String("time"),
		Body:               bytes.NewReader(timeBytes),
		ContentDisposition: aws.String("ratnet"),
	})

	if err != nil {
		return 0, err
	}

	return timeCounter, nil
}

// RPC : client interface
func (s3obj *Module) RPC(host string, method string, args ...interface{}) (interface{}, error) {

	events.Info(s3obj.node, fmt.Sprintf("\n***\n***RPC %s on %s called with: %+v\n***\n", method, host, args))

	switch method {

	case "Pickup":

		if args == nil || len(args) < 2 {
			return nil, errors.New("failed due to arg length in Pickup for s3obj")
		}

		var bundle api.Bundle

		lastTime := args[1].(int64)

		// If time is not -1, get the most recent time index from our "time" object
		if lastTime < 0 {
			bundle.Time = s3obj.getCounter()
			return bundle, nil
		}

		events.Debug(s3obj.node, "The last time was %d", lastTime)

		var rr api.RemoteResponse

		objects, _ := s3obj.s3Session.ListObjectsV2(&s3.ListObjectsV2Input{
			Bucket:     aws.String(s3obj.C2Bucket),
			StartAfter: aws.String(strconv.FormatInt(lastTime, 10)),
		})

		if objects.Contents == nil {
			return nil, nil
		}

		item := objects.Contents[0]

		object, err := s3obj.s3Session.GetObject(&s3.GetObjectInput{
			Bucket: aws.String(s3obj.C2Bucket),
			Key:    item.Key,
		})

		if err != nil {
			return nil, err
		}

		// Create the gob decoder to dec api.Bundle
		buf, _ := ioutil.ReadAll(object.Body)
		byteBuffer := bytes.NewBuffer(buf)
		reader := bufio.NewReader(byteBuffer)
		dec := gob.NewDecoder(reader)

		if err := dec.Decode(&bundle); err != nil {
			events.Warning(s3obj.node, "s3obj rpc gob decode failed: "+err.Error())
			return nil, err
		}

		bundle.Time, _ = strconv.ParseInt(*item.Key, 10, 64)
		rr.Value = bundle
		return rr.Value, nil

	case "Dropoff":

		if args == nil || len(args) < 1 {
			return nil, errors.New("failed due to arg length in Dropoff for s3obj")
		}

		// arg 0 is the bundle coming into Dropoff
		bundle := args[0].(api.Bundle)

		// Create the gob encoder to encode api.RemoteCall into
		buf := bytes.NewBuffer([]byte{})
		writer := bufio.NewWriter(buf)

		enc := gob.NewEncoder(writer)

		if err := enc.Encode(bundle); err != nil {
			events.Warning(s3obj.node, "s3obj rpc gob encode failed: "+err.Error())
			return nil, err
		}
		writer.Flush()

		timeCounter, _ := s3obj.setCounter()

		_, err := s3obj.s3Session.PutObject(&s3.PutObjectInput{
			Bucket: aws.String(s3obj.C2Bucket),
			Key:    aws.String(strconv.FormatInt(timeCounter, 10)),
			Body:   bytes.NewReader(buf.Bytes()),
		})

		return nil, err

	case "ID":
		return s3obj.RoutingPubKey, nil
	}
	return nil, errors.New("Not Implemented")
}

// Stop : Stops module
func (s3obj *Module) Stop() {
	return
}

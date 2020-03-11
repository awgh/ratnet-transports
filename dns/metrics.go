package dns

import "log"

var clientMetrics Metrics
var serverMetrics Metrics

func init() {
	clientMetrics.Name = "client"
	serverMetrics.Name = "server"
}

// Metrics - for collecting stats on DNS traffic
type Metrics struct {
	Name                    string
	bytesRecv, msgsRecv     int
	bytesSent, msgsSent     int
	rawBytesIn, rawBytesOut int
	sentRetries             int
}

//RecvBytes - record the reception of n bytes
func (c *Metrics) RecvBytes(n int) { c.bytesRecv += n }

// RecvMsgs - record the reception of a message
func (c *Metrics) RecvMsgs() { c.msgsRecv++ }

// SentBytes - record the sending of n bytes
func (c *Metrics) SentBytes(n int) { c.bytesSent += n }

// SentMsgs - record the sending of a message
func (c *Metrics) SentMsgs() { c.msgsSent++ }

// RawBytesIn - record the reception of n raw bytes
func (c *Metrics) RawBytesIn(n int) { c.rawBytesIn += n }

// RawBytesOut - record the sending of n raw bytes
func (c *Metrics) RawBytesOut(n int) { c.rawBytesOut += n }

// SentRetry - record the sending of a retry
func (c *Metrics) SentRetry() { c.sentRetries++ }

// Print - pretty-prints this structure
func (c *Metrics) Print() {
	log.Printf("%s Recv/Sent msgs=%d/%d, bytes=%d/%d raw in/out=%d/%d\n", c.Name, c.msgsRecv, c.msgsSent,
		c.bytesRecv, c.bytesSent, c.rawBytesIn, c.rawBytesOut)
}

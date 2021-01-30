module github.com/awgh/ratnet-transports

go 1.15

replace github.com/awgh/debouncer => /home/alquist/go/src/github.com/awgh/debouncer

replace github.com/xtaci/kcp-go => /home/alquist/go/src/github.com/xtaci/kcp-go

require (
	github.com/awgh/bencrypt v0.0.0-20190918184257-b65cb460b2c8
	github.com/awgh/debouncer v0.0.0-20200721022636-91ed01fa9bc9
	github.com/awgh/ratnet v1.1.1-0.20210126100655-bea2d99c2477
	github.com/aws/aws-sdk-go v1.36.28
	github.com/klauspost/reedsolomon v1.9.11 // indirect
	github.com/miekg/dns v1.1.35
	github.com/pkg/profile v1.5.0
	github.com/xtaci/kcp-go v5.4.20+incompatible
	github.com/xtaci/lossyconn v0.0.0-20200209145036-adba10fffc37 // indirect
	golang.org/x/net v0.0.0-20210119194325-5f4716e94777 // indirect
	golang.org/x/sys v0.0.0-20210124154548-22da62e12c0c // indirect
)

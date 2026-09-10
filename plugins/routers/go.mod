module github.com/gateyes/gateway/plugins/routers

go 1.26.0

require (
	github.com/cespare/xxhash/v2 v2.3.0
	github.com/gateyes/gateway v0.0.0
	google.golang.org/grpc v1.80.0
)

require (
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260401024825-9d38bb4040a9 // indirect
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af // indirect
)

replace github.com/gateyes/gateway => ../..

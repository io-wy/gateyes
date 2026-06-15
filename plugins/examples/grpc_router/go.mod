module grpc_router_plugin

go 1.26

require (
	github.com/gateyes/gateway v0.0.0
	google.golang.org/grpc v1.81.0
)

require (
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260401024825-9d38bb4040a9 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/gateyes/gateway => ../../..

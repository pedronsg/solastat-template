module github.com/pedronsg/solastat-template

go 1.25.4

require (
	github.com/OrbitOS-org/orbit-os-sdk-go/v26 v26.0.0
	github.com/pedronsg/solastat-auth v0.0.0
	google.golang.org/grpc v1.77.0
	google.golang.org/protobuf v1.36.10
)

require (
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251022142026-3a174f9686a8 // indirect
)

replace github.com/OrbitOS-org/orbit-os-sdk-go/v26 => ./orbit-os-sdk-go

replace github.com/pedronsg/solastat-auth => ./solastat-auth

#!/bin/sh

docker run -i -v `pwd`:/go/src/libvirt_nova golibvirt:1.0 /bin/sh << 'EOF'
cd /go/src/libvirt_nova
export GOPATH=/go
go get -d ./...
go build --ldflags '-extldflags "-static"' 
strip libvirt_nova
EOF

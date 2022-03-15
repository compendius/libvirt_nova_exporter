FROM golang:1.17.3-alpine3.15 AS build

ENV LIBVIRT_NOVA=/libvirt_nova

RUN apk add ca-certificates g++ git libvirt-dev libvirt
WORKDIR $LIBVIRT_NOVA
COPY . .
RUN go env -w  GO111MODULE=auto
RUN go get -d ./...
RUN go build

FROM alpine:3.15
RUN apk add ca-certificates libvirt
COPY --from=build $LIBVIRT_NOVA/libvirt_nova /

ENTRYPOINT [ "/libvirt_nova" ]

FROM alpine:latest as builder

ARG GO_BINARY_NAME="go1.22.0.linux-amd64.tar.gz"

WORKDIR /build

RUN apk update && apk add wget curl
RUN wget "https://go.dev/dl/$GO_BINARY_NAME" && tar -C /usr/local -xzf $GO_BINARY_NAME

COPY go.mod  .
COPY go.sum  .
RUN /usr/local/go/bin/go mod download

COPY main.go .
COPY ovpn.go .
RUN /usr/local/go/bin/go build -o koco .

FROM kylemanna/openvpn:latest as base

WORKDIR /app

COPY --from=builder /build/koco .
COPY templates templates

RUN echo "#!/bin/ash" >> /entrypoint.sh && \
    echo "nohup /app/koco 2>&1 &" >> /entrypoint.sh && \
    echo "/usr/local/bin/ovpn_run" >> /entrypoint.sh && \
    chmod +x /entrypoint.sh

ENTRYPOINT [ "/entrypoint.sh" ]

FROM golang:1.18-alpine as builder
LABEL maintainer="Julio Cesar <julio@blackdevs.com.br>"

WORKDIR /go/src/app

COPY go.mod go.sum ./
RUN go mod download

COPY ./ ./

RUN GOOS=linux GOARCH=amd64 GO111MODULE=on CGO_ENABLED=0 \
    go build -ldflags="-s -w" -o ./main .

FROM busybox:1

LABEL maintainer="Julio Cesar <julio@blackdevs.com.br>"
LABEL org.opencontainers.image.source "https://github.com/juliocesarscheidt/kube-log-wrapper-api"
LABEL org.opencontainers.image.description "API to serve logs from Kubernetes pods"
LABEL org.opencontainers.image.licenses "MIT"

WORKDIR /

COPY --from=builder --chown=65534:65534 /go/src/app/main .
EXPOSE 9000

# user nobody
USER 65534

ENTRYPOINT [ "/main" ]

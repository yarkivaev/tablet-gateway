FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /tablet-gateway ./cmd/tablet-gateway
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends adb ca-certificates curl && rm -rf /var/lib/apt/lists/*
ARG SCRCPY_VERSION=3.3.3
RUN mkdir -p /usr/share/tablet-gateway && curl -fL --retry 3 https://github.com/Genymobile/scrcpy/releases/download/v${SCRCPY_VERSION}/scrcpy-server-v${SCRCPY_VERSION} -o /usr/share/tablet-gateway/scrcpy-server && echo "7e70323ba7f259649dd4acce97ac4fefbae8102b2c6d91e2e7be613fd5354be0  /usr/share/tablet-gateway/scrcpy-server" | sha256sum -c -
WORKDIR /app
COPY --from=build /tablet-gateway /usr/local/bin/tablet-gateway
COPY web ./web
EXPOSE 8080/tcp 50000-50100/udp
ENTRYPOINT ["tablet-gateway"]

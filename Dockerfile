# Build the manager binary
FROM golang:1.19 as builder
ARG TARGETOS
ARG TARGETARCH

ENV CGO_ENABLED=0        \
    GOOS=linux           \
	GOPROXY="https://goproxy.cn,direct" \
	GO111MODULE=on

WORKDIR /workspace

COPY . .

RUN go mod download

RUN  go build -ldflags="-s -w" -o manager main.go

# running container

FROM alpine

ENV TZ=Asia/Shanghai

WORKDIR /app

COPY --from=builder /workspace/manager .

EXPOSE 6387


ENTRYPOINT ["./manager"]

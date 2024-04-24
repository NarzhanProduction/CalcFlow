FROM golang:1.22.1

WORKDIR /build

COPY . ./

WORKDIR ./backend/internal/agent

RUN go build -o agent agent.go

CMD ["./agent"]

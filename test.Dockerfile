FROM golang:1.25

WORKDIR /app
COPY . .
RUN go build -o ./out/ci cmd/ci/main.go

ENTRYPOINT [ "./out/ci" ]

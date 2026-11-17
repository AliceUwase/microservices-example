FROM golang:1.26-alpine
RUN apk add --no-cache git bash netcat-openbsd
WORKDIR /app
COPY . .
RUN go mod tidy
RUN go build -o server .
ENTRYPOINT ["/app/server"]

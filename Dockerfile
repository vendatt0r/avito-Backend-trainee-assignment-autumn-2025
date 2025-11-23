FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go env -w GOPROXY=https://proxy.golang.org,direct
RUN go mod download
COPY . .
RUN go build -o /app ./cmd/server

FROM alpine:3.18
RUN apk add --no-cache ca-certificates netcat-openbsd
COPY --from=build /app /app
COPY wait-for.sh /wait-for.sh
RUN chmod +x /wait-for.sh

EXPOSE 8080
ENV PORT=8080

ENTRYPOINT ["/wait-for.sh", "db", "/app"]

FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /app

FROM gcr.io/distroless/static
COPY --from=builder /app /app
USER nonroot
ENTRYPOINT ["/app"]

FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /agent ./cmd/agent

FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app

COPY --from=builder /agent .

# Runs the web UI *and* the auto-publish scheduler together on :8081.
# Config comes from real environment variables (e.g. docker --env-file),
# so we deliberately do NOT bake .env into the image.
EXPOSE 8081
ENTRYPOINT ["/app/agent"]
CMD ["-serve", "-addr=:8081"]

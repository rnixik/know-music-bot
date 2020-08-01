# build stage
FROM golang as builder

WORKDIR /app

COPY ./go.mod .
COPY ./go.sum .

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build

# final stage
FROM busybox
WORKDIR /app
COPY --from=builder /app/know-music-bot /app/
COPY --from=builder /app/.env /app/.env

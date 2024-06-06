# Local environment stage.
FROM golang:1.22-alpine AS local_base
WORKDIR /mapping

RUN go install github.com/cosmtrek/air@v1.49.0

COPY go.mod go.sum ./
RUN go mod download && go mod verify

RUN apk add -U tzdata
ENV TZ=America/Monterrey
RUN cp /usr/share/zoneinfo/America/Monterrey /etc/localtime

COPY . .
CMD ["air", "-c", ".air.toml"]

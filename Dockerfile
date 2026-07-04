FROM golang:1.26-alpine AS build

WORKDIR /src

RUN apk add --no-cache ca-certificates build-base

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /out/admin-panel ./cmd/admin-panel

FROM alpine:3.21 AS runtime

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S adminpanel \
    && adduser -S -D -H -G adminpanel adminpanel

WORKDIR /app
COPY --from=build /out/admin-panel /app/admin-panel
COPY migrations /app/migrations/admin

EXPOSE 8080

USER adminpanel:adminpanel

CMD ["/app/admin-panel"]

FROM runtime AS migrations

USER root

RUN apk add --no-cache postgresql-client

USER adminpanel:adminpanel

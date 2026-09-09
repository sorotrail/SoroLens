FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/sorolens ./cmd/sorolens

FROM alpine:3.20
RUN adduser -D -H sorolens && apk add --no-cache ca-certificates
USER sorolens
COPY --from=build /out/sorolens /usr/local/bin/sorolens
EXPOSE 8080
ENTRYPOINT ["sorolens"]

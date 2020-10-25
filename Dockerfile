FROM golang:alpine as builder
RUN mkdir /build
ADD . /build
WORKDIR /build
RUN go build -o bouncer_server cmd/server/main.go

FROM alpine
RUN adduser -S -D -H -h /app bouncer
USER bouncer
COPY --from=builder /build/bouncer_server /app/
WORKDIR /app
CMD ["./bouncer_server"]
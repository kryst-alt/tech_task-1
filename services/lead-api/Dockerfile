# Build from the repository root: docker build -f services/lead-api/Dockerfile .
FROM docker.io/library/golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY db ./db
COPY internal ./internal
COPY services/lead-api ./services/lead-api
RUN CGO_ENABLED=0 go build -o /out/lead-api ./services/lead-api

FROM docker.io/library/alpine:3.22
RUN adduser -D app
USER app
COPY --from=build /out/lead-api /lead-api
EXPOSE 8080
ENTRYPOINT ["/lead-api"]
